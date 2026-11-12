// Package ingest is the ingestion pipeline: it turns the records that adapters parse into stored
// samples and workouts.
package ingest

import (
	"strings"

	"github.com/gauthier-se/verve/internal/store"
)

// vendor is a maker of smart scales or smart rings, known by the names and the bundle ids of its
// apps.
type vendor struct {
	kind      store.SourceKind
	names     []string
	bundleIDs []string
}

// vendors are the known smart scale and smart ring vendors. Names are the source names their apps
// write to HealthKit, and bundle ids those of the App Store.
var vendors = []vendor{
	{kind: store.SourceScale, names: []string{"Withings", "Health Mate"}, bundleIDs: []string{"com.withings.wiScaleNG"}},
	{kind: store.SourceScale, names: []string{"RENPHO Health", "Renpho"}, bundleIDs: []string{"com.renpho.health", "com.qnniu.renpho"}},
	{kind: store.SourceScale, names: []string{"eufy Life", "EufyLife"}, bundleIDs: []string{"com.oceanwing.z.smarthome"}},
	{kind: store.SourceScale, names: []string{"VeSync"}, bundleIDs: []string{"com.etekcity.vesyncPlatform"}},
	{kind: store.SourceScale, names: []string{"Fitdays"}, bundleIDs: []string{"cn.fitdays.fitdays"}},
	{kind: store.SourceScale, names: []string{"arboleaf"}, bundleIDs: []string{"com.qingniu.arboleaf"}},
	{kind: store.SourceRing, names: []string{"Oura"}, bundleIDs: []string{"com.ouraring.oura"}},
	{kind: store.SourceRing, names: []string{"Ultrahuman"}, bundleIDs: []string{"com.ultrahuman.ios"}},
	{kind: store.SourceRing, names: []string{"RingConn"}, bundleIDs: []string{"com.gdjztech.ringconn"}},
	{kind: store.SourceRing, names: []string{"Circular"}, bundleIDs: []string{"com.circular.circular"}},
}

// healthAppBundleID is the bundle id of the Health app, the source of the entries made by hand.
const healthAppBundleID = "com.apple.Health"

// healthAppName is the source name of the entries made by hand in the Health app, which the XML
// export gives without bundle id.
const healthAppName = "Health"

// DetectKind returns the kind of the source of info (FR-2 of Deduplication and source priority).
// The first rule that matches gives the kind:
//   - a known vendor, by its bundle id, exactly, or its name, whatever its case: scale or ring;
//   - the Health app, by its bundle id, exactly, or its name, whatever its case, on an iPhone:
//     manual;
//   - the device model: watch for Watch, phone for iPhone;
//   - a bundle id outside com.apple.: app;
//   - without device model, a name that contains Apple Watch or iPhone, whatever its case: watch or
//     phone;
//   - otherwise unknown.
func DetectKind(info store.SourceInfo) store.SourceKind {
	for _, v := range vendors {
		if v.matches(info) {
			return v.kind
		}
	}

	if info.BundleID == healthAppBundleID ||
		(info.DeviceModel == "iPhone" && strings.EqualFold(info.Name, healthAppName)) {
		return store.SourceManual
	}

	switch info.DeviceModel {
	case "Watch":
		return store.SourceWatch
	case "iPhone":
		return store.SourcePhone
	}

	if info.BundleID != "" && !strings.HasPrefix(info.BundleID, "com.apple.") {
		return store.SourceApp
	}

	if info.DeviceModel == "" {
		name := strings.ToLower(info.Name)

		switch {
		case strings.Contains(name, "apple watch"):
			return store.SourceWatch
		case strings.Contains(name, "iphone"):
			return store.SourcePhone
		}
	}

	return store.SourceUnknown
}

// matches reports whether info is from an app of v.
func (v vendor) matches(info store.SourceInfo) bool {
	for _, id := range v.bundleIDs {
		if info.BundleID == id {
			return true
		}
	}

	for _, name := range v.names {
		if strings.EqualFold(info.Name, name) {
			return true
		}
	}

	return false
}
