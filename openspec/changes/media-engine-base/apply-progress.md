# Apply Progress: media-engine-base

## Current Phase: Phase 2 (NATS Event Bus & Worker Engine: Image/PDF Processors)

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

### TDD Cycle Evidence

| Component | RED Stage (Failing Test) | GREEN Stage (Implementation) | REFACTOR / Validation |
|-----------|---------------------------|------------------------------|-----------------------|
| `internal/domain` | Written `job_test.go` checking validation invariants, valid/invalid state transitions, artifact attachment, and expiration calculation. Failed with no Go files. | Implemented `errors.go`, `status.go`, `artifact.go`, `job.go`. | All tests passed: 100% domain rule coverage. |
| `internal/adapters/localfs` | Written `filestore_test.go` covering Save, Open, Delete (idempotent), Exists, GetPath, and path traversal security. Failed with no Go files. | Implemented `filestore.go` with safe directory sanitization and clean path checks. | Tests pass with `-race` flag enabled. |
| `internal/adapters/postgres` | Written `repository_test.go` testing CreateJob, GetJobByID (with artifacts), UpdateJobStatus (valid and invalid transitions), AddArtifact, ListExpiredJobs, and DeleteJob. Failed with no Go files. | Implemented `repository.go` with `pgxpool` and transaction locks on status update. | Full test suite passes against `pgxmock/v4` with zero race conditions. |
| `internal/adapters/nats` | Written `eventbus_test.go` with embedded in-memory NATS server, testing publish/subscribe, queue group load balancing, error validation, and malformed JSON handling. Failed due to missing Go files. | Implemented `eventbus.go` with structured JSON envelope serialization, subject hierarchy helpers (`jobs.created`, `jobs.status.{id}`, `jobs.completed`, `jobs.failed`), and error checks. | Tests pass with `-race` flag enabled. |
| `internal/handlers/media` | Written `processor_test.go` with synthetic PNG/JPEG/PDF fixtures, verifying `CanProcess`, thumbnail generation, medium resized images, PDF text extraction, and PDF cover thumbnail creation. Failed due to missing Go files. | Implemented `image_processor.go` (bilinear scaling, thumbnail, medium resized, webp/optimized artifacts) and `pdf_processor.go` (header validation, text extraction, styled PNG cover card rendering). | Full media processor test suite passes with zero race conditions. |
| `internal/core` | Written `worker_test.go` checking worker subscription, successful dispatch to media processors, state updates in repository, published events over event bus, unsupported media type error handling, processor failure handling, and end-to-end integration pipeline. | Implemented `worker.go` with `WorkerService`, queue group subscription, polymorphic dispatch, panic recovery (`defer func() { recover() }`), and status event publishing. | Full worker test suite passes with `-race` enabled. |

### Files Changed / Created
- `internal/adapters/nats/eventbus.go`
- `internal/adapters/nats/eventbus_test.go`
- `internal/handlers/media/image_processor.go`
- `internal/handlers/media/pdf_processor.go`
- `internal/handlers/media/processor_test.go`
- `internal/core/worker.go`
- `internal/core/worker_test.go`
- `cmd/worker/main.go`
- `openspec/changes/media-engine-base/tasks.md`
- `openspec/changes/media-engine-base/apply-progress.md`

### Test Commands Run
- `go test -v -count=1 -race ./internal/adapters/nats/...` (PASS)
- `go test -v -count=1 -race ./internal/handlers/media/...` (PASS)
- `go test -v -count=1 -race ./internal/core/...` (PASS)
- `go test -v -count=1 -race ./...` (PASS)
- `go build -o /tmp/worker ./cmd/worker` (PASS)

### Remaining Tasks (Phase 3)
```text
- [ ] Set up Web Gateway application service (internal/core/upload.go) for validating uploads, saving files, creating DB records, and publishing creation events. <!-- sdd-owner: implementation -->
- [ ] Create Templ components (internal/handlers/http/templates/) for layout, upload form, job cards, and real-time SSE progress updates. <!-- sdd-owner: implementation -->
- [ ] Implement HTTP handlers and routes (internal/handlers/http/handler.go) for multipart upload, static asset serving, and SSE streaming (/jobs/{id}/events). <!-- sdd-owner: implementation -->
- [ ] Write unit and integration tests for Web Gateway upload and SSE endpoints in internal/handlers/http/handler_test.go. <!-- sdd-owner: implementation -->
- [ ] Create Gateway entry point (cmd/gateway/main.go) with graceful shutdown. <!-- sdd-owner: implementation -->
- [ ] Implement Ephemeral TTL Janitor core service (internal/core/janitor.go) and background scheduler for pruning expired records and physical files. <!-- sdd-owner: implementation -->
- [ ] Write unit/integration tests for TTL Janitor in internal/core/janitor_test.go. <!-- sdd-owner: implementation -->
- [ ] Create Janitor entry point (cmd/janitor/main.go) or integrate background routine into gateway/worker. <!-- sdd-owner: implementation -->
- [ ] Create docker-compose.yml defining PostgreSQL, NATS, Web Gateway, Worker Engine, and Janitor services for end-to-end local validation. <!-- sdd-owner: implementation -->
- [ ] Perform end-to-end integration verification (upload image/PDF via web interface, verify background processing, SSE stream updates, and TTL cleanup). <!-- sdd-owner: implementation -->
```
