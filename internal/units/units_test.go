package units

import (
	"math"
	"testing"
)

func TestConvertIdentity(t *testing.T) {
	// Opaque units (no alternate in the table) must still normalize to
	// themselves via the identity fast-path.
	for _, u := range []string{"count/min", "W", "dBASPL", "kcal/hr·kg", "mL/min·kg", "kg"} {
		got, err := Convert(42, u, u)
		if err != nil {
			t.Errorf("Convert(42, %q, %q): unexpected error %v", u, u, err)
		}
		if got != 42 {
			t.Errorf("Convert(42, %q, %q) = %v, want 42", u, u, got)
		}
	}
}

func TestConvertKnownUnits(t *testing.T) {
	const eps = 1e-9
	tests := []struct {
		name     string
		value    float64
		from, to string
		want     float64
	}{
		{"kg to g", 1, "kg", "g", 1000},
		{"g to mg", 1, "g", "mg", 1000},
		{"mcg to mg", 1000, "mcg", "mg", 1},
		{"km to m", 2, "km", "m", 2000},
		{"cm to m", 150, "cm", "m", 1.5},
		{"kcal to cal", 1, "kcal", "cal", 1000},
		{"L to mL", 1, "L", "mL", 1000},
		{"hr to min", 1, "hr", "min", 60},
		{"min to s", 1, "min", "s", 60},
		{"ms to s", 500, "ms", "s", 0.5},
		{"km/hr to m/s", 3.6, "km/hr", "m/s", 1},
		{"lb to kg", 1, "lb", "kg", 0.45359237},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Convert(tc.value, tc.from, tc.to)
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if math.Abs(got-tc.want) > eps {
				t.Errorf("Convert(%v, %q, %q) = %v, want %v", tc.value, tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestConvertTemperature(t *testing.T) {
	const eps = 1e-9
	tests := []struct {
		value    float64
		from, to string
		want     float64
	}{
		{32, "degF", "degC", 0},
		{212, "degF", "degC", 100},
		{100, "degC", "degF", 212},
		{0, "degC", "K", 273.15},
	}
	for _, tc := range tests {
		got, err := Convert(tc.value, tc.from, tc.to)
		if err != nil {
			t.Fatalf("Convert(%v,%q,%q): %v", tc.value, tc.from, tc.to, err)
		}
		if math.Abs(got-tc.want) > eps {
			t.Errorf("Convert(%v, %q, %q) = %v, want %v", tc.value, tc.from, tc.to, got, tc.want)
		}
	}
}

func TestConvertIncompatible(t *testing.T) {
	cases := [][2]string{
		{"kg", "m"},        // different dimension
		{"kg", "degC"},     // mass vs temperature
		{"count/min", "W"}, // two distinct opaque units
		{"kg", "furlong"},  // unknown target
	}
	for _, c := range cases {
		if _, err := Convert(1, c[0], c[1]); err == nil {
			t.Errorf("Convert(1, %q, %q) = nil error, want error", c[0], c[1])
		}
	}
}
