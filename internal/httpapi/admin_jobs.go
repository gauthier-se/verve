package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/store"
)

// Job is a finished background job, without its payload, which can hold anything.
type Job struct {
	ID       int64  `json:"id" example:"42" doc:"The id of the job."`
	Kind     string `json:"kind" example:"backup.run" doc:"The kind of the job."`
	Status   string `json:"status" enum:"completed,failed,cancelled" example:"failed" doc:"The status of the job."`
	Attempts int64  `json:"attempts" example:"5" doc:"The number of failed attempts of the job. A lost lease is not one."`
	// LastError never holds health values or secrets, see the job worker.
	LastError  string    `json:"lastError,omitempty" example:"disk full" doc:"The error of the last failure of the job, left out when it never failed."`
	CreatedAt  time.Time `json:"createdAt" doc:"When the job was enqueued."`
	FinishedAt time.Time `json:"finishedAt" doc:"When the job ended."`
}

// jobsOutput is the response of GET /api/v1/admin/jobs.
type jobsOutput struct {
	Body Page[Job]
}

// adminJobsInput is the input of GET /api/v1/admin/jobs.
type adminJobsInput struct {
	PageParams

	Status store.JobStatus `query:"status" required:"true" enum:"completed,failed,cancelled" example:"failed" doc:"Keeps the jobs of this status."`
}

// registerAdminJobs registers GET /api/v1/admin/jobs, the finished jobs of a status for an admin,
// read in repos.
func registerAdminJobs(api huma.API, repos store.Repositories) {
	huma.Register(api, huma.Operation{
		OperationID: "list-admin-jobs",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/jobs",
		Summary:     "List the finished jobs",
		Description: "Returns the background jobs of a finished status, newest first by the time they ended, for an admin. Their payloads are never returned.",
		Tags:        []string{"Jobs"},
		Security:    sessionSecurity(),
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *adminJobsInput) (*jobsOutput, error) {
		if _, err := requireAdmin(ctx); err != nil {
			return nil, err
		}

		jobs, next, err := listPage(in.PageParams, func(page store.PageRequest) ([]store.Job, error) {
			return repos.Jobs().ListByStatus(ctx, in.Status, page)
		}, store.Job.Cursor)
		if err != nil {
			return nil, fmt.Errorf("list %s jobs: %w", in.Status, err)
		}

		items := make([]Job, 0, len(jobs))
		for _, job := range jobs {
			items = append(items, Job{
				ID:         job.ID,
				Kind:       job.Kind,
				Status:     string(job.Status),
				Attempts:   job.Attempts,
				LastError:  job.LastError,
				CreatedAt:  job.CreatedAt,
				FinishedAt: job.FinishedAt,
			})
		}

		return &jobsOutput{Body: Page[Job]{Items: items, NextCursor: next}}, nil
	})
}
