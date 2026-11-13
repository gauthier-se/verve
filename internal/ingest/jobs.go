package ingest

import "strconv"

// SerialKey returns the serial key of the import jobs of the user of userID, shared by every import
// kind, so that the imports of a user run one at a time, in arrival order (FR-4 of Ingestion
// pipeline).
func SerialKey(userID int64) string {
	return "import:" + strconv.FormatInt(userID, 10)
}
