package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handleCreateImport accepts an Apple Health export .zip, streams it to a temp
// file under the data dir, and launches a background import (ADR 0016). An oversize
// upload is refused via Content-Length before any streaming; a second concurrent
// import for the same Account is refused while one runs. The response carries the
// job's initial status, which the client then polls at GET /v1/imports.
func (s *Server) handleCreateImport(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	filename := r.URL.Query().Get("filename")
	if filename == "" {
		filename = "export.zip"
	}
	if !strings.EqualFold(filepath.Ext(filename), ".zip") {
		s.errorResponse(w, r, http.StatusUnsupportedMediaType, "only a .zip Apple Health export is accepted here")
		return
	}

	// Reject an oversize upload before streaming a single byte (ADR 0016).
	if r.ContentLength > s.imports.maxUpload {
		s.errorResponse(w, r, http.StatusRequestEntityTooLarge, "the export is larger than this instance allows")
		return
	}

	job, err := s.imports.begin(accountID, filename, r.ContentLength)
	if errors.Is(err, errImportInFlight) {
		s.errorResponse(w, r, http.StatusConflict, errImportInFlight.Error())
		return
	}
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	tmpPath := s.imports.tempPath(accountID)
	f, err := os.Create(tmpPath)
	if err != nil {
		job.fail("the server could not store the upload.")
		s.serverErrorResponse(w, r, err)
		return
	}

	// A lying/absent Content-Length is still capped mid-stream by MaxBytesReader.
	body := http.MaxBytesReader(w, r.Body, s.imports.maxUpload)
	if err := job.stream(f, body); err != nil {
		f.Close()
		os.Remove(tmpPath)
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			job.fail("the export is larger than this instance allows.")
			s.errorResponse(w, r, http.StatusRequestEntityTooLarge, "the export is larger than this instance allows")
			return
		}
		job.fail("the upload was interrupted.")
		s.badRequestResponse(w, r, errors.New("upload interrupted"))
		return
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		job.fail("the server could not store the upload.")
		s.serverErrorResponse(w, r, err)
		return
	}

	go s.imports.run(job, accountID, tmpPath)

	s.writeImportStatus(w, r, http.StatusAccepted, job)
}

// handleImportStatus reports the Account's current import job — phase, percent, and
// the final report or failure message — plus whether the Account has any data yet,
// which drives the dashboard's empty-state CTA (ADR 0016, ADR 0018).
func (s *Server) handleImportStatus(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	s.writeImportStatus(w, r, http.StatusOK, s.imports.job(accountID))
}

// writeImportStatus renders a job (possibly nil) and the Account's has-data flag as
// the shared status envelope used by both the create and poll endpoints.
func (s *Server) writeImportStatus(w http.ResponseWriter, r *http.Request, status int, job *importJob) {
	accountID, _ := s.accountID(r)
	hasData, err := s.models.Measurements.HasAny(r.Context(), accountID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	var view *importJobView
	if job != nil {
		v := job.view()
		view = &v
	}
	if err := writeJSON(w, status, envelope{"job": view, "has_data": hasData}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// importJobView is a job as exposed by the API: lifecycle status, phase, a single
// 0–100 percent, and — once settled — the report or a human failure message.
type importJobView struct {
	Status  string      `json:"status"`
	Phase   string      `json:"phase"`
	Percent int         `json:"percent"`
	Report  *reportView `json:"report,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// reportView is the compact import outcome the SPA renders: the source file and
// added / skipped / unmapped counts across all families.
type reportView struct {
	SourceFile string `json:"source_file"`
	Added      int    `json:"added"`
	Skipped    int    `json:"skipped"`
	Unmapped   int    `json:"unmapped"`
}

// view snapshots the job under its lock into the API shape, collapsing the active
// phase's counters into one honest percentage.
func (job *importJob) view() importJobView {
	job.mu.Lock()
	defer job.mu.Unlock()

	v := importJobView{Status: string(job.state), Phase: string(job.phase), Error: job.errMsg}
	switch {
	case job.state == stateDone:
		v.Percent = 100
	case job.phase == phaseUpload:
		v.Percent = percent(job.uploaded.Load(), job.uploadTotal)
	case job.phase == phaseImport:
		v.Percent = percent(job.decoded.Load(), job.decodeTotal.Load())
	}
	if job.report != nil {
		v.Report = &reportView{
			SourceFile: job.sourceFile,
			Added:      job.report.Added + job.report.StatesAdded + job.report.SessionsAdded,
			Skipped:    job.report.Skipped + job.report.StatesSkipped + job.report.SessionsSkipped,
			Unmapped:   job.report.Unmapped,
		}
	}
	return v
}

// percent is n/total as a 0–100 integer, guarding a zero or unknown denominator.
func percent(n, total int64) int {
	if total <= 0 {
		return 0
	}
	p := n * 100 / total
	if p > 100 {
		return 100
	}
	return int(p)
}
