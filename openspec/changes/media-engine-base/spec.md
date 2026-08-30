# Functional Specification: media-engine-base

## Overview
This document specifies the system requirements and acceptance criteria for the foundational asynchronous media and document processing engine (`media-engine-base`). The architecture comprises a Go Web Gateway, NATS Event Bus, polymorphic Worker Engine, PostgreSQL Task Repository, and Ephemeral TTL Janitor.

---

## 1. Web Gateway

### Requirement: Media Upload Processing
The Web Gateway MUST provide an HTTP POST endpoint (`/upload` / `/api/v1/jobs/upload`) accepting `multipart/form-data` uploads containing image and PDF files, validate payload constraints, persist the raw file to ephemeral storage, record the initial job in PostgreSQL with status `pending`, publish a job creation event to NATS, and return an immediate response containing the job ID.

#### Scenario: Successful media upload
- GIVEN an HTTP client submits a valid image (PNG/JPEG) or PDF file via `multipart/form-data`
- WHEN the Web Gateway processes the upload request
- THEN the file MUST be stored in the configured temporary storage directory
- AND a new task record MUST be created in the database with status `pending`
- AND a `jobs.created` event MUST be published to NATS with the job ID and file metadata
- AND the HTTP response MUST return status code `202 Accepted` (or `200 OK` with HTMX partial) containing the job ID and SSE stream connection target.

#### Scenario: Unsupported file format or empty payload
- GIVEN an HTTP client submits an empty payload or unsupported file type
- WHEN the upload endpoint validates the file
- THEN the Web Gateway MUST reject the request with HTTP `400 Bad Request`
- AND no task record SHALL be created in PostgreSQL
- AND no event SHALL be published to NATS.

### Requirement: Server-Side Rendered HTMX Views
The Web Gateway MUST serve HTML pages using Templ components styled with Tailwind CSS, supporting interactive upload forms and dynamic DOM swapping via HTMX without full page reloads.

#### Scenario: Initial dashboard view load
- GIVEN an HTTP GET request to `/`
- WHEN the Web Gateway processes the request
- THEN it MUST return status `200 OK` with a valid HTML document containing the upload form, active jobs panel, and HTMX attributes (`hx-post`, `hx-target`, `hx-swap`).

#### Scenario: Dynamic upload trigger via HTMX
- GIVEN a user submits a file through the HTMX-enhanced form
- WHEN the submission is received by the upload endpoint
- THEN the gateway MUST return an HTML fragment representing the initial processing card targeting the active job container.

### Requirement: Server-Sent Events (SSE) Stream
The Web Gateway MUST provide an SSE endpoint (`/jobs/{id}/events` or `/api/v1/jobs/{id}/stream`) that opens a long-lived HTTP connection (`Content-Type: text/event-stream`), subscribes to real-time events for the specific job, and streams progress, status transitions, and final results to the client.

#### Scenario: Real-time progress updates over SSE
- GIVEN an active job with an open SSE connection from a client
- WHEN a status transition or progress event (`processing`, `completed`, `failed`) occurs on NATS
- THEN the SSE handler MUST emit a formatted SSE message containing the event type and HTML fragment or JSON payload
- AND the connection MUST remain open until the job reaches a terminal state (`completed` or `failed`) or the client disconnects.

#### Scenario: SSE client reconnect after disconnection
- GIVEN a client whose SSE connection was interrupted during processing
- WHEN the client reconnects to the SSE endpoint for the job
- THEN the Web Gateway MUST query the current task state from PostgreSQL
- AND immediately emit the current state before resuming live NATS event streaming.

---

## 2. Event Bus (NATS)

### Requirement: Structured Event Envelope
All messages exchanged over NATS MUST adhere to a consistent JSON-encoded event envelope schema containing metadata fields (`event_id`, `event_type`, `job_id`, `timestamp`) and a payload body specific to the event type.

#### Scenario: Envelope serialization and deserialization
- GIVEN a service constructing a job notification event
- WHEN the event is serialized and published to NATS
- THEN the message payload MUST be valid JSON conforming to the envelope schema
- AND the receiving service MUST successfully parse the envelope and validate required fields before dispatching to handlers.

### Requirement: Topic Hierarchy and Subject Conventions
The system MUST publish and subscribe to NATS topics using structured dot-separated subject hierarchies:
- `jobs.created`: Broadcast when a new job is registered by the gateway.
- `jobs.status.{job_id}`: Job-specific status updates and progress events.
- `jobs.completed`: Emitted when worker processing completes successfully.
- `jobs.failed`: Emitted when worker processing encounters a non-recoverable failure.

#### Scenario: Targeted job status subscription
- GIVEN the Web Gateway handling an SSE connection for job `job-123`
- WHEN the gateway subscribes to `jobs.status.job-123`
- THEN it MUST receive only status updates matching that specific job identifier.

#### Scenario: Worker queue group subscription
- GIVEN multiple Worker Engine instances connected to NATS
- WHEN a `jobs.created` message is published
- THEN NATS MUST deliver the message to exactly one worker instance using a shared queue group (e.g., `worker-engine-group`) to prevent duplicate processing.

---

## 3. Worker Engine

### Requirement: Polymorphic Handler Dispatching
The Worker Engine MUST inspect the media type / MIME type declared in the job payload and dynamically dispatch execution to the corresponding registered media handler (e.g., Image Processor for `image/*`, PDF Processor for `application/pdf`).

#### Scenario: Dispatching valid media type
- GIVEN a worker receives a `jobs.created` message with media type `image/png`
- WHEN the dispatcher resolves the handler
- THEN it MUST invoke the Image Processor implementation.

#### Scenario: Unrecognized media type handling
- GIVEN a worker receives a job with an unsupported media type
- WHEN the dispatcher attempts resolution
- THEN it MUST return an error without crashing
- AND transition the job status to `failed` with an explicit error message
- AND publish a failure event to `jobs.failed` and `jobs.status.{job_id}`.

### Requirement: Image Processing Handler
The Image Processing Handler MUST support image transformations including resizing (e.g., thumbnail generation, standard display resolutions) and format conversion (e.g., PNG/JPEG/WEBP), saving output artifacts to ephemeral storage and recording artifact metadata.

#### Scenario: Successful image resizing and thumbnail generation
- GIVEN a valid image job pointing to an uploaded PNG or JPEG file
- WHEN the Image Processing Handler executes
- THEN it MUST generate the requested resized image variants and thumbnails
- AND store the resulting files in the output directory
- AND return artifact references (path, dimensions, byte size) to the worker coordinator.

### Requirement: PDF Processing Handler
The PDF Processing Handler MUST support document processing operations including plain text extraction and first-page cover thumbnail image rendering, saving output artifacts to ephemeral storage.

#### Scenario: PDF text extraction and cover thumbnail rendering
- GIVEN a valid PDF job pointing to an uploaded document
- WHEN the PDF Processing Handler executes
- THEN it MUST extract text content from document pages
- AND render a representative cover thumbnail image
- AND store the output text and image artifacts in ephemeral storage.

### Requirement: Resilient Error Handling and State Updates
The Worker Engine MUST guarantee that every processed job updates PostgreSQL with its latest status (`processing`, `completed`, `failed`), persists error descriptions upon failure, and publishes status update events over NATS regardless of processing outcome.

#### Scenario: Graceful failure on corrupted file
- GIVEN a worker receives a job whose underlying file is truncated or corrupted
- WHEN the media handler attempts decoding and encounters an error
- THEN the worker MUST catch the error, mark the job as `failed` in PostgreSQL with the error message
- AND publish a `jobs.failed` event on NATS.

---

## 4. Task Repository (PostgreSQL)

### Requirement: Task and Artifact Data Schema
The system MUST define and maintain a relational PostgreSQL schema containing at least:
1. `jobs`: Storing `id` (UUID/ULID primary key), `media_type` (VARCHAR), `original_filename` (VARCHAR), `file_path` (VARCHAR), `file_size` (BIGINT), `status` (VARCHAR: `pending`, `processing`, `completed`, `failed`), `error_message` (TEXT, nullable), `created_at` (TIMESTAMPTZ), `updated_at` (TIMESTAMPTZ), and `expires_at` (TIMESTAMPTZ).
2. `job_artifacts`: Storing `id` (UUID/ULID), `job_id` (FK referencing `jobs(id)` ON DELETE CASCADE), `artifact_type` (VARCHAR, e.g., `thumbnail`, `extracted_text`, `converted_image`), `file_path` (VARCHAR), `file_size` (BIGINT), `metadata` (JSONB, e.g., width, height, page count), and `created_at` (TIMESTAMPTZ).

#### Scenario: Schema initialization and migrations
- GIVEN an uninitialized or migrating database
- WHEN database migrations run
- THEN the `jobs` and `job_artifacts` tables, indexes on `status`, `created_at`, and `expires_at`, and foreign key constraints MUST be created successfully.

### Requirement: State Lifecycle and Invariant Transitions
The repository MUST enforce valid state transitions across the job lifecycle:
- Allowed transitions: `pending -> processing`, `processing -> completed`, `processing -> failed`, and `pending -> failed`.
- Terminal states: `completed` and `failed` SHALL NOT transition back to `pending` or `processing`.

#### Scenario: Valid state transition
- GIVEN a job with status `pending`
- WHEN the repository method `UpdateJobStatus` is called with `status = processing`
- THEN the job record MUST be updated to `processing` and `updated_at` timestamp refreshed.

#### Scenario: Invalid state transition rejection
- GIVEN a job with terminal status `completed`
- WHEN an update attempts to set status back to `pending`
- THEN the repository MUST reject the transition with an error and preserve the existing record state.

### Requirement: Task Repository Query Methods
The repository MUST provide methods for creating a job, retrieving a job by ID (including associated artifacts), updating status and error details, appending artifact records, and listing expired jobs eligible for deletion.

#### Scenario: Fetching job with associated artifacts
- GIVEN a completed job with multiple generated artifacts
- WHEN `GetJobByID` is called
- THEN the returned job entity MUST include all associated artifact records.

---

## 5. TTL Janitor

### Requirement: Scheduled Expiration Sweep
The TTL Janitor MUST execute periodic cleanup cycles at configurable intervals (e.g., every 5 minutes) to identify jobs and artifacts whose `expires_at` timestamp is earlier than the current system time (`expires_at <= NOW()`).

#### Scenario: Periodic sweep execution
- GIVEN a configured cleanup interval of `N` minutes and expired job records in PostgreSQL
- WHEN the scheduler timer triggers
- THEN the Janitor MUST query for all expired job records and initiate resource pruning.

### Requirement: Ephemeral File System Cleanup
The TTL Janitor MUST delete all raw uploaded media files and generated artifact files from the local filesystem associated with expired jobs.

#### Scenario: File removal on expiration
- GIVEN an expired job whose original file and generated artifacts exist on the filesystem
- WHEN the cleanup routine processes the job
- THEN all associated physical files MUST be removed from disk
- AND if a file is already missing from disk, the routine MUST log a warning and continue without failing the remaining cleanup tasks.

### Requirement: Database Record Pruning and Cascade Cleanup
The TTL Janitor MUST delete or archive expired job records from the database, ensuring all associated `job_artifacts` records are cleanly deleted via foreign key cascading.

#### Scenario: Cascade database deletion
- GIVEN an expired job with linked records in `job_artifacts`
- WHEN the Janitor deletes the job record from `jobs`
- THEN the corresponding `job_artifacts` rows MUST be removed automatically via CASCADE constraint
- AND the deletion count MUST be logged for observability.
