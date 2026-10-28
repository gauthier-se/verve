// Package loggingtest helps tests read what a JSON logger wrote.
package loggingtest

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// DecodeLines decodes each line of out, as written by a JSON logger.
func DecodeLines(t *testing.T, out string) []map[string]any {
	t.Helper()

	var lines []map[string]any

	for line := range strings.Lines(out) {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), "line %q", line)

		lines = append(lines, m)
	}

	return lines
}

// Buffer collects what a JSON logger writes. It is safe for concurrent use.
type Buffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

// Lines decodes every line written so far.
func (b *Buffer) Lines(t *testing.T) []map[string]any {
	t.Helper()

	b.mu.Lock()
	out := b.buf.String()
	b.mu.Unlock()

	return DecodeLines(t, out)
}
