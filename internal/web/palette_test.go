package web

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The Appearance stylesheet and the TypeScript roster that must agree with it.
// Both are read as text from the repo, because the contract spans two languages
// and neither side can assert it alone: the SPA has its own suite now (`make
// ui-test`), but a vitest run cannot see a Go file and a Go test cannot execute
// TypeScript. Reading both as text is what is left, and it is enough here because
// what must agree is a set of names and a set of colour values, not a behaviour.
const (
	stylesheetPath = "../../web/src/index.css"
	rosterPath     = "../../web/src/components/appearance.tsx"
)

// semanticTokens carry meaning rather than style and are defined once, under
// :root and .dark. A palette that restates one can invert the reading of a
// calorie balance or of a destructive action (ADR 0024).
var semanticTokens = map[string]bool{
	"destructive":            true,
	"destructive-foreground": true,
	"chart-positive":         true,
	"chart-negative":         true,
}

var (
	// The light block of a palette. Verve's doubles as :root, so it is matched
	// by the same expression: `:root,\n  [data-palette="verve"] {`.
	lightBlockRe = regexp.MustCompile(`(?m)^\s*(?:\:root,\s*\n\s*)?\[data-palette="([a-z]+)"\] \{`)
	darkBlockRe  = regexp.MustCompile(`(?m)^\s*(?:\.dark,\s*\n\s*)?\.dark\[data-palette="([a-z]+)"\] \{`)
	tokenRe      = regexp.MustCompile(`--([a-z0-9-]+):`)
	rosterRe     = regexp.MustCompile(`\{ id: "([a-z]+)", label: "[^"]+" \}`)
)

// paletteBlocks parses index.css into palette id -> variant -> token set.
func paletteBlocks(t *testing.T) map[string]map[string]map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(stylesheetPath))
	if err != nil {
		t.Fatalf("read %s: %v", stylesheetPath, err)
	}
	css := string(raw)

	blocks := map[string]map[string]map[string]bool{}
	for variant, re := range map[string]*regexp.Regexp{"light": lightBlockRe, "dark": darkBlockRe} {
		for _, loc := range re.FindAllStringSubmatchIndex(css, -1) {
			id := css[loc[2]:loc[3]]
			body := blockBody(css[loc[1]:])
			if blocks[id] == nil {
				blocks[id] = map[string]map[string]bool{}
			}
			tokens := map[string]bool{}
			for _, m := range tokenRe.FindAllStringSubmatch(body, -1) {
				tokens[m[1]] = true
			}
			blocks[id][variant] = tokens
		}
	}
	return blocks
}

// blockBody returns everything up to the first closing brace, which is the whole
// declaration list: these blocks never nest.
func blockBody(after string) string {
	if i := strings.Index(after, "}"); i >= 0 {
		return after[:i]
	}
	return after
}

// TestPaletteTokenSetsAreComplete is the test ADR 0024's own consequences ask for:
// "a token missing from one block silently falls through to Verve's, which is a
// bug and not a fallback". Nothing breaks visibly when it happens, the palette is
// just subtly wrong, which is not something review reliably catches across
// eighteen blocks.
func TestPaletteTokenSetsAreComplete(t *testing.T) {
	blocks := paletteBlocks(t)
	verve, ok := blocks["verve"]
	if !ok {
		t.Fatal("no verve palette found in index.css; the parser or the stylesheet moved")
	}
	want := verve["light"]
	if len(want) < 10 {
		t.Fatalf("verve light block has %d tokens, which cannot be right", len(want))
	}

	for _, id := range sortedKeys(blocks) {
		for _, variant := range []string{"light", "dark"} {
			got, ok := blocks[id][variant]
			if !ok {
				t.Errorf("palette %q has no %s block: a Palette must define both (ADR 0024)", id, variant)
				continue
			}
			for _, token := range sortedKeys(want) {
				if !got[token] {
					t.Errorf("palette %q (%s) is missing --%s: it will silently inherit Verve's", id, variant, token)
				}
			}
			for _, token := range sortedKeys(got) {
				if !want[token] {
					t.Errorf("palette %q (%s) declares --%s, which Verve does not", id, variant, token)
				}
			}
		}
	}
}

// TestPalettesDoNotRestateSemanticTokens guards the other half of ADR 0024: a
// Palette owns style, never meaning. A palette free to repaint the diverging pair
// could decide that a deficit is warm.
func TestPalettesDoNotRestateSemanticTokens(t *testing.T) {
	for id, variants := range paletteBlocks(t) {
		for variant, tokens := range variants {
			for _, token := range sortedKeys(tokens) {
				if semanticTokens[token] {
					t.Errorf("palette %q (%s) declares --%s, which is immune to the Palette (ADR 0024)", id, variant, token)
				}
			}
		}
	}
}

// TestPaletteRosterMatchesStylesheet keeps PALETTES and index.css in step. A
// roster entry with no block renders as Verve; a block with no entry is
// unreachable.
func TestPaletteRosterMatchesStylesheet(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(rosterPath))
	if err != nil {
		t.Fatalf("read %s: %v", rosterPath, err)
	}
	roster := map[string]bool{}
	for _, m := range rosterRe.FindAllStringSubmatch(string(raw), -1) {
		roster[m[1]] = true
	}
	if len(roster) == 0 {
		t.Fatal("no PALETTES entries parsed; the parser or appearance.tsx moved")
	}

	blocks := paletteBlocks(t)
	for _, id := range sortedKeys(roster) {
		if _, ok := blocks[id]; !ok {
			t.Errorf("PALETTES lists %q but index.css defines no block for it: it would paint as Verve", id)
		}
	}
	for _, id := range sortedKeys(blocks) {
		if !roster[id] {
			t.Errorf("index.css defines %q but PALETTES does not list it: it is unreachable", id)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// hsl is a parsed `H S% L%` token value, the only colour format index.css uses.
type hsl struct{ h, s, l float64 }

var hslRe = regexp.MustCompile(`^\s*([\d.]+) ([\d.]+)% ([\d.]+)%\s*$`)

func parseHSL(v string) (hsl, bool) {
	m := hslRe.FindStringSubmatch(v)
	if m == nil {
		return hsl{}, false
	}
	h, _ := strconv.ParseFloat(m[1], 64)
	s, _ := strconv.ParseFloat(m[2], 64)
	l, _ := strconv.ParseFloat(m[3], 64)
	return hsl{h, s / 100, l / 100}, true
}

// rgb converts to sRGB channels in 0..1, per the CSS Color 4 formulation.
func (c hsl) rgb() [3]float64 {
	a := c.s * math.Min(c.l, 1-c.l)
	f := func(n float64) float64 {
		k := math.Mod(n+c.h/30, 12)
		return c.l - a*math.Max(-1, math.Min(k-3, math.Min(9-k, 1)))
	}
	return [3]float64{f(0), f(8), f(4)}
}

// luminance is WCAG relative luminance.
func (c hsl) luminance() float64 {
	ch := c.rgb()
	lin := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(ch[0]) + 0.7152*lin(ch[1]) + 0.0722*lin(ch[2])
}

func contrastRatio(a, b hsl) float64 {
	x, y := a.luminance(), b.luminance()
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

// hueGap is the shorter way round the wheel.
func hueGap(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	return math.Min(d, 360-d)
}

// paletteRamps parses index.css into palette id -> variant -> the block's chart ramp
// and its card, which is what the separation rule is stated against.
func paletteRamps(t *testing.T) map[string]map[string]map[string]hsl {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(stylesheetPath))
	if err != nil {
		t.Fatalf("read %s: %v", stylesheetPath, err)
	}
	css := string(raw)

	out := map[string]map[string]map[string]hsl{}
	for variant, re := range map[string]*regexp.Regexp{"light": lightBlockRe, "dark": darkBlockRe} {
		for _, loc := range re.FindAllStringSubmatchIndex(css, -1) {
			id := css[loc[2]:loc[3]]
			body := blockBody(css[loc[1]:])
			values := map[string]hsl{}
			for _, m := range declRe.FindAllStringSubmatch(body, -1) {
				if c, ok := parseHSL(m[2]); ok {
					values[m[1]] = c
				}
			}
			if out[id] == nil {
				out[id] = map[string]map[string]hsl{}
			}
			out[id][variant] = values
		}
	}
	return out
}

var declRe = regexp.MustCompile(`--([a-z0-9-]+):([^;]+);`)

// chartRamp is every categorical slot, in order. chart-1..4 are the identities of up
// to four Metrics on one Panel (ADR 0020); chart-5..6 extend the ramp for the
// categorical dimensions a Panel does not bound — a Night's Stages, the kinds of
// event on the history rail — and never carry a Series.
var chartRamp = []string{"chart-1", "chart-2", "chart-3", "chart-4", "chart-5", "chart-6"}

// TestChartRampIsSeparated is the separation rule from CONTRIBUTING.md, held by the
// build instead of by a reviewer with a colour picker.
//
// ADR 0026 says the completeness of a palette is tested rather than reviewed, and
// then leaves this half to the eye — which was defensible at four values a palette
// and is not at six across eighteen blocks. Two colours a reader cannot tell apart
// is exactly the silent failure the token-completeness test above exists to catch:
// nothing errors, the chart is just lying about how many things it is showing.
func TestChartRampIsSeparated(t *testing.T) {
	for id, variants := range paletteRamps(t) {
		for _, variant := range []string{"light", "dark"} {
			values, ok := variants[variant]
			if !ok {
				continue // TestPaletteTokenSetsAreComplete owns the missing-block error.
			}
			card, ok := values["card"]
			if !ok {
				t.Errorf("palette %q (%s) has no --card to check the ramp against", id, variant)
				continue
			}
			for i, name := range chartRamp {
				c, ok := values[name]
				if !ok {
					t.Errorf("palette %q (%s) has no --%s", id, variant, name)
					continue
				}
				// 3:1 is the WCAG floor for a graphical object: a curve the card
				// swallows is a Metric the Panel silently stopped showing.
				if ratio := contrastRatio(c, card); ratio < 3 {
					t.Errorf("palette %q (%s): --%s is %.2f:1 against --card, under the 3:1 floor",
						id, variant, name, ratio)
				}
				for _, other := range chartRamp[i+1:] {
					o, ok := values[other]
					if !ok {
						continue
					}
					dh, dl := hueGap(c.h, o.h), math.Abs(c.l-o.l)*100
					if dh < 30 && dl < 12 {
						t.Errorf("palette %q (%s): --%s and --%s differ by %.0f° of hue and %.1f of lightness, under 30° or 12",
							id, variant, name, other, dh, dl)
					}
				}
			}
		}
	}
}
