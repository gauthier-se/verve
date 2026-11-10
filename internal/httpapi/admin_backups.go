package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/backup"
	"github.com/gauthier-se/verve/internal/logging"
)

// archiveContentType is the media type of a backup archive, a zstd-compressed tar.
const archiveContentType = "application/zstd"

// Backup is a backup archive of the instance.
type Backup struct {
	Name      string    `json:"name" example:"verve-0.4.0-20261110T140305Z-manual.tar.zst" doc:"The file name of the archive, which downloads it."`
	CreatedAt time.Time `json:"createdAt" doc:"When the snapshot of the backup was taken."`
	Reason    string    `json:"reason" enum:"scheduled,pre-migration,manual,pre-restore" example:"manual" doc:"Why the backup was made."`
	Size      int64     `json:"size" example:"1048576" doc:"The size of the archive, in bytes."`
}

// backupsOutput is the response of GET /api/v1/admin/backups.
type backupsOutput struct {
	Body struct {
		// Items is an empty array, never null, when there is no backup.
		Items []Backup `json:"items" nullable:"false" doc:"The backups, newest first."`
		// NextRun is nil, and left out, when scheduled backups are off or not scheduled yet.
		NextRun *time.Time `json:"nextRun,omitempty" doc:"When the next scheduled backup runs, left out when scheduled backups are off."`
	}
}

// createBackupOutput is the response of POST /api/v1/admin/backups.
type createBackupOutput struct {
	Body struct {
		JobID int64 `json:"jobId" example:"42" doc:"The id of the job of the backup, which the failed jobs list when it fails."`
	}
}

// downloadBackupInput is the input of GET /api/v1/admin/backups/{name}/download.
type downloadBackupInput struct {
	Name string `path:"name" example:"verve-0.4.0-20261110T140305Z-manual.tar.zst" doc:"The file name of the archive."`
}

// downloadBackupOutput is the response of GET /api/v1/admin/backups/{name}/download.
type downloadBackupOutput struct {
	ContentType        string `header:"Content-Type" doc:"The media type of the archive."`
	ContentDisposition string `header:"Content-Disposition" doc:"Saves the archive under its file name."`
	ContentLength      string `header:"Content-Length" doc:"The size of the archive, in bytes."`
	CacheControl       string `header:"Cache-Control" doc:"no-store: the archive holds the database and the secret key."`
	Body               func(huma.Context)
}

// registerAdminBackups registers the operations of the backups for an admin: GET and POST
// /api/v1/admin/backups and GET /api/v1/admin/backups/{name}/download, on the backups of
// deps.Backups and the schedule and jobs of deps.Store, with deps.Jobs queuing the backups.
func registerAdminBackups(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "list-admin-backups",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/backups",
		Summary:     "List the backups",
		Description: "Returns the backup archives of the backups directory, newest first, and the next run of " +
			"the scheduled backups, for an admin.",
		Tags:     []string{"Backups"},
		Security: sessionSecurity(),
		Errors:   []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*backupsOutput, error) {
		if _, err := requireAdmin(ctx); err != nil {
			return nil, err
		}

		archives, err := deps.Backups.List(ctx)
		if err != nil {
			return nil, err
		}

		schedules, err := deps.Store.Jobs().ListSchedules(ctx)
		if err != nil {
			return nil, fmt.Errorf("list schedules: %w", err)
		}

		out := &backupsOutput{}
		out.Body.Items = make([]Backup, 0, len(archives))

		for _, archive := range archives {
			out.Body.Items = append(out.Body.Items, Backup{
				Name:      archive.Name,
				CreatedAt: archive.CreatedAt,
				Reason:    string(archive.Reason),
				Size:      archive.Size,
			})
		}

		if next, ok := schedules[backup.KindRun]; ok {
			out.Body.NextRun = &next
		}

		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-admin-backup",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/backups",
		Summary:     "Back up now",
		Description: "Queues a manual backup, which retention never removes, and returns the id of its job. " +
			"A backup that fails marks its job failed. Manual backups run one at a time.",
		Tags:          []string{"Backups"},
		Security:      sessionSecurity(),
		DefaultStatus: http.StatusAccepted,
		Errors:        []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*createBackupOutput, error) {
		principal, err := requireAdmin(ctx)
		if err != nil {
			return nil, err
		}

		job, err := backup.ManualJob(principal.UserID)
		if err != nil {
			return nil, err
		}

		queued, err := deps.Jobs.Enqueue(ctx, deps.Store, job)
		if err != nil {
			return nil, fmt.Errorf("queue manual backup: %w", err)
		}

		out := &createBackupOutput{}
		out.Body.JobID = queued.ID

		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "download-admin-backup",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/backups/{name}/download",
		Summary:     "Download a backup",
		Description: "Streams a backup archive as an attachment, for an admin. The archive holds the database " +
			"and the secret key: keep it safe. A name that is not the one of an archive of the backups " +
			"directory answers 404 `not_found`.",
		Tags:     []string{"Backups"},
		Security: sessionSecurity(),
		Errors:   []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
		Responses: map[string]*huma.Response{
			strconv.Itoa(http.StatusOK): {
				Description: "The archive.",
				Content: map[string]*huma.MediaType{
					archiveContentType: {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
				},
			},
		},
	}, func(ctx context.Context, in *downloadBackupInput) (*downloadBackupOutput, error) {
		principal, err := requireAdmin(ctx)
		if err != nil {
			return nil, err
		}

		f, archive, err := deps.Backups.Open(ctx, in.Name)
		if errors.Is(err, backup.ErrArchiveNotFound) {
			return nil, huma.NewError(http.StatusNotFound, "")
		}

		if err != nil {
			return nil, err
		}

		// Recorded before the archive is sent: a download cut short still gave part of it.
		if err := audit.Record(ctx, deps.Store.AuditEvents(), audit.BackupDownloaded(principal.UserID, archive.Name)); err != nil {
			return nil, errors.Join(err, f.Close())
		}

		return &downloadBackupOutput{
			ContentType: archiveContentType,
			// The name of an archive holds no quote, backslash or other character to escape, see Open.
			ContentDisposition: `attachment; filename="` + archive.Name + `"`,
			ContentLength:      strconv.FormatInt(archive.Size, 10),
			CacheControl:       "no-store",
			Body: func(hctx huma.Context) {
				defer func() { _ = f.Close() }()

				hctx.SetStatus(http.StatusOK)

				// Streamed, so that a large archive never loads into memory.
				if _, err := io.Copy(hctx.BodyWriter(), f); err != nil {
					logging.FromContext(ctx).WarnContext(ctx, "download backup", slog.String("file", archive.Name),
						slog.Any("error", err))
				}
			},
		}, nil
	})
}
