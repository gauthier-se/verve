package catalog_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// metricTexts are the texts of a type in the metrics namespace of the web app.
type metricTexts struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Values      map[string]string `json:"values"`
}

// readMetricTexts reads web/src/locales/<language>/metrics.json, and fails on a field that a type
// does not have.
func readMetricTexts(t *testing.T, language string) map[string]metricTexts {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "locales", language, "metrics.json")) //nolint:gosec // G304: a locale file of the repository
	require.NoError(t, err)

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var texts map[string]metricTexts
	require.NoError(t, dec.Decode(&texts), language)

	return texts
}

// Given a curated key, then the metrics files hold its name, its description and the name of each
// of its values. Given a key in these files that the catalog lacks, then the test fails too.
func TestTranslations_MatchTheCatalog(t *testing.T) {
	t.Parallel()

	types := shipped(t).Types()

	for _, language := range []string{"en", "fr"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()

			texts := readMetricTexts(t, language)
			keys := make([]string, 0, len(types))

			for _, typ := range types {
				keys = append(keys, typ.Key)

				text, ok := texts[typ.Key]
				require.True(t, ok, "%s has no texts", typ.Key)
				require.NotEmpty(t, strings.TrimSpace(text.Name), "%s has no name", typ.Key)
				require.NotEmpty(t, strings.TrimSpace(text.Description), "%s has no description", typ.Key)

				valueKeys := make([]string, 0, len(typ.Values))
				for _, v := range typ.Values {
					valueKeys = append(valueKeys, v.Key)
					require.NotEmpty(t, strings.TrimSpace(text.Values[v.Key]), "%s has no name for its value %s", typ.Key, v.Key)
				}

				require.ElementsMatch(t, valueKeys, slices.Collect(maps.Keys(text.Values)),
					"%s: the values of the file and those of the catalog differ", typ.Key)
			}

			require.ElementsMatch(t, keys, slices.Collect(maps.Keys(texts)),
				"the keys of the file and those of the catalog differ")
		})
	}
}
