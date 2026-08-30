# Apply Progress: media-engine-base

## Current Phase: Phase 3 (Web Gateway, Templ Views, HTMX, SSE, TTL Janitor & Docker Compose)

### Completed Tasks
- [x] Initialize Go module structure (`github.com/SalvucciFacundo/media-engine-microservices`).
- [x] Write domain models (`internal/domain/job.go`, `internal/domain/status.go`, `internal/domain/artifact.go`, `internal/domain/errors.go`).
- [x] Write unit tests for domain invariants and state transitions in `internal/domain/job_test.go`.
- [x] Define port interfaces (`internal/ports/repository.go`, `internal/ports/storage.go`, `internal/ports/eventbus.go`, `internal/ports/processor.go`).
- [x] Implement `localfs` file store adapter (`internal/adapters/localfs/filestore.go`) with path traversal protection.
- [x] Write tests for `localfs` file store in `internal/adapters/localfs/filestore_test.go`.
- [x] Implement PostgreSQL job repository adapter (`internal/adapters/postgres/repository.go`) using `pgxpool` and state transition locks, including database migrations (`migrations/000001_init_schema.up.sql`, `migrations/000001_init_schema.down.sql`).
- [x] Write integration/mock tests for PostgreSQL repository in `internal/adapters/postgres/repository_test.go`.
- [x] Define NATS event bus ports (`internal/ports/eventbus.go`) and processor port (`internal/ports/processor.go`).
- [x] Implement NATS event bus adapter (`internal/adapters/nats/eventbus.go`) with JSON event envelope serialization and subject helper functions.
- [x] Write unit/integration tests for NATS serialization and event handling in `internal/adapters/nats/eventbus_test.go`.
- [x] Implement Image Media Processor (`internal/handlers/media/image_processor.go`) supporting resizing (thumbnail, medium) and WebP/optimized image conversion.
- [x] Implement PDF Media Processor (`internal/handlers/media/pdf_processor.go`) supporting text extraction and cover card thumbnail rendering.
- [x] Write unit tests for media processors using sample fixtures in `internal/handlers/media/processor_test.go` (Strict TDD).
- [x] Implement Worker Engine application service (`internal/core/worker.go`) subscribing to NATS queue groups, dispatching to media processors, updating PostgreSQL status, and publishing status events.
- [x] Write unit and integration tests for the worker pipeline in `internal/core/worker_test.go`.
- [x] Create Worker entry point (`cmd/worker/main.go`) with graceful shutdown support.
- [x] Implement Web Gateway application service (`internal/core/upload.go`) for validating uploads, saving files, creating DB records, and publishing creation events with rollback support.
- [x] Write unit tests for upload service in `internal/core/upload_test.go` (Strict TDD).
- [x] Implement Ephemeral TTL Janitor core service (`internal/core/janitor.go`) and background scheduler for pruning expired records and physical files.
- [x] Write unit and integration tests for TTL Janitor in `internal/core/janitor_test.go` (Strict TDD).
- [x] Create Templ components (`internal/handlers/http/templates/`) for layout, upload form, job cards, artifacts list, and real-time SSE progress updates.
- [x] Implement HTTP handlers and routes (`internal/handlers/http/handler.go`) for multipart upload, static asset serving, SSE streaming (`/jobs/{id}/events`), and artifact downloads (`/artifacts/{id}/download`).
- [x] Write unit and integration tests for Web Gateway upload and SSE streaming in `internal/handlers/http/handler_test.go` (Strict TDD).
- [x] Create Gateway entry point (`cmd/gateway/main.go`) with graceful shutdown and background Janitor ticker.
- [x] Create Janitor standalone entry point (`cmd/janitor/main.go`) with graceful shutdown.
- [x] Create multi-stage `Dockerfile` and `docker-compose.yml` configuring PostgreSQL, NATS, Web Gateway, and Worker Engine services.

### TDD Cycle Evidence

| Component | RED Stage (Failing Test) | GREEN Stage (Implementation) | REFACTOR / Validation |
|-----------|---------------------------|------------------------------|-----------------------|
| `internal/domain` | Written `job_test.go` checking validation invariants, valid/invalid state transitions, artifact attachment, and expiration calculation. Failed with no Go files. | Implemented `errors.go`, `status.go`, `artifact.go`, `job.go`. | All tests passed: 100% domain rule coverage. |
| `internal/adapters/localfs` | Written `filestore_test.go` covering Save, Open, Delete (idempotent), Exists, GetPath, and path traversal security. Failed with no Go files. | Implemented `filestore.go` with safe directory sanitization and clean path checks. | Tests pass with `-race` flag enabled. |
| `internal/adapters/postgres` | Written `repository_test.go` testing CreateJob, GetJobByID (with artifacts), UpdateJobStatus (valid and invalid transitions), AddArtifact, ListExpiredJobs, and DeleteJob. Failed with no Go files. | Implemented `repository.go` with `pgxpool` and transaction locks on status update. | Full test suite passes against `pgxmock/v4` with zero race conditions. |
| `internal/adapters/nats` | Written `eventbus_test.go` with embedded in-memory NATS server, testing publish/subscribe, queue group load balancing, error validation, and malformed JSON handling. Failed due to missing Go files. | Implemented `eventbus.go` with structured JSON envelope serialization, subject hierarchy helpers (`jobs.created`, `jobs.status.{id}`, `jobs.completed`, `jobs.failed`), and error checks. | Tests pass with `-race` flag enabled. |
| `internal/handlers/media` | Written `processor_test.go` with synthetic PNG/JPEG/PDF fixtures, verifying `CanProcess`, thumbnail generation, medium resized images, PDF text extraction, and PDF cover thumbnail creation. Failed due to missing Go files. | Implemented `image_processor.go` (bilinear scaling, thumbnail, medium resized, webp/optimized artifacts) and `pdf_processor.go` (header validation, text extraction, styled PNG cover card rendering). | Full media processor test suite passes with zero race conditions. |
| `internal/core` (Worker) | Written `worker_test.go` checking worker subscription, successful dispatch to media processors, state updates in repository, published events over event bus, unsupported media type error handling, processor failure handling, and end-to-end integration pipeline. | Implemented `worker.go` with `WorkerService`, queue group subscription, polymorphic dispatch, panic recovery (`defer func() { recover() }`), and status event publishing. | Full worker test suite passes with `-race` enabled. |
| `internal/core` (Upload) | Written `upload_test.go` testing ProcessUpload with valid files, supported media types, unsupported media types, empty files, and rollback on DB / Publisher failures. Failed due to missing symbols. | Implemented `upload.go` with MIME-type validation, unique storage paths, job registration, and full rollback protection on failure. | Tests pass with `-race` enabled. |
| `internal/core` (Janitor) | Written `janitor_test.go` testing PruneExpired with expired jobs, associated artifact physical file deletion, non-expired isolation, missing file tolerance, and background ticker start/stop. Failed due to missing symbols. | Implemented `janitor.go` with `JanitorService`, query limiting, multi-file deletion, and background ticker routine. | Tests pass with `-race` enabled. |
| `internal/handlers/http` | Written `handler_test.go` testing dashboard rendering, HTMX multipart upload (JobCard swap), JSON API upload, SSE live streaming (`/jobs/{id}/events`), and artifact downloads. Failed due to missing package. | Implemented `handler.go` with Go 1.22+ routing patterns, HTMX detection, SSE event streams with flusher, and Templ view integration. | All handler tests pass with zero race conditions. |

### Files Changed / Created
- `internal/core/upload.go`
- `internal/core/upload_test.go`
- `internal/core/janitor.go`
- `internal/core/janitor_test.go`
- `internal/handlers/http/templates/layout.templ`
- `internal/handlers/http/templates/layout_templ.go`
- `internal/handlers/http/templates/upload_form.templ`
- `internal/handlers/http/templates/upload_form_templ.go`
- `internal/handlers/http/templates/job_card.templ`
- `internal/handlers/http/templates/job_card_templ.go`
- `internal/handlers/http/templates/artifacts_list.templ`
- `internal/handlers/http/templates/artifacts_list_templ.go`
- `internal/handlers/http/templates/dashboard.templ`
- `internal/handlers/http/templates/dashboard_templ.go`
- `internal/handlers/http/handler.go`
- `internal/handlers/http/handler_test.go`
- `cmd/gateway/main.go`
- `cmd/janitor/main.go`
- `Dockerfile`
- `docker-compose.yml`
- `openspec/changes/media-engine-base/tasks.md`
- `openspec/changes/media-engine-base/apply-progress.md`

### Test Commands Run
- `go test -v -count=1 -race ./internal/adapters/localfs/...` (PASS)
- `go test -v -count=1 -race ./internal/adapters/nats/...` (PASS)
- `go test -v -count=1 -race ./internal/adapters/postgres/...` (PASS)
- `go test -v -count=1 -race ./internal/domain/...` (PASS)
- `go test -v -count=1 -race ./internal/handlers/media/...` (PASS)
- `go test -v -count=1 -race ./internal/core/...` (PASS)
- `go test -v -count=1 -race ./internal/handlers/http/...` (PASS)
- `go test -v -count=1 -race ./...` (PASS)
- `go build -o /tmp/gateway ./cmd/gateway` (PASS)
- `go build -o /tmp/worker ./cmd/worker` (PASS)
- `go build -o /tmp/janitor ./cmd/janitor` (PASS)

### Remaining Tasks
None. All tasks across Phase 1, Phase 2, and Phase 3 are complete.
