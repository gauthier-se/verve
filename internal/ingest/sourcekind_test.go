package ingest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
)

// kindTest is a source and the kind DetectKind gives it.
type kindTest struct {
	name string
	info store.SourceInfo
	want store.SourceKind
}

// runKindTests runs DetectKind on each test.
func runKindTests(t *testing.T, tests []kindTest) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, ingest.DetectKind(tt.info))
		})
	}
}

// Given the device model of an Apple Watch or an iPhone, when DetectKind runs, then it returns
// watch or phone.
func TestDetectKind_AppleDeviceModel_ReturnsWatchOrPhone(t *testing.T) {
	t.Parallel()

	runKindTests(t, []kindTest{
		{
			name: "watch",
			info: store.SourceInfo{
				Name:        "Camille's Apple Watch",
				DeviceModel: "Watch",
				BundleID:    "com.apple.health.3F9A61C2",
			},
			want: store.SourceWatch,
		},
		{
			name: "watch without bundle id",
			info: store.SourceInfo{Name: "Wrist", DeviceModel: "Watch"},
			want: store.SourceWatch,
		},
		{
			name: "phone",
			info: store.SourceInfo{
				Name:        "Camille's iPhone",
				DeviceModel: "iPhone",
				BundleID:    "com.apple.health.7B20D4E9",
			},
			want: store.SourcePhone,
		},
		{
			name: "manual entry in the Health app",
			info: store.SourceInfo{Name: "Health", DeviceModel: "iPhone", BundleID: "com.apple.Health"},
			want: store.SourcePhone,
		},
	})
}

// Given the bundle id or the name of a known smart scale or smart ring vendor, when DetectKind
// runs, then it returns scale or ring. Names match whatever their case, bundle ids only exactly.
func TestDetectKind_KnownVendor_ReturnsScaleOrRing(t *testing.T) {
	t.Parallel()

	runKindTests(t, []kindTest{
		{
			name: "scale bundle id",
			info: store.SourceInfo{Name: "Bathroom", BundleID: "com.withings.wiScaleNG"},
			want: store.SourceScale,
		},
		{
			name: "other scale bundle id",
			info: store.SourceInfo{Name: "Bathroom", BundleID: "com.renpho.health"},
			want: store.SourceScale,
		},
		{
			name: "scale name",
			info: store.SourceInfo{Name: "Withings"},
			want: store.SourceScale,
		},
		{
			name: "scale name in another case",
			info: store.SourceInfo{Name: "EUFY LIFE"},
			want: store.SourceScale,
		},
		{
			name: "ring bundle id",
			info: store.SourceInfo{Name: "Night", BundleID: "com.ouraring.oura"},
			want: store.SourceRing,
		},
		{
			name: "ring name",
			info: store.SourceInfo{Name: "RingConn"},
			want: store.SourceRing,
		},
		{
			name: "ring name in another case",
			info: store.SourceInfo{Name: "oura"},
			want: store.SourceRing,
		},
		{
			name: "vendor bundle id in another case is only an app",
			info: store.SourceInfo{Name: "Night", BundleID: "COM.OURARING.OURA"},
			want: store.SourceApp,
		},
	})
}

// Given a third-party app without a device model, when DetectKind runs, then it returns app.
func TestDetectKind_ThirdPartyAppWithoutDeviceModel_ReturnsApp(t *testing.T) {
	t.Parallel()

	runKindTests(t, []kindTest{
		{
			name: "running app",
			info: store.SourceInfo{Name: "Strava", BundleID: "com.strava.stravaride"},
			want: store.SourceApp,
		},
		{
			name: "nutrition app",
			info: store.SourceInfo{Name: "MyFitnessPal", BundleID: "com.myfitnesspal.mfp"},
			want: store.SourceApp,
		},
	})
}

// Given a source that matches no rule, when DetectKind runs, then it returns unknown.
func TestDetectKind_NoRuleMatches_ReturnsUnknown(t *testing.T) {
	t.Parallel()

	runKindTests(t, []kindTest{
		{
			name: "name only",
			info: store.SourceInfo{Name: "Camille's Apple Watch"},
			want: store.SourceUnknown,
		},
		{
			name: "app name without bundle id",
			info: store.SourceInfo{Name: "Strava"},
			want: store.SourceUnknown,
		},
		{
			name: "apple bundle id without device model",
			info: store.SourceInfo{Name: "Health", BundleID: "com.apple.Health"},
			want: store.SourceUnknown,
		},
		{
			name: "other apple device",
			info: store.SourceInfo{
				Name:        "Camille's iPad",
				DeviceModel: "iPad",
				BundleID:    "com.apple.health.C41E0B7A",
			},
			want: store.SourceUnknown,
		},
	})
}

// Given rules that disagree, when DetectKind runs, then a known vendor wins over the device
// model, and the device model over the generic app rule.
func TestDetectKind_RulesDisagree_VendorThenDeviceModelThenApp(t *testing.T) {
	t.Parallel()

	runKindTests(t, []kindTest{
		{
			name: "scale written through the phone app of its vendor",
			info: store.SourceInfo{Name: "Withings", DeviceModel: "iPhone", BundleID: "com.withings.wiScaleNG"},
			want: store.SourceScale,
		},
		{
			name: "ring written through the phone app of its vendor",
			info: store.SourceInfo{Name: "Oura", DeviceModel: "iPhone", BundleID: "com.ouraring.oura"},
			want: store.SourceRing,
		},
		{
			name: "vendor name over device model",
			info: store.SourceInfo{Name: "Withings", DeviceModel: "Watch"},
			want: store.SourceScale,
		},
		{
			name: "third-party app on the watch",
			info: store.SourceInfo{Name: "Strava", DeviceModel: "Watch", BundleID: "com.strava.stravaride.watchkitapp"},
			want: store.SourceWatch,
		},
	})
}
