package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Given sizes `10GB`, `512MiB` and `1048576`, then they parse to
// 10,000,000,000, 536,870,912 and 1,048,576 bytes.
func TestParseByteSize_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want ByteSize
	}{
		{in: "10GB", want: 10_000_000_000},
		{in: "512MiB", want: 536_870_912},
		{in: "1048576", want: 1_048_576},
		{in: "0", want: 0},
		{in: "1KB", want: 1_000},
		{in: "1MB", want: 1_000_000},
		{in: "2TB", want: 2_000_000_000_000},
		{in: "1KiB", want: 1_024},
		{in: "3GiB", want: 3 * 1024 * 1024 * 1024},
		{in: "1TiB", want: 1024 * 1024 * 1024 * 1024},
		{in: "10gb", want: 10_000_000_000},
		{in: "512mib", want: 536_870_912},
		{in: " 1GB ", want: 1_000_000_000},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			got, err := ParseByteSize(tt.in)

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// Given `10XB` or `-1`, then the size is invalid.
func TestParseByteSize_Invalid(t *testing.T) {
	t.Parallel()

	tests := []string{
		"10XB",
		"-1",
		"",
		"GB",
		"1.5GB",
		"10 G B",
		"9999999999TB",
		"9223372036854775808",
	}

	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			_, err := ParseByteSize(in)

			require.Error(t, err)
			require.Contains(t, err.Error(), `invalid value "`+in+`"`)
		})
	}
}

// Given a size, then String returns a human-readable value that parses back to the same size.
func TestByteSize_StringRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   ByteSize
		want string
	}{
		{in: 0, want: "0"},
		{in: 1_023, want: "1023"},
		{in: 1_000, want: "1KB"},
		{in: 1_024, want: "1KiB"},
		{in: 1_048_576, want: "1MiB"},
		{in: 536_870_912, want: "512MiB"},
		{in: 1_000_000_000, want: "1GB"},
		{in: 10_000_000_000, want: "10GB"},
		{in: 1024 * 1024 * 1024 * 1024, want: "1TiB"},
		{in: 2_000_000_000_000, want: "2TB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.in.String())

			back, err := ParseByteSize(tt.in.String())
			require.NoError(t, err)
			require.Equal(t, tt.in, back)
		})
	}
}

// Given a size as text, then it unmarshals and marshals through the text interfaces
// used by the YAML and environment decoders.
func TestByteSize_Text(t *testing.T) {
	t.Parallel()

	var size ByteSize
	require.NoError(t, size.UnmarshalText([]byte("512MiB")))
	require.Equal(t, ByteSize(536_870_912), size)

	text, err := size.MarshalText()
	require.NoError(t, err)
	require.Equal(t, "512MiB", string(text))

	require.Error(t, size.UnmarshalText([]byte("10XB")))
}
