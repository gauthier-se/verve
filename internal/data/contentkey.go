package data

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Content keys are the deduplication identity of a stored row (ADR 0006). They
// live here, beside the models they identify, rather than in a Connector: the
// identity is a property of Verve's storage model, not of the source that
// happens to produce a row. A Manual entry is keyed by the very same function as
// an imported one, so typing a value twice is idempotent exactly as re-importing
// is (ADR 0022).

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

// ContentKey is the deduplication identity of a Measurement (ADR 0006): a hash
// of (metric, source, start, end, value, unit). creationDate is deliberately
// excluded because it shifts between exports, so re-importing a later snapshot
// of the same reading yields the same key and is skipped. The raw value string
// (not the normalized float) is hashed so the key is byte-stable and free of
// float-formatting ambiguity.
func ContentKey(metric, source, start, end, rawValue, rawUnit string) string {
	return hashKey(metric, source, start, end, rawValue, rawUnit)
}

// StateContentKey is a State's dedup identity: a hash of
// (kind, state_value, source, start, end). The "state" prefix keeps it disjoint
// from other families' keys even though States have their own table.
func StateContentKey(kind, stateValue, source, start, end string) string {
	return hashKey("state", kind, stateValue, source, start, end)
}

// SessionContentKey is a Session's dedup identity: a hash of
// (activity_type, source, start, end) — a workout's stable identity across
// re-exports, with creationDate excluded like every other family (ADR 0006).
func SessionContentKey(activityType, source, start, end string) string {
	return hashKey("session", activityType, source, start, end)
}
