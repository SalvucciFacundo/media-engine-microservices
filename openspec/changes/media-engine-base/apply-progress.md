# Apply Progress: media-engine-base

## Current Phase: Phase 1 (Domain, Ports, PostgreSQL & LocalFS Adapters)

### Completed Tasks
- [x] Initialize Go module structure (`github.com/SalvucciFacundo/media-engine-microservices`).
- [x] Write domain models (`internal/domain/job.go`, `internal/domain/status.go`, `internal/domain/artifact.go`, `internal/domain/errors.go`).
- [x] Write unit tests for domain invariants and state transitions in `internal/domain/job_test.go`.
- [x] Define port interfaces (`internal/ports/repository.go`, `internal/ports/storage.go`, `internal/ports/eventbus.go`, `internal/ports/processor.go`).
- [x] Implement `localfs` file store adapter (`internal/adapters/localfs/filestore.go`) with path traversal protection.
- [x] Write tests for `localfs` file store in `internal/adapters/localfs/filestore_test.go`.
- [x] Implement PostgreSQL job repository adapter (`internal/adapters/postgres/repository.go`) using `pgxpool` and state transition locks, including database migrations (`migrations/000001_init_schema.up.sql`, `migrations/000001_init_schema.down.sql`).
- [x] Write integration/mock tests for PostgreSQL repository in `internal/adapters/postgres/repository_test.go`.

### TDD Cycle Evidence

| Component | RED Stage (Failing Test) | GREEN Stage (Implementation) | REFACTOR / Validation |
|-----------|---------------------------|------------------------------|-----------------------|
| `internal/domain` | Written `job_test.go` checking validation invariants, valid/invalid state transitions, artifact attachment, and expiration calculation. Failed with no Go files. | Implemented `errors.go`, `status.go`, `artifact.go`, `job.go`. | All tests passed: 100% domain rule coverage. |
| `internal/adapters/localfs` | Written `filestore_test.go` covering Save, Open, Delete (idempotent), Exists, GetPath, and path traversal security. Failed with no Go files. | Implemented `filestore.go` with safe directory sanitization and clean path checks. | Tests pass with `-race` flag enabled. |
| `internal/adapters/postgres` | Written `repository_test.go` testing CreateJob, GetJobByID (with artifacts), UpdateJobStatus (valid and invalid transitions), AddArtifact, ListExpiredJobs, and DeleteJob. Failed with no Go files. | Implemented `repository.go` with `pgxpool` and transaction locks on status update. | Full test suite passes against `pgxmock/v4` with zero race conditions. |

### Files Changed / Created
- `go.mod`, `go.sum`
- `migrations/000001_init_schema.up.sql`
- `migrations/000001_init_schema.down.sql`
- `internal/domain/errors.go`
- `internal/domain/status.go`
- `internal/domain/artifact.go`
- `internal/domain/job.go`
- `internal/domain/job_test.go`
- `internal/ports/repository.go`
- `internal/ports/storage.go`
- `internal/ports/eventbus.go`
- `internal/ports/processor.go`
- `internal/adapters/localfs/filestore.go`
- `internal/adapters/localfs/filestore_test.go`
- `internal/adapters/postgres/repository.go`
- `internal/adapters/postgres/repository_test.go`
- `openspec/changes/media-engine-base/tasks.md`

### Test Commands Run
- `go test -v -race ./internal/domain/...` (PASS)
- `go test -v -race ./internal/adapters/localfs/...` (PASS)
- `go test -v -race ./internal/adapters/postgres/...` (PASS)
- `go test -count=1 -race ./...` (PASS)

### Remaining Tasks (Phase 2 & Phase 3)
```text
- [ ] Define NATS event bus ports (internal/ports/eventbus.go) for publisher and subscriber, and processor port (internal/ports/processor.go). <!-- sdd-owner: implementation -->
- [ ] Implement NATS event bus adapter (internal/adapters/nats/eventbus.go) with JSON event envelope serialization and subject helper functions. <!-- sdd-owner: implementation -->
- [ ] Write unit/integration tests for NATS serialization and event handling in internal/adapters/nats/eventbus_test.go. <!-- sdd-owner: implementation -->
- [ ] Implement Image Media Processor (internal/handlers/media/image_processor.go) supporting resizing and WebP conversion. <!-- sdd-owner: implementation -->
- [ ] Implement PDF Media Processor (internal/handlers/media/pdf_processor.go) supporting text extraction and cover rendering. <!-- sdd-owner: implementation -->
- [ ] Write unit tests for media processors using sample fixtures in internal/handlers/media/processor_test.go (Strict TDD). <!-- sdd-owner: implementation -->
- [ ] Implement Worker Engine application service (internal/core/worker.go) subscribing to NATS queue groups, dispatching to media processors, updating PostgreSQL status, and publishing status events. <!-- sdd-owner: implementation -->
- [ ] Write integration tests for the worker pipeline in internal/core/worker_test.go. <!-- sdd-owner: implementation -->
- [ ] Create Worker entry point (cmd/worker/main.go) with graceful shutdown support. <!-- sdd-owner: implementation -->
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
