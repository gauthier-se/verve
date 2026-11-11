package catalog_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
)

// Given an identifier that the catalog does not list, when an uncurated type is created for it,
// then its key is derived by the rule of FR-2 of the storage spec.
func TestUncuratedKey_DerivesTheKeyOfAnUnknownIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		identifier string
		want       string
	}{
		{"QuantityPrefixIsStripped", "HKQuantityTypeIdentifierUVExposure", "uv_exposure"},
		{"CategoryPrefixIsStripped", "HKCategoryTypeIdentifierSleepAnalysis", "sleep_analysis"},
		{"DigitBeforeUpperCaseSplits", "HKQuantityTypeIdentifierVO2Max", "vo2_max"},
		{"OtherPrefixIsKept", "HKDataTypeIdentifierFoo", "hk_data_type_identifier_foo"},
		{"PrefixOnlyInTheMiddleIsKept", "MyHKQuantityTypeIdentifierFoo", "my_hk_quantity_type_identifier_foo"},
		{"HealthAutoExportName", "Blood Oxygen", "blood_oxygen"},
		{"LowerCaseNameIsKept", "step_count", "step_count"},
		{"RunOfSeparatorsGivesOneUnderscore", "Heart -- Rate (Resting)", "heart_rate_resting"},
		{"NonASCIILettersAreSeparators", "Fréquence cardiaque", "fr_quence_cardiaque"},
		{"UnderscoresAreTrimmed", "_Foo_", "foo"},
		{"UpperCaseRunEndsBeforeTheLastLetter", "HTTPServer", "http_server"},
		{"UpperCaseRunAtTheEndStaysWhole", "MaxVO", "max_vo"},
		{"LeadingDigit", "2Fast", "2_fast"},
		{"EmptyIdentifier", "", "uncurated"},
		{"PrefixAlone", "HKQuantityTypeIdentifier", "uncurated"},
		{"SeparatorsAlone", " -_ ", "uncurated"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, catalog.UncuratedKey(tt.identifier))
		})
	}
}
