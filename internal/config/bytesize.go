package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ByteSize is a size in bytes. As text it is a plain integer or an integer followed by a
// decimal unit (KB, MB, GB, TB, powers of 1000) or a binary unit (KiB, MiB, GiB, TiB,
// powers of 1024), case-insensitive.
type ByteSize int64

// byteUnits is ordered from largest to smallest, so String picks the largest exact unit.
var byteUnits = []struct {
	suffix string
	size   int64
}{
	{suffix: "TiB", size: 1 << 40},
	{suffix: "TB", size: 1_000_000_000_000},
	{suffix: "GiB", size: 1 << 30},
	{suffix: "GB", size: 1_000_000_000},
	{suffix: "MiB", size: 1 << 20},
	{suffix: "MB", size: 1_000_000},
	{suffix: "KiB", size: 1 << 10},
	{suffix: "KB", size: 1_000},
}

// ParseByteSize parses a size such as "10GB", "512MiB" or "1048576".
func ParseByteSize(s string) (ByteSize, error) {
	trimmed := strings.TrimSpace(s)
	digits := len(trimmed) - len(strings.TrimLeft(trimmed, "0123456789"))
	number, unit := trimmed[:digits], trimmed[digits:]

	if number == "" {
		return 0, invalidValue(s, "must be a whole number of bytes, optionally followed by a unit such as GB or MiB")
	}

	n, err := strconv.ParseInt(number, 10, 64)
	if err != nil {
		return 0, invalidValue(s, "too large")
	}

	if unit == "" {
		return ByteSize(n), nil
	}

	for _, u := range byteUnits {
		if !strings.EqualFold(unit, u.suffix) {
			continue
		}

		if n > math.MaxInt64/u.size {
			return 0, invalidValue(s, "too large")
		}

		return ByteSize(n * u.size), nil
	}

	return 0, invalidValue(s, fmt.Sprintf("unknown unit %q, must be one of KB, MB, GB, TB, KiB, MiB, GiB, TiB", unit))
}

// String returns the size with the largest unit that represents it exactly, for example "10GB".
func (b ByteSize) String() string {
	if b != 0 {
		for _, u := range byteUnits {
			if int64(b)%u.size == 0 {
				return strconv.FormatInt(int64(b)/u.size, 10) + u.suffix
			}
		}
	}

	return strconv.FormatInt(int64(b), 10)
}

// MarshalText implements encoding.TextMarshaler.
func (b ByteSize) MarshalText() ([]byte, error) {
	return []byte(b.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (b *ByteSize) UnmarshalText(text []byte) error {
	size, err := ParseByteSize(string(text))
	if err != nil {
		return err
	}

	*b = size

	return nil
}
