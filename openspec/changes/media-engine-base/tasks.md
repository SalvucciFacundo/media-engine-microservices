# Tasks: media-engine-base

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 1200-1800 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1: Domain, Ports, & PostgreSQL/LocalFS Adapters → PR 2: NATS Event Bus & Worker Engine (Image/PDF Processors) → PR 3: Web Gateway (Templ, HTMX, SSE) & TTL Janitor / Docker Compose |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

```text
Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High
```

---

## Task Breakdown

### Phase 1: Domain, Ports, PostgreSQL, & LocalFS Adapters
- [x] Initialize Go module structure (`cmd/`, `internal/domain/`, `internal/ports/`, `internal/adapters/postgres/`, `internal/adapters/localfs/`). <!-- sdd-owner: implementation -->
- [x] Write domain models (`internal/domain/job.go`) for `Job`, `JobStatus`, and `Artifact`, along with domain errors (`internal/domain/errors.go`). <!-- sdd-owner: implementation -->
- [x] Write unit tests for domain invariants and state transitions in `internal/domain/job_test.go` (Strict TDD: RED → GREEN). <!-- sdd-owner: implementation -->
- [x] Define port interfaces (`internal/ports/repository.go` for `JobRepository`, `internal/ports/storage.go` for `FileStore`). <!-- sdd-owner: implementation -->
- [x] Implement `localfs` file store adapter (`internal/adapters/localfs/filestore.go`) supporting Save, Delete, and GetPath. <!-- sdd-owner: implementation -->
- [x] Write integration tests for `localfs` file store in `internal/adapters/localfs/filestore_test.go`. <!-- sdd-owner: implementation -->
- [x] Implement PostgreSQL job repository adapter (`internal/adapters/postgres/repository.go`) using `pgxpool`, including database migrations for `jobs` and `job_artifacts` tables with indexes. <!-- sdd-owner: implementation -->
- [x] Write integration tests for PostgreSQL repository with testcontainers or test database in `internal/adapters/postgres/repository_test.go`. <!-- sdd-owner: implementation -->

### Phase 2: NATS Event Bus & Worker Engine (Image/PDF Processors)
- [ ] Define NATS event bus ports (`internal/ports/eventbus.go`) for publisher and subscriber, and processor port (`internal/ports/processor.go`). <!-- sdd-owner: implementation -->
- [ ] Implement NATS event bus adapter (`internal/adapters/nats/eventbus.go`) with JSON event envelope serialization and subject helper functions. <!-- sdd-owner: implementation -->
- [ ] Write unit/integration tests for NATS serialization and event handling in `internal/adapters/nats/eventbus_test.go`. <!-- sdd-owner: implementation -->
- [ ] Implement Image Media Processor (`internal/handlers/media/image_processor.go`) supporting resizing and WebP conversion. <!-- sdd-owner: implementation -->
- [ ] Implement PDF Media Processor (`internal/handlers/media/pdf_processor.go`) supporting text extraction and cover rendering. <!-- sdd-owner: implementation -->
- [ ] Write unit tests for media processors using sample fixtures in `internal/handlers/media/processor_test.go` (Strict TDD). <!-- sdd-owner: implementation -->
- [ ] Implement Worker Engine application service (`internal/core/worker.go`) subscribing to NATS queue groups, dispatching to media processors, updating PostgreSQL status, and publishing status events. <!-- sdd-owner: implementation -->
- [ ] Write integration tests for the worker pipeline in `internal/core/worker_test.go`. <!-- sdd-owner: implementation -->
- [ ] Create Worker entry point (`cmd/worker/main.go`) with graceful shutdown support. <!-- sdd-owner: implementation -->

### Phase 3: Web Gateway, Templ Views, HTMX, SSE, & TTL Janitor
- [ ] Set up Web Gateway application service (`internal/core/upload.go`) for validating uploads, saving files, creating DB records, and publishing creation events. <!-- sdd-owner: implementation -->
- [ ] Create Templ components (`internal/handlers/http/templates/`) for layout, upload form, job cards, and real-time SSE progress updates. <!-- sdd-owner: implementation -->
- [ ] Implement HTTP handlers and routes (`internal/handlers/http/handler.go`) for multipart upload, static asset serving, and SSE streaming (`/jobs/{id}/events`). <!-- sdd-owner: implementation -->
- [ ] Write unit and integration tests for Web Gateway upload and SSE endpoints in `internal/handlers/http/handler_test.go`. <!-- sdd-owner: implementation -->
- [ ] Create Gateway entry point (`cmd/gateway/main.go`) with graceful shutdown. <!-- sdd-owner: implementation -->
- [ ] Implement Ephemeral TTL Janitor core service (`internal/core/janitor.go`) and background scheduler for pruning expired records and physical files. <!-- sdd-owner: implementation -->
- [ ] Write unit/integration tests for TTL Janitor in `internal/core/janitor_test.go`. <!-- sdd-owner: implementation -->
- [ ] Create Janitor entry point (`cmd/janitor/main.go`) or integrate background routine into gateway/worker. <!-- sdd-owner: implementation -->
- [ ] Create `docker-compose.yml` defining PostgreSQL, NATS, Web Gateway, Worker Engine, and Janitor services for end-to-end local validation. <!-- sdd-owner: implementation -->
- [ ] Perform end-to-end integration verification (upload image/PDF via web interface, verify background processing, SSE stream updates, and TTL cleanup). <!-- sdd-owner: implementation -->
