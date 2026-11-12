package route

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"time"

	"github.com/klauspost/compress/zstd"
)

// MaxPoints is the largest number of points of a route, about 12 days at one point per second. It
// bounds the memory that decoding corrupt data takes.
const MaxPoints = 1 << 20

// version is the first byte of an encoding.
const version = 1

// Precisions of the fields, in units per unit of Point.
const (
	degreeUnits = 1e7 // 1e-7 degree
	centiUnits  = 1e2 // centimeter, centimeter per second, hundredth of a degree
)

// field is an optional field of a point.
type field struct {
	name string
	// signed reports whether a negative value is valid. Core Location uses a negative accuracy,
	// speed or course for an invalid value.
	signed bool
	value  func(*Point) **float64
}

// optionalFields are the optional fields, in the order of their columns.
var optionalFields = []field{
	{name: "altitude", signed: true, value: func(p *Point) **float64 { return &p.Altitude }},
	{name: "horizontal accuracy", value: func(p *Point) **float64 { return &p.HorizontalAccuracy }},
	{name: "vertical accuracy", value: func(p *Point) **float64 { return &p.VerticalAccuracy }},
	{name: "speed", value: func(p *Point) **float64 { return &p.Speed }},
	{name: "course", value: func(p *Point) **float64 { return &p.Course }},
}

// errTruncated is the error of an encoding that ends before its last column.
var errTruncated = errors.New("truncated data")

// Codec encodes and decodes routes. It is safe for concurrent use.
type Codec struct {
	enc *zstd.Encoder
	dec *zstd.Decoder
}

// NewCodec returns a codec that compresses with enc and decompresses with dec, through EncodeAll
// and DecodeAll. dec should limit its memory with zstd.WithDecoderMaxMemory.
func NewCodec(enc *zstd.Encoder, dec *zstd.Decoder) *Codec {
	return &Codec{enc: enc, dec: dec}
}

// Encode returns the encoding of points (ADR 0014): the version byte, then a zstd frame of the
// point count and one delta encoded column per field. A negative accuracy, speed or course is
// encoded as absent. It fails on a latitude, longitude or optional value that is not finite, and on
// a latitude or longitude out of range. It fails as well on more than MaxPoints points.
func (c *Codec) Encode(points []Point) ([]byte, error) {
	n := len(points)
	if n > MaxPoints {
		return nil, fmt.Errorf("encode route: %d points, more than %d", n, MaxPoints)
	}

	raw := binary.AppendUvarint(nil, uint64(n))

	times := make([]int64, n)
	lats := make([]int64, n)
	lons := make([]int64, n)

	for i, p := range points {
		if math.IsNaN(p.Lat) || p.Lat < -90 || p.Lat > 90 {
			return nil, fmt.Errorf("encode route: point %d: latitude %v out of range", i, p.Lat)
		}

		if math.IsNaN(p.Lon) || p.Lon < -180 || p.Lon > 180 {
			return nil, fmt.Errorf("encode route: point %d: longitude %v out of range", i, p.Lon)
		}

		times[i] = unixMilli(p.Time)
		lats[i] = int64(math.Round(p.Lat * degreeUnits))
		lons[i] = int64(math.Round(p.Lon * degreeUnits))
	}

	raw = appendDeltas(raw, times)
	raw = appendDeltas(raw, lats)
	raw = appendDeltas(raw, lons)

	for _, f := range optionalFields {
		bitmap := make([]byte, (n+7)/8)
		values := make([]int64, 0, n)

		for i := range points {
			v := *f.value(&points[i])
			if v == nil {
				continue
			}

			units, ok := toUnits(*v, centiUnits)
			if !ok {
				return nil, fmt.Errorf("encode route: point %d: invalid %s %v", i, f.name, *v)
			}

			if !f.signed && *v < 0 {
				continue
			}

			bitmap[i/8] |= 1 << (i % 8)
			values = append(values, units)
		}

		raw = append(raw, bitmap...)
		raw = appendDeltas(raw, values)
	}

	return c.enc.EncodeAll(raw, []byte{version}), nil
}

// Decode returns the points of an encoding of Encode, every field at the precision of its column.
// Times are in UTC. It fails on an unknown version, on more than MaxPoints points and on truncated
// or corrupt data.
func (c *Codec) Decode(data []byte) ([]Point, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("decode route: %w", errTruncated)
	}

	if data[0] != version {
		return nil, fmt.Errorf("decode route: unknown version %d", data[0])
	}

	raw, err := c.dec.DecodeAll(data[1:], nil)
	if err != nil {
		return nil, fmt.Errorf("decode route: %w", err)
	}

	points, err := decodeColumns(raw)
	if err != nil {
		return nil, fmt.Errorf("decode route: %w", err)
	}

	return points, nil
}

// decodeColumns returns the points of a decompressed encoding.
func decodeColumns(raw []byte) ([]Point, error) {
	r := reader{buf: raw}

	count := r.uvarint()
	if r.err == nil && count > MaxPoints {
		return nil, fmt.Errorf("%d points, more than %d", count, MaxPoints)
	}

	// Every point takes at least one byte in each required column.
	if r.err == nil && count > uint64(len(r.buf))/3 {
		return nil, fmt.Errorf("%d points in %d bytes: %w", count, len(r.buf), errTruncated)
	}

	n := int(count) //nolint:gosec // G115: bounded by MaxPoints above
	points := make([]Point, n)

	for i, v := range r.deltas(n) {
		points[i].Time = time.UnixMilli(v).UTC()
	}

	for i, v := range r.deltas(n) {
		points[i].Lat = float64(v) / degreeUnits
	}

	for i, v := range r.deltas(n) {
		points[i].Lon = float64(v) / degreeUnits
	}

	for _, f := range optionalFields {
		bitmap := r.bytes((n + 7) / 8)

		if n%8 != 0 && bitmap[len(bitmap)-1]>>(n%8) != 0 {
			return nil, fmt.Errorf("%s bitmap: bit set past the last point", f.name)
		}

		present := 0
		for _, b := range bitmap {
			present += bits.OnesCount8(b)
		}

		values := r.deltas(present)

		for i := range points {
			if bitmap[i/8]&(1<<(i%8)) == 0 {
				continue
			}

			v := float64(values[0]) / centiUnits
			values = values[1:]
			*f.value(&points[i]) = &v
		}
	}

	if r.err != nil {
		return nil, r.err
	}

	if len(r.buf) > 0 {
		return nil, fmt.Errorf("%d bytes after the last column", len(r.buf))
	}

	return points, nil
}

// unixMilli returns t in milliseconds since the Unix epoch, rounded halves away from zero.
func unixMilli(t time.Time) int64 {
	const half = int(time.Millisecond / 2)

	ms := t.Unix()*1000 + int64(t.Nanosecond()/int(time.Millisecond))
	rem := t.Nanosecond() % int(time.Millisecond)

	if rem > half || (rem == half && ms >= 0) {
		ms++
	}

	return ms
}

// toUnits returns v in units of 1/scale, rounded halves away from zero, and false when the result
// is not an int64.
func toUnits(v, scale float64) (int64, bool) {
	units := math.Round(v * scale)
	if math.IsNaN(units) || units < math.MinInt64 || units >= math.MaxInt64 {
		return 0, false
	}

	return int64(units), true
}

// appendDeltas appends the differences between consecutive values, the first from zero, as zigzag
// encoded varints. binary.AppendVarint zigzag encodes, then writes an unsigned varint.
func appendDeltas(buf []byte, values []int64) []byte {
	var prev int64

	for _, v := range values {
		buf = binary.AppendVarint(buf, v-prev)
		prev = v
	}

	return buf
}

// reader reads a decompressed encoding. After the first error, it returns zero values and keeps
// that error.
type reader struct {
	buf []byte
	err error
}

// uvarint reads an unsigned varint.
func (r *reader) uvarint() uint64 {
	if r.err != nil {
		return 0
	}

	v, n := binary.Uvarint(r.buf)
	if n <= 0 {
		r.fail(n)
		return 0
	}

	r.buf = r.buf[n:]

	return v
}

// deltas reads n delta encoded values, and returns them summed. It returns n zeros after an error.
func (r *reader) deltas(n int) []int64 {
	values := make([]int64, n)

	var prev int64

	for i := range values {
		if r.err != nil {
			return values
		}

		d, size := binary.Varint(r.buf)
		if size <= 0 {
			r.fail(size)
			return values
		}

		r.buf = r.buf[size:]
		prev += d
		values[i] = prev
	}

	return values
}

// bytes reads n bytes. It returns n zero bytes after an error.
func (r *reader) bytes(n int) []byte {
	if r.err == nil && len(r.buf) < n {
		r.err = errTruncated
	}

	if r.err != nil {
		return make([]byte, n)
	}

	b := r.buf[:n]
	r.buf = r.buf[n:]

	return b
}

// fail records the error of a varint read that returned size.
func (r *reader) fail(size int) {
	if size == 0 {
		r.err = errTruncated
		return
	}

	r.err = errors.New("varint overflows 64 bits")
}
