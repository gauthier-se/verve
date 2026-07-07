package catalog

// Term is one operand of a Formula: a Catalog Metric slug weighted by a
// coefficient (e.g. the Atwater factor 4 for protein). Pure data — a Formula
// carries no closures, so a compiled definition today can back a user-defined
// editor later without a rewrite (ADR 0014).
type Term struct {
	// Metric is the operand's canonical Catalog slug.
	Metric string
	// Coefficient scales the operand within its weighted sum.
	Coefficient float64
}

// Formula is the declarative definition of a derived Metric: a ratio of two
// weighted sums times a constant scale — (k · Σ numᵢ) / (Σ dénⱼ). An empty
// Denominator means 1 (a plain weighted sum, e.g. total_energy_expenditure).
// Deliberately not a general expression: no nesting, no operator precedence
// (ADR 0014). Every operand is required; a bucket missing any operand — or the
// whole denominator — is a gap, never a zero.
type Formula struct {
	// Scale is the constant k: 1 for none, 100 to turn a fraction into a
	// percentage.
	Scale float64
	// Numerator is the weighted sum on top. Its terms share a unit.
	Numerator []Term
	// Denominator is the weighted sum below; empty means the denominator is 1.
	Denominator []Term
}

// Evaluate combines one bucket's operand values into the derived value:
// (k · Σ aᵢ·numᵢ) / (Σ bⱼ·dénⱼ). The values map holds the aggregated value of
// each operand slug for a single bucket; an operand absent from the map has no
// data in that bucket. It returns the value and true, or (0, false) for a gap.
//
// Every operand is required: a numerator or denominator term whose operand is
// missing yields a gap, as does a zero (or absent-defaulted-to-zero) denominator
// — a missing operand is never treated as zero (ADR 0014).
func (f Formula) Evaluate(values map[string]float64) (float64, bool) {
	num := 0.0
	for _, t := range f.Numerator {
		v, ok := values[t.Metric]
		if !ok {
			return 0, false
		}
		num += t.Coefficient * v
	}

	den := 1.0
	if len(f.Denominator) > 0 {
		den = 0
		for _, t := range f.Denominator {
			v, ok := values[t.Metric]
			if !ok {
				return 0, false
			}
			den += t.Coefficient * v
		}
		if den == 0 {
			return 0, false // an undefined ratio is a gap, not a zero
		}
	}

	return f.Scale * num / den, true
}

// Operands returns every distinct operand slug the Formula references, numerator
// terms first. The engine resolves each independently by its own Source priority
// (ADR 0003); the Catalog validates each against the Catalog at build time.
func (f Formula) Operands() []string {
	seen := make(map[string]bool)
	slugs := make([]string, 0, len(f.Numerator)+len(f.Denominator))
	for _, t := range f.Numerator {
		if !seen[t.Metric] {
			seen[t.Metric] = true
			slugs = append(slugs, t.Metric)
		}
	}
	for _, t := range f.Denominator {
		if !seen[t.Metric] {
			seen[t.Metric] = true
			slugs = append(slugs, t.Metric)
		}
	}
	return slugs
}
