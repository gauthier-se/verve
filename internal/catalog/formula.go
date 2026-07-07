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
