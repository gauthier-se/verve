package app

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/gauthier-se/verve/internal/config"
)

// secretKeyFile is the name of the secret key file in the data directory.
const secretKeyFile = "secret.key"

// secretKeySize is the number of random bytes of a generated key.
const secretKeySize = 32

// ensureSecretKey returns the key that encrypts secrets at rest: configured when it is set, without
// touching the file at path, otherwise the content of that file, generated on first use.
func ensureSecretKey(path, configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	key, err := readSecretKey(path)
	if errors.Is(err, fs.ErrNotExist) {
		return generateSecretKey(path)
	}

	return key, err
}

func readSecretKey(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path comes from the configured data directory
	if err != nil {
		return "", fmt.Errorf("read secret key: %w", err)
	}

	key := strings.TrimSpace(string(data))
	if err := config.CheckSecretKey(key); err != nil {
		return "", fmt.Errorf("read secret key: %s: %w", path, err)
	}

	return key, nil
}

// generateSecretKey writes a new random key to a temporary file, with the permissions 0600 of
// os.CreateTemp, and links it to path. Like O_CREATE|O_EXCL, the link fails when another process
// got there first, whose key is then read back. Unlike it, path only ever appears complete: a
// process that lost the race cannot read an empty file, and a crash or a full disk leaves no
// partial key behind.
func generateSecretKey(path string) (string, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+secretKeyFile+"-*")
	if err != nil {
		return "", fmt.Errorf("create secret key: %w", err)
	}

	defer func() { _ = os.Remove(tmp.Name()) }()

	raw := make([]byte, secretKeySize)
	// crypto/rand.Read never returns an error.
	_, _ = rand.Read(raw)

	key := hex.EncodeToString(raw)

	_, err = tmp.WriteString(key)
	if err := errors.Join(err, tmp.Sync(), tmp.Close()); err != nil {
		return "", fmt.Errorf("create secret key: %w", err)
	}

	err = os.Link(tmp.Name(), path)
	if errors.Is(err, fs.ErrExist) {
		return readSecretKey(path)
	}

	if err != nil {
		return "", fmt.Errorf("create secret key: %w", err)
	}

	return key, nil
}
