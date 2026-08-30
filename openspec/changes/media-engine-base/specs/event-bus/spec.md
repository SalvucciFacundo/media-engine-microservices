# Event Bus Specification

## Purpose
The Event Bus provides decoupled, asynchronous communication between the Web Gateway, Worker Engine, and any auxiliary services using NATS messaging.

## Requirements

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
