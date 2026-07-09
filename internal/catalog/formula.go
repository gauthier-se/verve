package catalog

// Term is one Formula operand: a Catalog slug weighted by a coefficient (e.g. the
// Atwater factor 4 for protein). Pure data, no closures (ADR 0014).
type Term struct {
	Metric      string  // operand's canonical Catalog slug
	Coefficient float64 // weight within its sum
}

// Formula is a derived Metric's definition: a ratio of two weighted sums times a
// constant — (k · Σ numᵢ) / (Σ dénⱼ), an empty Denominator meaning 1. Not a general
// expression: no nesting, no precedence (ADR 0014). Every operand is required; a
// bucket missing any is a gap, never a zero.
type Formula struct {
	Scale       float64 // constant k (1, or 100 for a percentage)
	Numerator   []Term  // weighted sum on top
	Denominator []Term  // weighted sum below; empty means 1
}

// Evaluate combines one bucket's operand values into the derived value, or returns
// (0, false) for a gap: any missing operand — or a zero denominator — is a gap,
// never a zero (ADR 0014).
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

// Operands returns the distinct operand slugs the Formula references, numerator first.
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
