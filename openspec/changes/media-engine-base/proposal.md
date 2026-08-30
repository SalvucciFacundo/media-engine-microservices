# SDD Proposal: media-engine-base

## 1. Intent and Business Goals
The "media-engine-base" initiative aims to establish the foundational architecture for an asynchronous media and document processing engine. The goal is to provide a robust, scalable system that can handle background processing of user uploads (such as images and PDFs) without blocking the user interface. It will provide real-time status updates back to the client, ensuring a seamless user experience, and automatically clean up temporary files to optimize storage costs.

## 2. Architecture Context
This architecture is based on Go microservices communicating asynchronously via NATS. It involves:
- A user-facing Web Gateway that handles HTTP traffic, serves server-side rendered HTML using Templ and HTMX, and streams real-time updates using Server-Sent Events (SSE).
- A NATS event bus acting as the central nervous system for decoupled microservice communication.
- A Worker Engine configured with polymorphic handlers to process different media types (Images, PDFs) asynchronously.
- A PostgreSQL database acting as the persistent task repository, tracking the state of all processing jobs.
- An Ephemeral TTL Janitor to automatically prune expired files and database records to prevent unbounded storage growth.

## 3. Scope
### In-Scope
- **Web Gateway:** HTTP server in Go, rendering UI via Templ, HTMX, and Tailwind CSS. Implements SSE endpoints for real-time task status updates.
- **NATS Event Bus:** Integration for publishing and subscribing to task events (e.g., `job.created`, `job.completed`, `job.failed`).
- **Worker Engine:** A scalable worker service that consumes NATS messages and executes polymorphic handlers:
  - Image handler (e.g., resizing, format conversion).
  - PDF handler (e.g., text extraction, thumbnail generation).
- **PostgreSQL Task Repository:** Schema and data access layer for tracking job metadata, status (`pending`, `processing`, `completed`, `failed`), and results.
- **Ephemeral TTL Janitor:** A background scheduler/cron mechanism to periodically clean up expired temporary files and old task records.

### Out of Scope (Non-Goals)
- User authentication and authorization (deferred to a future change).
- Advanced media processing capabilities (e.g., video transcoding, AI-based image tagging).
- Cloud object storage integration (e.g., AWS S3) for permanent storage; this scope focuses on local/ephemeral storage for the baseline.
- Multi-region or highly available NATS/PostgreSQL cluster setups (infrastructure concerns).
- Billing or quotas.

## 4. Affected Areas and Implications
- **User Interface:** HTMX and SSE will introduce a stateful, connection-oriented pattern on the frontend for tracking ongoing jobs.
- **Data Layer:** New database tables for job tracking and file metadata.
- **Infrastructure:** Requires running NATS and PostgreSQL alongside the Go services.
- **Storage:** Local disk I/O will increase during media processing. The TTL Janitor must strictly enforce storage limits to avoid disk exhaustion.

## 5. Risks and Edge Cases
### Risks
- **SSE Connection Management:** High number of concurrent SSE connections could strain the Web Gateway.
- **Worker Starvation/Bottlenecks:** Large PDF or image files might block worker goroutines, delaying smaller jobs.
- **Orphaned Files:** If the system crashes during processing, files might not be properly tracked or cleaned up.

### Edge Cases
- Client disconnects while a job is processing (SSE connection drops). The system should continue processing and allow the user to view the result upon reconnecting.
- Unsupported file types or corrupted media uploads. The worker must fail gracefully, update the database to `failed` state, and notify the gateway via NATS.
- NATS unavailability: Gateway must queue or reject uploads gracefully if the event bus is down.

## 6. Trade-offs
- **Complexity vs. Scalability:** Introducing NATS and PostgreSQL adds operational complexity compared to a monolithic synchronous approach, but it is necessary for horizontal scaling of the Worker Engine.
- **Server-Side Rendering vs. SPA:** Using Templ and HTMX keeps the frontend logic simple and centralized on the server, trading off the rich client-side state management of an SPA for faster initial load and reduced client-side JavaScript.
- **Polling vs. SSE:** SSE provides immediate updates with lower overhead than polling, but requires keeping long-lived HTTP connections open.

## 7. Rollback Plan
Since this is a foundational change (the base architecture), rollback would consist of reverting the repository to the pre-implementation state and dropping the newly created PostgreSQL schema. If this is replacing an existing system, the old system should remain active until this engine is fully validated.

## 8. Success Criteria
- [ ] Users can upload an image or PDF via the web interface.
- [ ] The Web Gateway successfully enqueues the job via NATS and returns immediately.
- [ ] The Worker Engine picks up the job, processes it, and updates the state in PostgreSQL.
- [ ] The frontend receives real-time progress updates via SSE and displays the final result without a full page reload.
- [ ] The TTL Janitor successfully deletes files and records older than the configured time-to-live.