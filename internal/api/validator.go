package api

// Validator accumulates per-field validation errors so a handler can check
// several parameters and report them all at once, keyed by field name.
type Validator struct {
	Errors map[string]string
}

// NewValidator returns an empty Validator.
func NewValidator() *Validator {
	return &Validator{Errors: make(map[string]string)}
}

// Valid reports whether no errors have been recorded.
func (v *Validator) Valid() bool {
	return len(v.Errors) == 0
}

// AddError records message for key, keeping the first message if key repeats.
func (v *Validator) AddError(key, message string) {
	if _, exists := v.Errors[key]; !exists {
		v.Errors[key] = message
	}
}

// Check records message for key unless ok is true — the ergonomic one-liner for
// a single constraint.
func (v *Validator) Check(ok bool, key, message string) {
	if !ok {
		v.AddError(key, message)
	}
}
