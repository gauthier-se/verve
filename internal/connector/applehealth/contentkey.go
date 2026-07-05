package applehealth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// keySep separates fields inside a content-key preimage. It is a control
// character that never occurs in the hashed values, so distinct field tuples
// can never collide by concatenation (e.g. "a"+"bc" vs "ab"+"c").
const keySep = "\x1f"

// hashKey hashes a field tuple into a hex content key. Fields are joined with
// keySep so distinct tuples can never collide by concatenation.
func hashKey(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, keySep)))
	return hex.EncodeToString(sum[:])
}

// contentKey is the deduplication identity of a Measurement (ADR 0006): a hash
// of (metric, source, start, end, value, unit). creationDate is deliberately
// excluded because it shifts between exports, so re-importing a later snapshot
// of the same reading yields the same key and is skipped. The raw value string
// (not the normalized float) is hashed so the key is byte-stable and free of
// float-formatting ambiguity.
func contentKey(metric, source, start, end, rawValue, rawUnit string) string {
	return hashKey(metric, source, start, end, rawValue, rawUnit)
}

// stateContentKey is a State's dedup identity: a hash of
// (kind, state_value, source, start, end). The "state" prefix keeps it disjoint
// from other families' keys even though States have their own table.
func stateContentKey(kind, stateValue, source, start, end string) string {
	return hashKey("state", kind, stateValue, source, start, end)
}

// sessionContentKey is a Session's dedup identity: a hash of
// (activity_type, source, start, end) — a workout's stable identity across
// re-exports, with creationDate excluded like every other family (ADR 0006).
func sessionContentKey(activityType, source, start, end string) string {
	return hashKey("session", activityType, source, start, end)
}
