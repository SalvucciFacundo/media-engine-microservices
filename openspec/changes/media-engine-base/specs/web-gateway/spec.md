# Web Gateway Specification

## Purpose
The Web Gateway serves as the user-facing HTTP ingress for the media processing engine. It provides web views via Templ and HTMX, accepts media uploads, dispatches processing jobs, and streams real-time state transitions to clients via Server-Sent Events (SSE).

## Requirements

### Requirement: Media Upload Processing
The Web Gateway MUST provide an HTTP POST endpoint (`/api/v1/jobs/upload` or `/upload`) accepting `multipart/form-data` uploads containing image and PDF files, validate basic payload constraints, persist the raw file to ephemeral storage, record the initial job in PostgreSQL with status `pending`, publish a job creation event to NATS, and return an immediate response containing the generated job ID.

#### Scenario: Successful media upload
- GIVEN an HTTP client submits a valid image (PNG/JPEG) or PDF file via `multipart/form-data`
- WHEN the Web Gateway processes the upload request
- THEN the file MUST be stored in the configured temporary storage directory
- AND a new task record MUST be created in the database with status `pending`
- AND a `job.created` event MUST be published to NATS with the job ID and file metadata
- AND the HTTP response MUST return status code `202 Accepted` (or `200 OK` with HTMX partial) containing the job ID and SSE stream connection target.

#### Scenario: Unsupported file format or empty payload
- GIVEN an HTTP client submits an empty payload or unsupported file type (e.g., `.exe`)
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
The Web Gateway MUST provide an SSE endpoint (`/api/v1/jobs/{id}/stream` or `/jobs/{id}/events`) that opens a long-lived HTTP connection (`Content-Type: text/event-stream`), subscribes to real-time events for the specific job, and streams progress, status transitions, and final results to the client.

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
