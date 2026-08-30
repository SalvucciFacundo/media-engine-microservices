# Worker Engine Specification

## Purpose
The Worker Engine consumes background processing jobs from NATS, executes polymorphic media handlers tailored to specific MIME types (Images and PDFs), persists output artifacts to ephemeral storage, and records execution status updates in PostgreSQL.

## Requirements

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
