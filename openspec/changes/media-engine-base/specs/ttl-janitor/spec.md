# TTL Janitor Specification

## Purpose
The TTL Janitor provides automated background cleanup of expired media files and historical database records to prevent unbounded disk and database storage growth.

## Requirements

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
