package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

// sampleExportXML is a tiny valid export: a step count and a body mass (both map),
// plus a sleep category (a State). It yields 2 measurements + 1 state added.
const sampleExportXML = `<?xml version="1.0" encoding="UTF-8"?>
<HealthData locale="en_US">
 <Record type="HKQuantityTypeIdentifierStepCount" sourceName="Watch" unit="count" startDate="2024-01-01 08:00:00 +0000" endDate="2024-01-01 09:00:00 +0000" value="120"/>
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Scale" unit="kg" startDate="2024-01-01 07:00:00 +0000" endDate="2024-01-01 07:00:00 +0000" value="70.5"/>
 <Record type="HKCategoryTypeIdentifierSleepAnalysis" sourceName="Watch" startDate="2024-01-01 23:00:00 +0000" endDate="2024-01-02 06:00:00 +0000" value="HKCategoryValueSleepAnalysisAsleepCore"/>
</HealthData>`

// zipBytes wraps xml as an apple_health_export/export.xml entry in a .zip, the
// artifact a browser uploads.
func zipBytes(t *testing.T, xml string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("apple_health_export/export.xml")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := io.WriteString(w, xml); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// postImport uploads body as filename and returns the response and decoded envelope.
func postImport(t *testing.T, srv *Server, filename string, body []byte, cookie *http.Cookie) (*http.Response, map[string]json.RawMessage) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/imports?filename="+filename, bytes.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	var env map[string]json.RawMessage
	if b, _ := io.ReadAll(res.Body); len(b) > 0 {
		if err := json.Unmarshal(b, &env); err != nil {
			t.Fatalf("decode %q: %v", b, err)
		}
	}
	return res, env
}

// waitForImport polls the status endpoint until the job settles (done/failed) or
// the deadline passes, returning the job view.
func waitForImport(t *testing.T, srv *Server, cookie *http.Cookie) importJobView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, env := do(t, srv, "/v1/imports", cookie)
		var job importJobView
		if err := json.Unmarshal(env["job"], &job); err != nil {
			t.Fatalf("decode job: %v", err)
		}
		if job.Status == string(stateDone) || job.Status == string(stateFailed) {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("import did not settle within the deadline")
	return importJobView{}
}

func TestImportUploadRunsToReport(t *testing.T) {
	srv, models, cookie := newTestServer(t)

	res, _ := postImport(t, srv, "export.zip", zipBytes(t, sampleExportXML), cookie)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("POST status = %d, want 202", res.StatusCode)
	}

	job := waitForImport(t, srv, cookie)
	if job.Status != string(stateDone) {
		t.Fatalf("final status = %q, want done (error: %q)", job.Status, job.Error)
	}
	if job.Percent != 100 {
		t.Errorf("percent = %d, want 100", job.Percent)
	}
	if job.Report == nil {
		t.Fatal("done job carries no report")
	}
	if job.Report.Added != 3 { // 2 measurements + 1 state
		t.Errorf("report Added = %d, want 3", job.Report.Added)
	}
	if job.Report.SourceFile != "export.zip" {
		t.Errorf("report SourceFile = %q, want export.zip", job.Report.SourceFile)
	}

	// The Account now has data: the empty-state CTA must retire.
	_, env := do(t, srv, "/v1/imports", cookie)
	var hasData bool
	if err := json.Unmarshal(env["has_data"], &hasData); err != nil {
		t.Fatalf("decode has_data: %v", err)
	}
	if !hasData {
		t.Error("has_data = false after a successful import")
	}

	acc, _ := models.Accounts.GetByEmail(context.Background(), testEmail)
	if ok, _ := models.Measurements.HasAny(context.Background(), acc.ID); !ok {
		t.Error("no measurements persisted after import")
	}
}

func TestImportSecondConcurrentRefused(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	// Register an in-flight job directly so the second POST races a running one.
	acc, _ := srv.models.Accounts.GetByEmail(context.Background(), testEmail)
	if _, err := srv.imports.begin(acc.ID, "first.zip", 10); err != nil {
		t.Fatalf("seed in-flight job: %v", err)
	}

	res, env := postImport(t, srv, "second.zip", zipBytes(t, sampleExportXML), cookie)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("second import status = %d, want 409", res.StatusCode)
	}
	if !bytes.Contains(env["error"], []byte("already running")) {
		t.Errorf("error = %s, want 'already running'", env["error"])
	}
}

func TestImportOversizeRejectedBeforeStreaming(t *testing.T) {
	srv, cookie := newCappedServer(t, 8)

	res, _ := postImport(t, srv, "export.zip", zipBytes(t, sampleExportXML), cookie)
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize status = %d, want 413", res.StatusCode)
	}
	// No job was registered — the upload never began.
	acc, _ := srv.models.Accounts.GetByEmail(context.Background(), testEmail)
	if job := srv.imports.job(acc.ID); job != nil {
		t.Error("an oversize upload registered a job")
	}
}

func TestImportInvalidZipFails(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	res, _ := postImport(t, srv, "export.zip", []byte("this is not a zip"), cookie)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("POST status = %d, want 202 (failure surfaces via the job)", res.StatusCode)
	}
	job := waitForImport(t, srv, cookie)
	if job.Status != string(stateFailed) {
		t.Fatalf("status = %q, want failed", job.Status)
	}
	if job.Error == "" {
		t.Error("failed job carries no message")
	}
}

func TestImportMissingExportXMLFails(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("something_else.txt")
	io.WriteString(w, "no export here")
	zw.Close()

	postImport(t, srv, "export.zip", buf.Bytes(), cookie)
	job := waitForImport(t, srv, cookie)
	if job.Status != string(stateFailed) {
		t.Fatalf("status = %q, want failed", job.Status)
	}
	if !bytes.Contains([]byte(job.Error), []byte("export.xml")) {
		t.Errorf("error = %q, want it to mention export.xml", job.Error)
	}
}

func TestImportReuploadIsIdempotent(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc, _ := models.Accounts.GetByEmail(context.Background(), testEmail)
	body := zipBytes(t, sampleExportXML)

	postImport(t, srv, "export.zip", body, cookie)
	if job := waitForImport(t, srv, cookie); job.Status != string(stateDone) {
		t.Fatalf("first import status = %q", job.Status)
	}
	firstCount := measurementCount(t, models, acc.ID)

	postImport(t, srv, "export.zip", body, cookie)
	job := waitForImport(t, srv, cookie)
	if job.Status != string(stateDone) {
		t.Fatalf("re-import status = %q", job.Status)
	}
	if job.Report.Added != 0 {
		t.Errorf("re-import Added = %d, want 0 (idempotent)", job.Report.Added)
	}
	if got := measurementCount(t, models, acc.ID); got != firstCount {
		t.Errorf("measurement rows after re-import = %d, want %d (no duplicates)", got, firstCount)
	}
}

func TestImportTempFileRemovedAfterCompletion(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	postImport(t, srv, "export.zip", zipBytes(t, sampleExportXML), cookie)
	waitForImport(t, srv, cookie)

	entries, err := os.ReadDir(srv.imports.tmpDir)
	if err != nil {
		t.Fatalf("read tmp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("tmp dir holds %d files after import, want 0", len(entries))
	}
}

func TestImportSweepsOrphansAtStartup(t *testing.T) {
	dir := t.TempDir()
	tmpDir := filepath.Join(dir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	orphan := filepath.Join(tmpDir, "import-1-999.zip")
	if err := os.WriteFile(orphan, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	db, err := data.Open(filepath.Join(dir, "verve.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := data.Migrate(context.Background(), db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := New(slog.New(slog.NewTextHandler(io.Discard, nil)), data.NewModels(db), query.Engine{DB: db},
		Config{DataDir: dir, ArtifactsDir: filepath.Join(dir, "artifacts")}); err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphan temp file survived startup: %v", err)
	}
}

// newCappedServer builds a logged-in test server whose import upload cap is
// maxUpload bytes.
func newCappedServer(t *testing.T, maxUpload int64) (*Server, *http.Cookie) {
	t.Helper()
	dir := t.TempDir()
	db, err := data.Open(filepath.Join(dir, "verve.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := data.Migrate(context.Background(), db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	models := data.NewModels(db)
	srv, err := New(slog.New(slog.NewTextHandler(io.Discard, nil)), models, query.Engine{DB: db},
		Config{SecureCookies: false, DataDir: dir, ArtifactsDir: filepath.Join(dir, "artifacts"), MaxUploadBytes: maxUpload})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	seedAccountWithPassword(t, models, testEmail, testPassword)
	return srv, login(t, srv, testEmail, testPassword)
}

func measurementCount(t *testing.T, models data.Models, accountID int64) int {
	t.Helper()
	var n int
	if err := models.Measurements.DB.QueryRowContext(context.Background(),
		`SELECT count(*) FROM measurements WHERE account_id = ?`, accountID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
