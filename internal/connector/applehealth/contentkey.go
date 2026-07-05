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

// contentKey is the deduplication identity of a Measurement (ADR 0006): a hash
// of (metric, source, start, end, value, unit). creationDate is deliberately
// excluded because it shifts between exports, so re-importing a later snapshot
// of the same reading yields the same key and is skipped. The raw value string
// (not the normalized float) is hashed so the key is byte-stable and free of
// float-formatting ambiguity.
func contentKey(metric, source, start, end, rawValue, rawUnit string) string {
	sum := sha256.Sum256([]byte(strings.Join(
		[]string{metric, source, start, end, rawValue, rawUnit}, keySep)))
	return hex.EncodeToString(sum[:])
}
