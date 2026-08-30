# Architecture Design: media-engine-base

## 1. Hexagonal / Ports & Adapters Architecture & Go Package Breakdown

The system will strictly follow Hexagonal Architecture (Ports and Adapters) to isolate business domain logic from infrastructural concerns like HTTP handlers, NATS, and PostgreSQL.

### Go Package Breakdown
- **`cmd/`**
  - `cmd/gateway/`: Entry point for the Web Gateway HTTP server. Wires the NATS event bus, PostgreSQL DB, local file storage, and starts the HTTP server.
  - `cmd/worker/`: Entry point for the Worker Engine. Wires NATS subscriptions, DB access, and media processing handlers.
  - `cmd/janitor/`: Entry point (or separate command) for the Ephemeral TTL Janitor. Runs the scheduled cleanup loop.
- **`internal/domain/`**: Pure Go models representing core concepts.
  - `job.go`: `Job`, `JobStatus` (pending, processing, completed, failed), `Artifact` models.
  - `errors.go`: Domain-specific errors (e.g., `ErrUnsupportedMediaType`, `ErrInvalidStateTransition`).
- **`internal/ports/`**: Go interfaces defining the contract for adapters.
  - `repository.go`: `JobRepository` (Get, Create, UpdateStatus, ListExpired, Delete).
  - `eventbus.go`: `EventPublisher`, `EventSubscriber`.
  - `storage.go`: `FileStore` (Save, Delete, GetPath).
  - `processor.go`: `MediaProcessor` interface for polymorphic handler dispatch.
- **`internal/core/`**: Application services and use cases orchestrating the domain logic.
  - `upload.go`: Handles upload HTTP intent, saves to `FileStore`, creates DB record, publishes to `EventPublisher`.
  - `worker.go`: Reads from NATS, fetches job, dispatches to `MediaProcessor`, updates DB and publishes events.
  - `janitor.go`: Queries expired jobs, deletes from `FileStore`, deletes from `JobRepository`.
- **`internal/adapters/`**: Concrete implementations of ports.
  - `postgres/`: `JobRepository` implementation using `pgx` or `database/sql`.
  - `nats/`: `EventBus` implementation using the NATS Go client.
  - `localfs/`: `FileStore` implementation for local disk operations.
- **`internal/handlers/`**
  - `http/`: Gateway HTTP routes, HTMX/Templ rendering, and SSE stream handlers.
  - `media/`: Concrete processors implementing `MediaProcessor` (`image_processor.go`, `pdf_processor.go`).

## 2. Event Schema and NATS Topology

The architecture relies on NATS for asynchronous communication, using publish-subscribe patterns and queue groups for load balancing.

### NATS Topology
- **Publish-Subscribe (Pub/Sub):** The Web Gateway publishes `jobs.created` events. The Gateway also subscribes to specific job streams (`jobs.status.{job_id}`) to forward real-time updates to connected clients via SSE.
- **Queue Groups:** Worker Engine instances subscribe to `jobs.created` using a shared NATS Queue Group (e.g., `worker-engine-group`). NATS ensures that a single upload event is delivered to exactly one worker instance, preventing duplicate processing.

### Event Subject Hierarchy
- `jobs.created`: Dispatched by Gateway when an upload is persisted.
- `jobs.status.{job_id}`: Standard stream for all lifecycle updates of a specific job.
- `jobs.completed`: Dispatched by Worker upon success (can also route to `jobs.status.{job_id}`).
- `jobs.failed`: Dispatched by Worker upon non-recoverable failure.

### Event Schema (JSON Envelope)
```json
{
  "event_id": "ulid-or-uuid",
  "job_id": "ulid-or-uuid",
  "event_type": "processing", // e.g. created, processing, completed, failed
  "timestamp": "2023-10-12T10:00:00Z",
  "payload": {
    "media_type": "image/png",
    "progress": 50,
    "message": "Generating thumbnails",
    "error": null,
    "artifacts": []
  }
}
```

## 3. PostgreSQL Relational Schema & Migrations

The database acts as the single source of truth for the task state and references to local ephemeral files.

### Tables
- **`jobs`**: Tracks the top-level processing request.
  - `id` (VARCHAR/ULID, Primary Key)
  - `media_type` (VARCHAR)
  - `original_filename` (VARCHAR)
  - `file_path` (VARCHAR, reference to local FS)
  - `file_size` (BIGINT)
  - `status` (VARCHAR, enum: `pending`, `processing`, `completed`, `failed`)
  - `error_message` (TEXT, nullable)
  - `created_at` (TIMESTAMPTZ, Default NOW())
  - `updated_at` (TIMESTAMPTZ, Default NOW())
  - `expires_at` (TIMESTAMPTZ)
- **`job_artifacts`**: Tracks generated output files.
  - `id` (VARCHAR/ULID, Primary Key)
  - `job_id` (VARCHAR/ULID, Foreign Key to `jobs.id` `ON DELETE CASCADE`)
  - `artifact_type` (VARCHAR, e.g., `thumbnail`, `extracted_text`, `webp_converted`)
  - `file_path` (VARCHAR)
  - `file_size` (BIGINT)
  - `metadata` (JSONB, holds width/height/pages)
  - `created_at` (TIMESTAMPTZ)

### Indexes
- `CREATE INDEX idx_jobs_status ON jobs(status);`
- `CREATE INDEX idx_jobs_expires_at ON jobs(expires_at);` (Critical for Janitor performance)
- `CREATE INDEX idx_job_artifacts_job_id ON job_artifacts(job_id);`

## 4. HTMX + Templ View Composition and SSE Lifecycle Management

The UI logic is contained entirely on the server within the Web Gateway.

### View Composition
- **Templ Components**: `layout.templ` serves the HTML skeleton and imports Tailwind/HTMX. `upload.templ` contains the `<form>` with `hx-post="/api/v1/jobs/upload"` and `hx-target="#active-jobs"`.
- **Response Fragment Swap**: Upon successful POST, the Gateway returns an HTTP 200 (or 202) with a `job_card.templ` fragment containing the `hx-ext="sse"` attribute configured to connect to `/api/v1/jobs/{job_id}/stream`.

### SSE Lifecycle Management
- **Connection Open**: Client establishes the SSE connection. The Gateway validates the Job ID.
- **State Recovery**: Before subscribing to NATS, the Gateway queries PostgreSQL for the current job status. If the job is already `completed` or `failed`, it immediately pushes a final SSE fragment (e.g., rendered HTMX `job_result.templ`) and closes the connection cleanly.
- **Streaming**: The Gateway subscribes to NATS `jobs.status.{job_id}`. As JSON events arrive from the Worker, the Gateway renders Templ HTML fragments (e.g., a progress bar or error badge) and pushes them over the SSE stream formatted as `data: <html>...</html>\n\n`.
- **Termination**: Upon receiving a `completed` or `failed` event, the Gateway flushes the final fragment and gracefully terminates the HTTP request, which closes the client connection.

## 5. Worker Engine Pipeline and Polymorphic Handlers

### Pipeline Execution
1. Worker receives a NATS message on `jobs.created`.
2. Resolves the job in the DB and marks it `processing`.
3. Reads the payload's `media_type` and routes execution through a polymorphic dispatcher.

### Polymorphic Handlers
- **Image Processor (`image/*`)**: Leverages Go standard libraries or `github.com/h2non/bimg` (libvips wrapper) / pure Go image libs. Resizes the image and converts to `.webp` format for optimized delivery. Capable of extracting basic EXIF data (e.g., orientation, dimensions) and storing it in the `job_artifacts` metadata JSONB field.
- **PDF Processor (`application/pdf`)**: Uses CLI wrappers (e.g., calling `pdftotext` or `pdfcpu` and `pdftoppm` via `os/exec` securely) or native Go libraries. Extracts text into a `.txt` artifact and renders the first page to a thumbnail `.png` / `.webp` artifact.

### Janitor TTL Execution
A background ticker (e.g., every 5 minutes) executes `PruneExpired()`.
1. Queries: `SELECT id, file_path FROM jobs WHERE expires_at <= NOW() LIMIT 100`.
2. Iterates: Deletes the source file and associated artifact files from the `FileStore`.
3. Deletes the job record from the `JobRepository`. Due to `ON DELETE CASCADE` in PostgreSQL, artifact records are purged automatically.
4. Missing physical files log a warning but do not halt the database pruning.

## 6. Failure Modes, Concurrency, and Graceful Shutdown

### Concurrency
- **Gateway**: Standard `net/http` handles concurrent requests via goroutines. NATS subscriptions for SSE run in separate goroutines using Go channels to pipe NATS events to the HTTP writer.
- **Worker**: Uses NATS Queue Groups (`worker-engine-group`) to distribute load. Inside a worker process, `nats.Subscribe` invokes handler goroutines concurrently. DB connection pooling (`pgxpool`) handles concurrent DB updates.

### Failure Modes & Resiliency
- **Corrupt File / Handler Panic**: The worker executes media handlers within a `defer func() { recover() }` block to catch panics. If the file is unreadable, the error is caught, the DB status is set to `failed` with an explicit `error_message`, and a `jobs.failed` event is published to notify the UI.
- **NATS Unavailability**: If NATS is down during upload, the Gateway returns an HTTP 503 Service Unavailable, refusing to create the DB record and orphan the file.
- **Database Unavailability**: If PostgreSQL is unreachable, the Worker Engine logs an error, NATS NACKs (negative acknowledgments) the message if using JetStream, or it fails gracefully if using Core NATS (message loss acceptable depending on QoS design; for base design, worker updates retry locally before failing).

### Graceful Shutdown
- `context.WithCancel` combined with `os.Signal` (SIGINT/SIGTERM) propagates down the context tree.
- **Gateway**: HTTP server invokes `server.Shutdown(ctx)`, stopping new requests but allowing active SSE streams and uploads to finish within a timeout (e.g., 30s).
- **Worker**: Invokes `subscription.Drain()` to stop receiving new messages from NATS, waits for currently processing jobs to complete (bounded by context timeout), and then closes the PostgreSQL connection pool.