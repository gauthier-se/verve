package applexml

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// failingReader yields its data, then err.
type failingReader struct {
	data io.Reader
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	n, err := r.data.Read(p)
	if errors.Is(err, io.EOF) {
		return n, r.err
	}

	return n, err
}

// Given the rest of a zip entry whose document read as malformed, then the drain tells a corrupted
// entry, keeps a read error of the disk, which is retried, stops once the context is done, and
// reports nothing for an entry that reads to its end.
func TestDrainEntry_TellsTheCauseOfAMalformedDocument(t *testing.T) {
	t.Parallel()

	disk := diskError{errors.New("input/output error")}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		r    io.Reader
		want error
	}{
		{name: "whole entry", ctx: t.Context(), r: strings.NewReader("rest"), want: nil},
		{name: "corrupted entry", ctx: t.Context(), r: entryReader{io.NopCloser(&failingReader{strings.NewReader("rest"), errors.New("zip: checksum error")})}, want: ErrCorruptedZip},
		{name: "read error of the disk", ctx: t.Context(), r: &failingReader{strings.NewReader("rest"), disk}, want: disk},
		{name: "context done", ctx: cancelled, r: strings.NewReader(strings.Repeat("rest", 1<<16)), want: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := drainEntry(tt.ctx, tt.r)
			if tt.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.want)
			}
		})
	}
}
