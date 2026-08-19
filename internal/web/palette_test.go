package web

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The Appearance stylesheet and the TypeScript roster that must agree with it.
// Both are read as text from the repo: this package already owns the SPA, and a
// Go test needs no front-end toolchain (web/ has none) while running in `make ci`.
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
