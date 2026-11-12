package route_test

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/workouts/route"
)

// newCodec returns a codec with the zstd options of the application.
func newCodec(t testing.TB) *route.Codec {
	t.Helper()

	enc, err := zstd.NewWriter(nil)
	require.NoError(t, err)

	dec, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(64<<20))
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, enc.Close())
		dec.Close()
	})

	return route.NewCodec(enc, dec)
}

// ptr returns a pointer to v.
func ptr(v float64) *float64 {
	return &v
}

// roundTrip encodes points and decodes the result.
func roundTrip(t *testing.T, points []route.Point) []route.Point {
	t.Helper()

	codec := newCodec(t)

	data, err := codec.Encode(points)
	require.NoError(t, err)

	decoded, err := codec.Decode(data)
	require.NoError(t, err)

	return decoded
}

// Given route points, when they are encoded and decoded, then every field equals the point rounded
// to the precisions of ADR 0014, absent optional values included.
func TestCodec_RoundTrip_RoundsEveryFieldToItsPrecision(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 3, 14, 7, 0, 0, 0, time.UTC)

	points := []route.Point{
		{
			Time:               start.Add(1500 * time.Microsecond),
			Lat:                0.123456789,
			Lon:                -0.987654321,
			Altitude:           ptr(-12.345),
			HorizontalAccuracy: ptr(4.567),
			VerticalAccuracy:   ptr(3.0001),
			Speed:              ptr(2.71828),
			Course:             ptr(359.996),
		},
		{
			Time: start.Add(time.Second + 499*time.Microsecond),
			Lat:  0.12345671,
			Lon:  -0.98765426,
		},
		{
			Time:     start.Add(2*time.Second + 250*time.Microsecond).In(time.FixedZone("CET", 3600)),
			Lat:      -0.00000005,
			Lon:      179.99999996,
			Altitude: ptr(0.125),
			Speed:    ptr(0.004),
			Course:   ptr(0),
		},
	}

	want := []route.Point{
		{
			Time:               start.Add(2 * time.Millisecond),
			Lat:                0.1234568,
			Lon:                -0.9876543,
			Altitude:           ptr(-12.35),
			HorizontalAccuracy: ptr(4.57),
			VerticalAccuracy:   ptr(3),
			Speed:              ptr(2.72),
			Course:             ptr(360),
		},
		{
			Time: start.Add(time.Second),
			Lat:  0.1234567,
			Lon:  -0.9876543,
		},
		{
			Time:     start.Add(2 * time.Second),
			Lat:      -0.0000001,
			Lon:      180,
			Altitude: ptr(0.13),
			Speed:    ptr(0),
			Course:   ptr(0),
		},
	}

	require.Equal(t, want, roundTrip(t, points))
}

// Given a negative accuracy, speed or course, which Core Location uses for an invalid value, when
// the point is encoded and decoded, then the field is absent. A negative altitude stays.
func TestCodec_RoundTrip_NegativeInvalidValuesComeBackAbsent(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 3, 14, 7, 0, 0, 0, time.UTC)

	points := []route.Point{
		{
			Time:               at,
			Lat:                0.001,
			Lon:                0.002,
			Altitude:           ptr(-3),
			HorizontalAccuracy: ptr(-1),
			VerticalAccuracy:   ptr(-1),
			Speed:              ptr(-1),
			Course:             ptr(-1),
		},
		// Negative, although it rounds to 0 cm/s.
		{Time: at.Add(time.Second), Lat: 0.001, Lon: 0.002, Speed: ptr(-0.004)},
	}

	want := []route.Point{
		{Time: at, Lat: 0.001, Lon: 0.002, Altitude: ptr(-3)},
		{Time: at.Add(time.Second), Lat: 0.001, Lon: 0.002},
	}

	require.Equal(t, want, roundTrip(t, points))
}

// Given an empty route or a single point, when it is encoded and decoded, then encoding works.
func TestCodec_RoundTrip_EmptyRouteAndSinglePoint(t *testing.T) {
	t.Parallel()

	single := []route.Point{{Time: time.UnixMilli(1_700_000_000_000).UTC(), Lat: -0.5, Lon: 0.5, Speed: ptr(1.5)}}

	require.Empty(t, roundTrip(t, nil))
	require.Equal(t, single, roundTrip(t, single))
}

// Given a latitude or longitude out of range, or a value that is not finite, when the route is
// encoded, then Encode returns an error.
func TestCodec_Encode_InvalidValue_ReturnsError(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 3, 14, 7, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		point route.Point
	}{
		{name: "latitude above 90", point: route.Point{Time: at, Lat: 90.1}},
		{name: "latitude NaN", point: route.Point{Time: at, Lat: math.NaN()}},
		{name: "longitude below -180", point: route.Point{Time: at, Lon: -180.1}},
		{name: "longitude infinite", point: route.Point{Time: at, Lon: math.Inf(1)}},
		{name: "altitude NaN", point: route.Point{Time: at, Altitude: ptr(math.NaN())}},
		{name: "speed infinite", point: route.Point{Time: at, Speed: ptr(math.Inf(1))}},
		{name: "altitude too large", point: route.Point{Time: at, Altitude: ptr(1e300)}},
		{name: "speed negative infinite", point: route.Point{Time: at, Speed: ptr(math.Inf(-1))}},
		{name: "course NaN", point: route.Point{Time: at, Course: ptr(math.NaN())}},
	}

	codec := newCodec(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := codec.Encode([]route.Point{tt.point})
			require.Error(t, err)
		})
	}
}

// Given an encoding, then it starts with the version byte 1.
func TestCodec_Encode_StartsWithVersionByte(t *testing.T) {
	t.Parallel()

	data, err := newCodec(t).Encode([]route.Point{{Time: time.UnixMilli(0), Lat: 1, Lon: 2}})
	require.NoError(t, err)
	require.Equal(t, byte(1), data[0])
}

// Given an unknown version, truncated or corrupt data, when it is decoded, then Decode returns an
// error.
func TestCodec_Decode_InvalidData_ReturnsError(t *testing.T) {
	t.Parallel()

	codec := newCodec(t)

	points := []route.Point{
		{Time: time.UnixMilli(1_000), Lat: 0.1, Lon: 0.2, Altitude: ptr(10), Course: ptr(90)},
		{Time: time.UnixMilli(2_000), Lat: 0.1001, Lon: 0.2001, Altitude: ptr(11), Course: ptr(91)},
	}

	valid, err := codec.Encode(points)
	require.NoError(t, err)

	// compressed returns the encoding of raw columns, which zstd decompresses but which are invalid.
	compressed := func(raw ...byte) []byte {
		enc, err := zstd.NewWriter(nil)
		require.NoError(t, err)

		t.Cleanup(func() { require.NoError(t, enc.Close()) })

		return enc.EncodeAll(raw, []byte{1})
	}

	tests := []struct {
		name string
		data []byte
	}{
		{name: "empty", data: nil},
		{name: "version only", data: []byte{1}},
		{name: "unknown version", data: append([]byte{2}, valid[1:]...)},
		{name: "version zero", data: append([]byte{0}, valid[1:]...)},
		{name: "truncated frame", data: valid[:len(valid)-3]},
		{name: "corrupt frame", data: append(append([]byte{}, valid[:len(valid)/2]...), 0xff, 0xff, 0xff)},
		{name: "not zstd", data: []byte{1, 'g', 'p', 'x'}},
		{name: "no point count", data: compressed()},
		{name: "more points than data", data: compressed(0xff, 0xff, 0xff, 0xff, 0x0f, 0, 0, 0)},
		{name: "missing columns", data: compressed(2, 0, 0, 0, 0, 0, 0)},
		{name: "varint overflow", data: compressed(1, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01, 0, 0)},
		{name: "trailing bytes", data: compressed(0, 0, 0, 0, 0, 0, 0)},
		{name: "more points than MaxPoints", data: compressed(tooManyPoints()...)},
		{name: "bitmap bit past the last point", data: compressed(1, 0, 0, 0, 0x03, 0x02, 0x02, 0, 0, 0, 0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := codec.Decode(tt.data)
			require.Error(t, err)
		})
	}
}

// Given any data, when it is decoded, then Decode returns points or an error, and never panics.
func FuzzDecode(f *testing.F) {
	codec := newCodec(f)

	seeds, err := filepath.Glob(filepath.Join("testdata", "*.route"))
	require.NoError(f, err)

	for _, seed := range seeds {
		data, err := os.ReadFile(seed) //nolint:gosec // G304: a golden file of testdata
		require.NoError(f, err)
		f.Add(data)
	}

	f.Add([]byte{})
	f.Add([]byte{1})
	f.Add([]byte{2, 0x28, 0xb5, 0x2f, 0xfd})

	f.Fuzz(func(t *testing.T, data []byte) {
		points, err := codec.Decode(data)
		if err != nil {
			require.Nil(t, points)
		}
	})
}

// tooManyPoints returns the decompressed encoding of MaxPoints + 1 points at the same time and
// place, without optional fields, which is valid but for its count.
func tooManyPoints() []byte {
	const n = route.MaxPoints + 1

	raw := binary.AppendUvarint(nil, n)

	return append(raw, make([]byte, 3*n+5*((n+7)/8))...)
}

// Given more than MaxPoints points, when the route is encoded, then Encode returns an error.
func TestCodec_Encode_MoreThanMaxPoints_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := newCodec(t).Encode(make([]route.Point, route.MaxPoints+1))
	require.Error(t, err)
}

// Given times before the Unix epoch, when they are encoded and decoded, then they are rounded to
// the millisecond halves away from zero.
func TestCodec_RoundTrip_TimesBeforeEpochRoundAwayFromZero(t *testing.T) {
	t.Parallel()

	points := []route.Point{
		{Time: time.Unix(0, -1_500_000)},
		{Time: time.Unix(0, -500_000)},
		{Time: time.Unix(0, -400_000)},
	}

	want := []route.Point{
		{Time: time.UnixMilli(-2).UTC()},
		{Time: time.UnixMilli(-1).UTC()},
		{Time: time.UnixMilli(0).UTC()},
	}

	require.Equal(t, want, roundTrip(t, points))
}

// Given a route, then its encoding is the version byte and a zstd frame of the point count and the
// columns of ADR 0014, here computed by hand.
func TestCodec_Encode_FollowsTheLayoutOfADR(t *testing.T) {
	t.Parallel()

	points := []route.Point{
		{Time: time.UnixMilli(1000), Lat: 0.0000001, Lon: -0.0000002, Altitude: ptr(0.01)},
		{Time: time.UnixMilli(2000), Lat: 0.0000003, Lon: -0.0000002, Speed: ptr(0.05)},
	}

	data, err := newCodec(t).Encode(points)
	require.NoError(t, err)
	require.Equal(t, byte(1), data[0])

	dec, err := zstd.NewReader(nil)
	require.NoError(t, err)

	defer dec.Close()

	raw, err := dec.DecodeAll(data[1:], nil)
	require.NoError(t, err)

	want := []byte{
		0x02,                   // 2 points
		0xd0, 0x0f, 0xd0, 0x0f, // time: deltas 1000 and 1000 ms, zigzag 2000
		0x02, 0x04, // latitude: deltas 1 and 2
		0x03, 0x00, // longitude: deltas -2 and 0
		0x01, 0x02, // altitude: first point only, delta 1 cm
		0x00,       // horizontal accuracy: absent
		0x00,       // vertical accuracy: absent
		0x02, 0x0a, // speed: second point only, delta 5 cm/s
		0x00, // course: absent
	}
	require.Equal(t, want, raw)
}
