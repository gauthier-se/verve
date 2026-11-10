package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/app"
	"github.com/gauthier-se/verve/internal/version"
)

// Given serve holds the data directory lock and writes, when verve backup runs, then it prints the
// path of backups/verve-<version>-<UTC timestamp>-manual.tar.zst, and the writes go on meanwhile.
func TestBackup_PrintsThePathWhileTheServerWrites(t *testing.T) {
	t.Parallel()

	deps, dataDir := userDeps(t)
	deps.Backup = app.Backup

	lock, err := app.AcquireLock(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	s := openStore(t, dataDir)

	writeCtx, stopWriting := context.WithCancel(t.Context())

	var (
		wg       sync.WaitGroup
		writeErr error
		writes   atomic.Int64
	)

	wg.Go(func() {
		for writeCtx.Err() == nil {
			// Each write must land well before the busy timeout: a backup that held the writer
			// would make it wait.
			ctx, cancel := context.WithTimeout(writeCtx, 2*time.Second)
			err := s.InstanceSettings().Set(ctx, fmt.Sprintf("write.%d", writes.Load()), "value")

			cancel()

			if err != nil && writeCtx.Err() == nil {
				writeErr = err

				return
			}

			writes.Add(1)
		}
	})

	// The backup starts once the writes do.
	require.Eventually(t, func() bool { return writes.Load() > 0 }, 3*time.Second, 5*time.Millisecond)

	res := run(t, deps, "backup")

	stopWriting()
	wg.Wait()

	require.NoError(t, writeErr)

	require.Equal(t, 0, res.code, res.stderr)
	require.Empty(t, res.stderr)

	backups := filepath.Join(dataDir, "backups")
	pattern := "^" + regexp.QuoteMeta(filepath.Join(backups, "verve-"+version.Get().Version+"-")) +
		`\d{8}T\d{6}Z-manual\.tar\.zst` + "\n$"
	require.Regexp(t, pattern, res.stdout)

	info, err := os.Stat(strings.TrimSuffix(res.stdout, "\n"))
	require.NoError(t, err)
	require.Positive(t, info.Size())

	_, err = app.AcquireLock(dataDir)
	require.EqualError(t, err, "another Verve process is using "+dataDir)
}

// Given an invalid configuration, then verve backup fails without backing up.
func TestBackup_InvalidConfigFails(t *testing.T) {
	t.Parallel()

	deps := depsWithEnv(map[string]string{"VERVE_LOG_LEVEL": "verbose"})
	deps.Backup = app.Backup

	res := run(t, deps, "backup")

	require.Equal(t, 1, res.code)
	require.True(t, strings.HasPrefix(res.stderr, "Error: invalid configuration"), res.stderr)
	require.Empty(t, res.stdout)
}
