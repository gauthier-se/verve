package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/logging"
	"github.com/gauthier-se/verve/internal/logging/loggingtest"
	"github.com/gauthier-se/verve/internal/store"
)

// setupLink matches the setup link on stderr and captures its token.
var setupLink = regexp.MustCompile(`/setup#([A-Za-z0-9_-]+)`)

// startServe runs Run as verve serve does: its logs, in the format and from the level of cfg.Log,
// and the setup line go to one stderr. It waits until the server is ready, and returns the bound
// address, stderr and a function that stops Run and checks that it returned without error.
func startServe(t *testing.T, cfg config.Config) (string, *syncBuffer, func()) {
	t.Helper()

	var stderr syncBuffer

	// The trace sees the info logs whatever cfg.Log, to tell when the server is ready.
	traceLogger, trace := testLogger()
	logger := slog.New(slog.NewMultiHandler(logging.New(&stderr, cfg.Log).Handler(), traceLogger.Handler()))

	addr, stop := runUntilListening(t, cfg, logger, &stderr, trace, beforeMigrate, jobs.NewRegistry())

	require.Eventually(t, func() bool {
		return strings.Contains(trace.String(), `msg="server ready"`)
	}, 5*time.Second, 10*time.Millisecond, "Run must log that the server is ready")

	return addr, &stderr, stop
}

// setupTokenOf returns the token of the setup link that stderr holds.
func setupTokenOf(t *testing.T, stderr *syncBuffer) string {
	t.Helper()

	m := setupLink.FindStringSubmatch(stderr.String())
	require.NotNil(t, m, "serve must write the setup link on stderr")

	return m[1]
}

// Given no user, then after the migrations serve writes one plain line on stderr with the setup link
// of the public URL, or the path of the setup page without one, whatever the log level and format.
// No other line holds the token, and the other lines are logs of the configured format.
func TestRun_NoUser_WritesTheSetupLinkOnStderr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		publicURL string
		log       config.LogConfig
		// want is the setup line, with %s for the token.
		want string
	}{
		{
			name:      "text with a public URL",
			publicURL: "https://verve.example.com",
			log:       config.LogConfig{Level: "info", Format: config.LogFormatText},
			want:      "Create the first admin at https://verve.example.com/setup#%s\n",
		},
		{
			name: "text without a public URL",
			log:  config.LogConfig{Level: "debug", Format: config.LogFormatText},
			want: "Create the first admin at /setup#%s on the address of this server\n",
		},
		{
			name:      "json",
			publicURL: "http://verve.lan:8080",
			log:       config.LogConfig{Level: "debug", Format: config.LogFormatJSON},
			want:      "Create the first admin at http://verve.lan:8080/setup#%s\n",
		},
		{
			name: "json at the error level",
			log:  config.LogConfig{Level: "error", Format: config.LogFormatJSON},
			want: "Create the first admin at /setup#%s on the address of this server\n",
		},
		{
			name: "text at the error level",
			log:  config.LogConfig{Level: "error", Format: config.LogFormatText},
			want: "Create the first admin at /setup#%s on the address of this server\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := testConfig(t.TempDir())
			cfg.Log = tt.log

			if tt.publicURL != "" {
				require.NoError(t, cfg.PublicURL.UnmarshalText([]byte(tt.publicURL)))
			}

			_, stderr, stop := startServe(t, cfg)
			stop()

			token := setupTokenOf(t, stderr)
			line := fmt.Sprintf(tt.want, token)

			var withToken, logs []string

			for l := range strings.Lines(stderr.String()) {
				if strings.Contains(l, token) {
					withToken = append(withToken, l)
				} else {
					logs = append(logs, l)
				}
			}

			require.Equal(t, []string{line}, withToken)

			out := stderr.String()
			require.Less(t, strings.LastIndex(out, "applied migration"), strings.Index(out, line),
				"the setup line must follow the migrations")

			if tt.log.Format == config.LogFormatJSON {
				loggingtest.DecodeLines(t, strings.Join(logs, ""))
			}

			if tt.log.Level == "error" {
				require.Empty(t, logs, "the setup line is written whatever the log level")
			}
		})
	}
}

// Given a user exists, then serve writes no setup line.
func TestRun_UserExists_WritesNoSetupLine(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	_, err = withSharedDB(ctx, cfg, func(s store.Store) (store.User, error) {
		return s.Users().Create(ctx, store.NewUser{Username: "alice", Role: store.RoleMember})
	})
	require.NoError(t, err)

	_, stderr, stop := startServe(t, cfg)
	stop()

	require.NotContains(t, stderr.String(), "/setup#")
	require.NotContains(t, stderr.String(), "first admin")
}

// postSetup creates the first admin named username with token on the server at addr, as the web app
// there sends it, and returns the response.
func postSetup(t *testing.T, addr, token, username string) fetched {
	t.Helper()

	body, err := json.Marshal(map[string]string{"token": token, "username": username, "password": "correct horse battery staple"})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+addr+"/api/v1/setup", bytes.NewReader(body))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Verve-Csrf", "1")
	req.Header.Set("Origin", "http://"+addr)

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	read, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	return fetched{status: res.StatusCode, header: res.Header, body: string(read)}
}

// Given a restart with no user, then serve writes a new token, and the old one fails.
func TestRun_RestartWithNoUser_WritesANewTokenAndTheOldOneFails(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())

	_, stderr, stop := startServe(t, cfg)
	stop()

	old := setupTokenOf(t, stderr)

	addr, stderr, stop := startServe(t, cfg)
	defer stop()

	current := setupTokenOf(t, stderr)
	require.NotEqual(t, old, current)

	res := postSetup(t, addr, old, "alice")
	require.Equal(t, http.StatusForbidden, res.status, res.body)
	require.Contains(t, res.body, `"code":"setup_token_invalid"`)

	res = postSetup(t, addr, current, "alice")
	require.Equal(t, http.StatusCreated, res.status, res.body)
}

// Given verve user create next to a running serve, then the setup is no longer required, and the
// setup token answers 409 setup_completed.
func TestRun_UserCreatedByTheCLIWhileServing_CompletesTheSetup(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())

	addr, stderr, stop := startServe(t, cfg)
	defer stop()

	token := setupTokenOf(t, stderr)

	_, err := CreateUser(t.Context(), cfg, auth.NewAccount{
		Username: "alice",
		Password: "correct horse battery staple",
		Role:     store.RoleAdmin,
	})
	require.NoError(t, err)

	status := fetch(t, "http://"+addr+"/api/v1/setup")
	require.Equal(t, http.StatusOK, status.status, status.body)
	require.JSONEq(t, `{"required":false}`, status.body)

	res := postSetup(t, addr, token, "bob")
	require.Equal(t, http.StatusConflict, res.status, res.body)
	require.Contains(t, res.body, `"code":"setup_completed"`)
}

// Given logs in text and JSON from the debug level, and the setup token used in a request, then the
// token appears in no log line: only in the setup line.
func TestRun_SetupTokenAppearsInNoLog(t *testing.T) {
	t.Parallel()

	for _, format := range []string{config.LogFormatText, config.LogFormatJSON} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			cfg := testConfig(t.TempDir())
			cfg.Log = config.LogConfig{Level: "debug", Format: format}

			addr, stderr, stop := startServe(t, cfg)
			token := setupTokenOf(t, stderr)

			res := postSetup(t, addr, token, "alice")
			require.Equal(t, http.StatusCreated, res.status, res.body)

			stop()

			out := stderr.String()
			require.Equal(t, 1, strings.Count(out, token), out)
			require.Contains(t, out, "Create the first admin at /setup#"+token+" on the address of this server\n")
			require.Contains(t, out, "/api/v1/setup", "the request log must have logged the setup")
		})
	}
}
