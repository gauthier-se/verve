package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gauthier-se/verve/internal/connector"
	"github.com/gauthier-se/verve/internal/connector/registry"
	"github.com/gauthier-se/verve/internal/data"
)

// defaultMaxUploadBytes caps a web import upload absent an override: 2 GiB, ample
// for Apple's ~750 MB export while refusing a runaway upload early (ADR 0016).
const defaultMaxUploadBytes int64 = 2 << 30

// errImportInFlight is begin's refusal when the Account already has a running
// import — one at a time (ADR 0016).
var errImportInFlight = errors.New("an import is already running for this account")

// importPhase is which half of a two-phase import a job is in (ADR 0016).
type importPhase string

const (
	phaseUpload importPhase = "upload"
	phaseImport importPhase = "import"
)

// importState is a job's lifecycle status (ADR 0016).
type importState string

const (
	statePending importState = "pending"
	stateRunning importState = "running"
	stateDone    importState = "done"
	stateFailed  importState = "failed"
)

// importJob is one web import in flight (ADR 0016): its lifecycle state, the
// two-phase progress counters, and — once settled — the Report or a human error.
// The progress counters are written by the upload/decode and read concurrently by
// the polling status handler, so they are atomic; the settled fields sit under mu.
type importJob struct {
	sourceFile string // the user's uploaded filename, for the report
	// connectorLabel is the Connector that read it, as a person names it. It is set
	// when the archive is recognized, which is after the upload: a job that fails
	// before then has no source to name, and says so by leaving this empty.
	connectorLabel string

	uploaded    atomic.Int64
	uploadTotal int64
	decoded     atomic.Int64
	decodeTotal atomic.Int64

	mu     sync.Mutex
	state  importState
	phase  importPhase
	report *connector.Report
	errMsg string
}

// importRegistry holds the at-most-one in-flight import per Account (ADR 0016),
// in memory. It owns the temp directory uploads stream through and the import
// engine's dependencies: the store it writes through, the Exclusions it must honour,
// the artifacts dir, and the size cap.
type importRegistry struct {
	logger       *slog.Logger
	store        connector.Store
	exclusions   data.ExclusionModel
	artifactsDir string
	tmpDir       string
	maxUpload    int64

	mu   sync.Mutex
	jobs map[int64]*importJob // by accountID; a settled job lingers for polling
}

// newImportRegistry prepares the temp directory under dataDir and sweeps any
// orphan upload left by a crashed import (ADR 0016). maxUpload ≤ 0 uses the default.
func newImportRegistry(logger *slog.Logger, models data.Models, dataDir, artifactsDir string, maxUpload int64) (*importRegistry, error) {
	if maxUpload <= 0 {
		maxUpload = defaultMaxUploadBytes
	}
	tmpDir := filepath.Join(dataDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		return nil, fmt.Errorf("api: create import tmp dir: %w", err)
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("api: sweep import tmp dir: %w", err)
	}
	for _, e := range entries {
		orphan := filepath.Join(tmpDir, e.Name())
		if err := os.Remove(orphan); err != nil {
			logger.Warn("sweep orphan import temp file", "path", orphan, "err", err)
		}
	}
	return &importRegistry{
		logger: logger, store: models.ImportStore(), exclusions: models.Exclusions, artifactsDir: artifactsDir,
		tmpDir: tmpDir, maxUpload: maxUpload, jobs: map[int64]*importJob{},
	}, nil
}

// begin registers a new job for accountID, refusing with errImportInFlight if one
// is still pending or running — the one-import-per-Account guard (ADR 0016). A
// settled (done/failed) prior job is simply replaced.
func (reg *importRegistry) begin(accountID int64, sourceFile string, uploadTotal int64) (*importJob, error) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if j, ok := reg.jobs[accountID]; ok && j.active() {
		return nil, errImportInFlight
	}
	job := &importJob{sourceFile: sourceFile, uploadTotal: uploadTotal, state: statePending, phase: phaseUpload}
	reg.jobs[accountID] = job
	return job, nil
}

// job returns the Account's current job, or nil if it never imported.
func (reg *importRegistry) job(accountID int64) *importJob {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return reg.jobs[accountID]
}

// tempPath is a fresh ".zip" path under the temp dir. The extension is a hint for
// anyone looking in that directory and nothing more: which Connector reads the file
// is decided by looking inside it (ADR 0009).
func (reg *importRegistry) tempPath(accountID int64) string {
	return filepath.Join(reg.tmpDir, fmt.Sprintf("import-%d-%d.zip", accountID, time.Now().UnixNano()))
}

// stream copies the upload body to dst, counting bytes into the job for the upload
// phase's progress. It returns the copy error (e.g. a MaxBytesReader overflow or a
// dropped connection) so the caller can settle the job and answer the request.
func (job *importJob) stream(dst io.Writer, body io.Reader) error {
	job.setRunning()
	_, err := io.Copy(countingWriter{dst, &job.uploaded}, body)
	return err
}

// run executes the import to completion in the background, then deletes the temp
// file (ADR 0016). It uses a background context so the import outlives the upload
// request that started it. On failure the job carries a human message; there is no
// rollback — a re-upload is the idempotent recovery.
func (reg *importRegistry) run(job *importJob, accountID int64, tmpPath string) {
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			reg.logger.Warn("remove import temp file", "path", tmpPath, "err", err)
		}
	}()

	job.enterImport()
	progress := func(decoded, total int64) {
		job.decoded.Store(decoded)
		job.decodeTotal.Store(total)
	}

	ctx := context.Background()
	// The Exclusions are read here, at run time, and not when the registry was
	// built: they are edited on the page that starts the import, seconds before it
	// starts, and a set cached at startup would spend that interval being wrong
	// (ADR 0033).
	exclusions, err := reg.exclusions.Set(ctx, accountID)
	if err != nil {
		reg.logger.Error("read exclusions for import", "account", accountID, "err", err)
		job.fail("the server could not read your exclusions.")
		return
	}

	// Which Connector reads this upload is decided by the file itself (ADR 0009):
	// both supported exports are .zip, so the extension settles nothing and the
	// owner is never asked a question their archive already answers.
	conn, err := registry.For(tmpPath)
	if err != nil {
		reg.logger.Info("unrecognized export", "account", accountID, "err", err)
		job.fail(humanImportError(err))
		return
	}

	job.setConnector(conn.Label())

	report, err := conn.Import(ctx, reg.store, accountID, tmpPath, connector.Options{
		ArtifactsDir: reg.artifactsDir,
		Progress:     progress,
		Exclusions:   exclusions,
	})
	if err != nil {
		reg.logger.Error("web import failed", "account", accountID, "err", err)
		job.fail(humanImportError(err))
		return
	}
	job.finish(report)
}

func (job *importJob) active() bool {
	job.mu.Lock()
	defer job.mu.Unlock()
	return job.state == statePending || job.state == stateRunning
}

func (job *importJob) setConnector(label string) {
	job.mu.Lock()
	job.connectorLabel = label
	job.mu.Unlock()
}

func (job *importJob) setRunning() {
	job.mu.Lock()
	job.state = stateRunning
	job.mu.Unlock()
}

func (job *importJob) enterImport() {
	job.mu.Lock()
	job.phase = phaseImport
	job.state = stateRunning
	job.mu.Unlock()
}

func (job *importJob) fail(msg string) {
	job.mu.Lock()
	job.state = stateFailed
	job.errMsg = msg
	job.mu.Unlock()
}

func (job *importJob) finish(report connector.Report) {
	job.mu.Lock()
	job.state = stateDone
	job.report = &report
	job.mu.Unlock()
}

// countingWriter tallies bytes written into an atomic counter, feeding the upload
// phase's progress without holding a lock on the hot path.
type countingWriter struct {
	w io.Writer
	n *atomic.Int64
}

func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(int64(n))
	return n, err
}

// humanImportError maps an import failure to a message safe and useful to show a
// non-developer (ADR 0016). The underlying errors are wrapped strings from the
// Connector; matching their stable phrasing keeps the mapping in one place.
func humanImportError(err error) string {
	if errors.Is(err, connector.ErrUnknownExport) {
		return "no connector recognizes this archive. Verve reads " +
			strings.Join(registry.Labels(), " and ") + " exports."
	}
	switch msg := err.Error(); {
	case strings.Contains(msg, "no export.xml"):
		return "no export.xml inside the archive — is this an Apple Health export?"
	case strings.Contains(msg, "open zip"):
		return "the file isn’t a valid .zip archive."
	default:
		return "the import failed while reading the export."
	}
}
