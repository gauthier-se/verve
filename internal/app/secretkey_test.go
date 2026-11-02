package app

import (
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// existingKey is a key that an earlier start or the operator left in secret.key.
const existingKey = "0123456789abcdef0123456789abcdef"

// configuredKey is a key from VERVE_SECRET_KEY.
const configuredKey = "fedcba9876543210fedcba9876543210"

func secretKeyPath(t *testing.T) string {
	t.Helper()

	return newLayout(t.TempDir()).SecretKey
}

func requireFileContent(t *testing.T, path, want string) {
	t.Helper()

	content, err := os.ReadFile(path) //nolint:gosec // G304: a test file
	require.NoError(t, err)
	require.Equal(t, want, string(content))
}

// Given no VERVE_SECRET_KEY and no secret.key, then 32 random bytes, hex encoded, are written to
// secret.key with permissions 0600.
func TestEnsureSecretKey_NoKeyGeneratesTheFile(t *testing.T) {
	t.Parallel()

	path := secretKeyPath(t)

	key, err := ensureSecretKey(path, "")
	require.NoError(t, err)

	raw, err := hex.DecodeString(key)
	require.NoError(t, err)
	require.Len(t, raw, 32)

	requireFileContent(t, path, key)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// Given two instances without a key, then each generates its own.
func TestEnsureSecretKey_GeneratedKeysAreRandom(t *testing.T) {
	t.Parallel()

	first, err := ensureSecretKey(secretKeyPath(t), "")
	require.NoError(t, err)

	second, err := ensureSecretKey(secretKeyPath(t), "")
	require.NoError(t, err)

	require.NotEqual(t, first, second)
}

// Given an existing secret.key, then it is reused unchanged. Surrounding whitespace, as left by
// `openssl rand -hex 32 > secret.key` or an editor, is not part of the key.
func TestEnsureSecretKey_ExistingFileIsReusedUnchanged(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		content string
	}{
		{name: "generated", content: existingKey},
		{name: "with a final newline", content: existingKey + "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := secretKeyPath(t)
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o600))

			key, err := ensureSecretKey(path, "")
			require.NoError(t, err)
			require.Equal(t, existingKey, key)

			requireFileContent(t, path, tt.content)
		})
	}
}

// Given VERVE_SECRET_KEY, then it wins, and no file is created.
func TestEnsureSecretKey_ConfiguredKeyCreatesNoFile(t *testing.T) {
	t.Parallel()

	path := secretKeyPath(t)

	key, err := ensureSecretKey(path, configuredKey)
	require.NoError(t, err)
	require.Equal(t, configuredKey, key)

	require.NoFileExists(t, path)
}

// Given VERVE_SECRET_KEY and an existing secret.key, then the variable wins and the file stays.
func TestEnsureSecretKey_ConfiguredKeyWinsOverTheFile(t *testing.T) {
	t.Parallel()

	path := secretKeyPath(t)
	require.NoError(t, os.WriteFile(path, []byte(existingKey), 0o600))

	key, err := ensureSecretKey(path, configuredKey)
	require.NoError(t, err)
	require.Equal(t, configuredKey, key)

	requireFileContent(t, path, existingKey)
}

// Given VERVE_SECRET_KEY and a secret.key that cannot be read, then the file is not even read.
func TestEnsureSecretKey_ConfiguredKeyIgnoresAnUnreadableFile(t *testing.T) {
	t.Parallel()

	path := secretKeyPath(t)
	require.NoError(t, os.Mkdir(path, 0o750))

	key, err := ensureSecretKey(path, configuredKey)
	require.NoError(t, err)
	require.Equal(t, configuredKey, key)
}

// Given a secret.key that cannot be read, then it fails with a clear error and keeps the file.
func TestEnsureSecretKey_UnreadableFileFails(t *testing.T) {
	t.Parallel()

	t.Run("no read permission", func(t *testing.T) {
		t.Parallel()
		skipUnlessPermissionsEnforced(t)

		path := secretKeyPath(t)
		require.NoError(t, os.WriteFile(path, []byte(existingKey), 0o000))

		_, err := ensureSecretKey(path, "")
		require.ErrorContains(t, err, "read secret key: ")
		require.ErrorContains(t, err, path)
		require.ErrorIs(t, err, fs.ErrPermission)

		require.NoError(t, os.Chmod(path, 0o600))
		requireFileContent(t, path, existingKey)
	})

	t.Run("a directory in its place", func(t *testing.T) {
		t.Parallel()

		path := secretKeyPath(t)
		require.NoError(t, os.Mkdir(path, 0o750))

		_, err := ensureSecretKey(path, "")
		require.ErrorContains(t, err, "read secret key: ")
		require.ErrorContains(t, err, path)
	})
}

// Given a secret.key without a usable key, then it fails instead of encrypting with it, and the
// error does not hold the content of the file. A generated file is never partial, so this is
// damage that the operator has to see.
func TestEnsureSecretKey_TooShortFileFails(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "only a newline", content: "\n"},
		{name: "31 characters", content: existingKey[:31]},
		{name: "31 characters and a newline", content: existingKey[:31] + "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := secretKeyPath(t)
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o600))

			_, err := ensureSecretKey(path, "")
			require.EqualError(t, err, "read secret key: "+path+": must be at least 32 characters")

			requireFileContent(t, path, tt.content)
		})
	}
}

// Given no key and a data directory that refuses new files, then it fails with a clear error.
func TestEnsureSecretKey_UnwritableDirectoryFails(t *testing.T) {
	t.Parallel()
	skipUnlessPermissionsEnforced(t)

	dir := t.TempDir()
	setWritable(t, dir, false)
	t.Cleanup(func() { setWritable(t, dir, true) })

	_, err := ensureSecretKey(newLayout(dir).SecretKey, "")
	require.ErrorContains(t, err, "create secret key: ")
	require.ErrorIs(t, err, fs.ErrPermission)
}

// Given two processes generating at once, then both end with the same key. Goroutines race on the
// file system like processes do, because the function shares nothing else.
func TestEnsureSecretKey_ConcurrentGenerationsEndWithTheSameKey(t *testing.T) {
	t.Parallel()

	const (
		attempts  = 50
		processes = 8
	)

	for range attempts {
		dir := t.TempDir()
		path := filepath.Join(dir, secretKeyFile)

		var (
			done sync.WaitGroup
			keys [processes]string
			errs [processes]error
		)

		start := make(chan struct{})

		for i := range processes {
			done.Go(func() {
				<-start

				keys[i], errs[i] = ensureSecretKey(path, "")
			})
		}

		close(start)
		done.Wait()

		for i := range processes {
			require.NoError(t, errs[i])
			require.Equal(t, keys[0], keys[i])
		}

		requireFileContent(t, path, keys[0])

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, entries, 1, "generating must leave only secret.key behind")
	}
}
