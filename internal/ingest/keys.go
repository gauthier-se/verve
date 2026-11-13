package ingest

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/store"
)

// The natural keys of FR-1 of Deduplication and source priority, as SHA-256 hashes of their fields:
// the store only compares them. Each key starts with a tag of its kind, so that keys of different
// kinds never meet, and a uuid is compared whatever its case.

// Tags of the kinds of natural keys.
const (
	keySampleUUID  = "sample-uuid"
	keySampleRaw   = "sample-raw"
	keySamplePoint = "sample-point"
	keyWorkoutID   = "workout-id"
	keyWorkoutNoID = "workout-no-id"
)

// sampleRule returns the conflict rule of a sample: update if changed for a sample with a uuid,
// replace for a pre-aggregated point, and ignore for a raw sample.
func sampleRule(uuid string, preAggregated bool) store.ConflictRule {
	switch {
	case uuid != "":
		return store.ConflictUpdateIfChanged
	case preAggregated:
		return store.ConflictReplace
	default:
		return store.ConflictIgnore
	}
}

// sampleKey returns the natural key of s: from its uuid, from its type, source, start and end for
// a pre-aggregated point, and from those, its value and its category value for a raw sample.
func sampleKey(s store.Sample, preAggregated bool) []byte {
	var k key

	switch {
	case s.UUID != "":
		return sampleUUIDKey(s.UUID)
	case preAggregated:
		k.text(keySamplePoint).int(s.TypeID).int(s.SourceID).time(s.Start).time(s.End)
	default:
		k.text(keySampleRaw).int(s.TypeID).int(s.SourceID).time(s.Start).time(s.End).
			float(s.Value).text(s.Category)
	}

	return k.sum()
}

// workoutRule returns the conflict rule of a workout: update for a workout with an external id, and
// ignore otherwise.
func workoutRule(w store.Workout) store.ConflictRule {
	if w.ExternalID != "" {
		return store.ConflictUpdate
	}

	return store.ConflictIgnore
}

// workoutKey returns the natural key of w: from its origin and external id, or from its activity
// type, source, start and end when it has no external id.
func workoutKey(w store.Workout) []byte {
	if w.ExternalID != "" {
		return workoutIDKey(w.Origin, w.ExternalID)
	}

	var k key

	return k.text(keyWorkoutNoID).text(w.Activity).int(w.SourceID).time(w.Start).time(w.End).sum()
}

// sampleUUIDKey returns the natural key of the sample of uuid, whatever its origin.
func sampleUUIDKey(uuid string) []byte {
	var k key

	return k.text(keySampleUUID).text(strings.ToUpper(uuid)).sum()
}

// workoutIDKey returns the natural key of the workout of origin whose external id is id.
func workoutIDKey(origin store.Origin, id string) []byte {
	var k key

	return k.text(keyWorkoutID).text(string(origin)).text(strings.ToUpper(id)).sum()
}

// key builds the input of a natural key hash, each field self-delimiting, by its length or its
// encoding, so that no two lists of fields give the same input.
type key struct {
	buf []byte
}

// text appends a string with its length.
func (k *key) text(s string) *key {
	k.buf = binary.AppendUvarint(k.buf, uint64(len(s)))
	k.buf = append(k.buf, s...)

	return k
}

// int appends an id, as a varint.
func (k *key) int(v int64) *key {
	k.buf = binary.AppendVarint(k.buf, v)

	return k
}

// time appends a time, at the millisecond precision of stored times.
func (k *key) time(t time.Time) *key {
	return k.int(t.UnixMilli())
}

// float appends a value, with its negative zero as zero.
func (k *key) float(v float64) *key {
	if v == 0 {
		v = 0
	}

	k.buf = binary.BigEndian.AppendUint64(k.buf, math.Float64bits(v))

	return k
}

// sum returns the hash of the fields.
func (k *key) sum() []byte {
	sum := sha256.Sum256(k.buf)

	return sum[:]
}
