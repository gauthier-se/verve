package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/ingest/applexml"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// progressInterval is how often ImportXML reports the progress of its import (FR-10 of XML import).
const progressInterval = 5 * time.Second

// XMLImport is an import of an Apple Health export from the command line (FR-10 of XML import).
type XMLImport struct {
	// Login is the username of the user who imports, in any case.
	Login string
	// Path is the absolute path of the export, a zip or a bare export.xml.
	Path string
	// Waiting is called once when the import waits for another import of the user to end.
	Waiting func()
	// Progress is called with the progress of the import, from 0 to 1, at most every 5 seconds.
	Progress func(progress float64)
}

// ImportXML imports the export of req for its user in the database of cfg, as an import of origin
// cli whose job import.xml runs in this process, and returns the import once it ended: completed,
// failed or cancelled once ctx is. It opens the database in shared mode, so that it also runs next
// to a serving process, and registers no other job kind: the server recomputes the aggregates.
//
// It returns store.ErrNotFound for an unknown user, auth.ErrUserDisabled and auth.ErrUserDeleting
// for a user who cannot import, and the *os.PathError of a file that cannot be opened, without
// creating an import, and an error when the import did not end.
func ImportXML(ctx context.Context, cfg config.Config, req XMLImport, logger *slog.Logger) (store.Import, error) {
	return importXML(ctx, cfg, req, logger, importOptions{progressInterval: progressInterval})
}

// importOptions tunes importXML for the tests.
type importOptions struct {
	progressInterval time.Duration
	// waitInterval is how often a waiting import tries again, the default of jobs.Options when 0.
	waitInterval time.Duration
	// batchSize is the batch size of the pipeline, its default when 0.
	batchSize int
	// wrapPipeline, when set, returns the pipeline that the import runs in place of the one it gets.
	wrapPipeline func(ingest.ImportPipeline) ingest.ImportPipeline
}

// importXML is ImportXML with opts.
func importXML(ctx context.Context, cfg config.Config, req XMLImport, logger *slog.Logger, opts importOptions,
) (store.Import, error) {
	return withSharedDB(ctx, cfg, func(s store.Store) (store.Import, error) {
		user, err := importingUser(ctx, s, req.Login)
		if err != nil {
			return store.Import{}, err
		}

		if err := checkReadable(req.Path); err != nil {
			return store.Import{}, err
		}

		shipped, err := catalog.Shipped()
		if err != nil {
			return store.Import{}, err
		}

		clk := clock.Real()

		ing, err := newIngestion(s, shipped, newLayout(cfg.DataDir).Inbox, clk, opts.batchSize)
		if err != nil {
			return store.Import{}, err
		}

		defer func() { err = errors.Join(err, ing.Close()) }()

		var pipeline ingest.ImportPipeline = ing.pipeline
		if opts.wrapPipeline != nil {
			pipeline = opts.wrapPipeline(pipeline)
		}

		// Only the import runs here: the server recomputes the aggregates that it changes.
		registry := jobs.NewRegistry()
		applexml.Register(registry, ingest.ImportJobDeps{Store: s, Pipeline: pipeline, Inbox: ing.inbox})

		worker := jobs.NewWorker(s, registry, clk, logger, jobs.Options{WaitInterval: opts.waitInterval})

		// The id of the import once it is created with its job, read by the progress reports.
		var importID atomic.Int64

		stopReporting := reportProgress(ctx, s, user.ID, &importID, opts.progressInterval, req.Progress)

		// The import is created with its job, so that a command stopped while it waits leaves none.
		runErr := worker.RunInProcessWith(ctx, func(ctx context.Context, tx store.Repositories) (store.NewJob, error) {
			imp, err := tx.Imports().Create(ctx, store.NewImport{UserID: user.ID, Origin: store.OriginCLI})
			if err != nil {
				return store.NewJob{}, fmt.Errorf("create import: %w", err)
			}

			tx.AfterCommit(func() { importID.Store(imp.ID) })

			return applexml.Job(user.ID, imp.ID, req.Path)
		}, req.Waiting)

		stopReporting()

		id := importID.Load()
		if id == 0 {
			return store.Import{}, runErr
		}

		// Read even once ctx is done, since an interrupted import ends cancelled.
		ended, err := s.Imports().Get(context.WithoutCancel(ctx), user.ID, id)
		if err != nil {
			return store.Import{}, errors.Join(runErr, fmt.Errorf("get import %d: %w", id, err))
		}

		if !ended.Status.Finished() {
			return ended, errors.Join(runErr, fmt.Errorf("import %d is still %s", id, ended.Status))
		}

		return ended, nil
	})
}

// importingUser returns the user of login, who must be active to import.
func importingUser(ctx context.Context, s store.Store, login string) (store.User, error) {
	user, err := s.Users().GetByLogin(ctx, login)
	if err != nil {
		return store.User{}, fmt.Errorf("get user: %w", err)
	}

	switch user.Status {
	case store.UserDisabled:
		return store.User{}, auth.ErrUserDisabled
	case store.UserDeleting:
		return store.User{}, auth.ErrUserDeleting
	}

	return user, nil
}

// checkReadable returns the *os.PathError of the file at path when it cannot be opened for reading,
// a directory included.
func checkReadable(path string) error {
	f, err := os.Open(path) //nolint:gosec // G304: the path of a CLI import, given by the operator
	if err != nil {
		return err
	}

	info, err := f.Stat()

	closeErr := f.Close()

	switch {
	case err != nil:
		return err
	case info.IsDir():
		return &os.PathError{Op: "open", Path: path, Err: syscall.EISDIR}
	}

	return closeErr
}

// reportProgress calls report every interval with the progress of the import of userID whose id
// importID holds, once it is created and its job stored the size of its export, when it changed,
// until the returned function is called, which waits for it to stop.
func reportProgress(ctx context.Context, s store.Store, userID int64, importID *atomic.Int64, interval time.Duration,
	report func(float64),
) func() {
	ctx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup

	wg.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		last := -1.0

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			id := importID.Load()
			if id == 0 {
				continue
			}

			current, err := s.Imports().Get(ctx, userID, id)
			if err != nil {
				// A failed read skips a report: the next one tells.
				continue
			}

			if progress, ok := current.Progress(); ok && progress != last {
				last = progress
				report(progress)
			}
		}
	})

	return func() {
		cancel()
		wg.Wait()
	}
}
