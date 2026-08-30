# Task Repository Specification

## Purpose
The Task Repository manages persistent relational storage in PostgreSQL for media jobs, tracking job states, timestamps, metadata, and generated artifact references across the processing lifecycle.

## Requirements

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
