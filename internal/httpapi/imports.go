package httpapi

import (
	"time"

	"github.com/gauthier-se/verve/internal/store"
)

// Import is an import of the user of the request, never its token or its payload (Ingestion
// pipeline, Data and API notes).
type Import struct {
	ID         int64             `json:"id" example:"42" doc:"The id of the import."`
	Origin     string            `json:"origin" enum:"xml,hae,native,cli" example:"native" doc:"Where the data comes from: the Apple Health export, Health Auto Export, the native format or the command line."`
	Status     string            `json:"status" enum:"queued,running,completed,failed,cancelled" example:"queued" doc:"The status of the import."`
	Client     map[string]string `json:"client,omitempty" doc:"The metadata of the client that sent the data, left out when there is none. A push stores it once its job reads the payload."`
	TokenName  string            `json:"tokenName,omitempty" example:"Verve for iOS" doc:"The name of the token that pushed the data, left out when there is none."`
	CreatedAt  time.Time         `json:"createdAt" doc:"When the import was created."`
	StartedAt  *time.Time        `json:"startedAt,omitempty" doc:"When the import first ran, left out until it does."`
	FinishedAt *time.Time        `json:"finishedAt,omitempty" doc:"When the import ended, left out until it does."`
	Report     ImportReport      `json:"report" doc:"What happened to the records of the import so far."`
}

// ImportReport is the report of an import: its counters in total, per type key and for the
// workouts, and its sample error messages, which never hold a value.
type ImportReport struct {
	Total    ImportCounters                `json:"total" doc:"Every counter, for every record."`
	Types    map[string]ImportTypeCounters `json:"types" nullable:"false" doc:"The counters of each type key, a metric type key or the identifier of an unsupported type."`
	Workouts ImportTypeCounters            `json:"workouts" doc:"The counters of the workouts."`
	Errors   []string                      `json:"errors" nullable:"false" doc:"Up to 100 sample error messages, which name the position, the type and the reason of a record."`
}

// ImportCounters counts the records of an import by what happened to them, every counter given.
type ImportCounters struct {
	Received    int64 `json:"received" doc:"Samples and workouts read from the payload."`
	Inserted    int64 `json:"inserted" doc:"Records stored for the first time."`
	Updated     int64 `json:"updated" doc:"Records that replaced a stored one."`
	Duplicates  int64 `json:"duplicates" doc:"Records identical to a stored one."`
	Blocked     int64 `json:"blocked" doc:"Records dropped by a block rule."`
	Invalid     int64 `json:"invalid" doc:"Records rejected, such as one with a missing field or an unknown unit."`
	Unsupported int64 `json:"unsupported" doc:"Records of known but excluded types, such as ECG."`
	Suspicious  int64 `json:"suspicious" doc:"Stored records that start before 2000 or more than one day after the import was created."`
	Warnings    int64 `json:"warnings" doc:"Stored records that lost a detail."`
	Deleted     int64 `json:"deleted" doc:"Records removed by a deletion."`
	NotFound    int64 `json:"notFound" doc:"Deleted uuids that matched no record."`
}

// ImportTypeCounters counts the records of a type key, or the workouts, of an import, each counter
// at zero left out.
type ImportTypeCounters struct {
	Received    int64 `json:"received,omitempty"`
	Inserted    int64 `json:"inserted,omitempty"`
	Updated     int64 `json:"updated,omitempty"`
	Duplicates  int64 `json:"duplicates,omitempty"`
	Blocked     int64 `json:"blocked,omitempty"`
	Invalid     int64 `json:"invalid,omitempty"`
	Unsupported int64 `json:"unsupported,omitempty"`
	Suspicious  int64 `json:"suspicious,omitempty"`
	Warnings    int64 `json:"warnings,omitempty"`
	Deleted     int64 `json:"deleted,omitempty"`
	NotFound    int64 `json:"notFound,omitempty"`
}

// importOf returns imp as the API answers it, with tokenName, the name of its token or empty.
func importOf(imp store.Import, tokenName string) Import {
	out := Import{
		ID:        imp.ID,
		Origin:    string(imp.Origin),
		Status:    string(imp.Status),
		Client:    imp.Client,
		TokenName: tokenName,
		CreatedAt: imp.CreatedAt,
		Report: ImportReport{
			Total:    ImportCounters(imp.Report.Total),
			Types:    make(map[string]ImportTypeCounters, len(imp.Report.Types)),
			Workouts: ImportTypeCounters(imp.Report.Workouts),
			Errors:   imp.Report.Errors,
		},
	}

	for key, counters := range imp.Report.Types {
		out.Report.Types[key] = ImportTypeCounters(counters)
	}

	if out.Report.Errors == nil {
		out.Report.Errors = []string{}
	}

	if !imp.StartedAt.IsZero() {
		out.StartedAt = &imp.StartedAt
	}

	if !imp.FinishedAt.IsZero() {
		out.FinishedAt = &imp.FinishedAt
	}

	return out
}
