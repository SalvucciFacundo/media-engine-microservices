package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
	"github.com/nats-io/nats.go"
)

// Subject constants and helpers for NATS topic hierarchy.
const (
	SubjectPrefixJob = "jobs"

	EventCreated    = "created"
	EventProcessing = "processing"
	EventCompleted  = "completed"
	EventFailed     = "failed"

	DefaultQueueGroup = "worker-engine-group"
)

// SubjectJobCreated returns the subject for new job creation events.
func SubjectJobCreated() string {
	return fmt.Sprintf("%s.%s", SubjectPrefixJob, EventCreated)
}

// SubjectJobCompleted returns the subject for completed job events.
func SubjectJobCompleted() string {
	return fmt.Sprintf("%s.%s", SubjectPrefixJob, EventCompleted)
}

// SubjectJobFailed returns the subject for failed job events.
func SubjectJobFailed() string {
	return fmt.Sprintf("%s.%s", SubjectPrefixJob, EventFailed)
}

// SubjectJobStatus returns the subject for real-time status updates of a specific job.
func SubjectJobStatus(jobID string) string {
	return fmt.Sprintf("%s.status.%s", SubjectPrefixJob, jobID)
}

// DefaultWorkerQueueGroup returns the default queue group name for worker engines.
func DefaultWorkerQueueGroup() string {
	return DefaultQueueGroup
}

// EventBus implements ports.EventPublisher and ports.EventSubscriber using NATS.
type EventBus struct {
	conn *nats.Conn
}

// NewEventBus creates a new NATS EventBus adapter.
func NewEventBus(conn *nats.Conn) *EventBus {
	return &EventBus{conn: conn}
}

// Publish serializes the event to JSON and publishes it to the specified subject.
func (b *EventBus) Publish(ctx context.Context, subject string, event ports.Event) error {
	if subject == "" {
		return errors.New("nats: subject cannot be empty")
	}
	if b.conn == nil || b.conn.IsClosed() {
		return errors.New("nats: connection is closed or nil")
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("nats: failed to marshal event: %w", err)
	}

	if err := b.conn.Publish(subject, data); err != nil {
		return fmt.Errorf("nats: failed to publish message: %w", err)
	}

	return nil
}

// Subscribe listens to messages on a subject and dispatches them to the handler.
func (b *EventBus) Subscribe(ctx context.Context, subject string, handler ports.EventHandler) (ports.SubscriptionCloser, error) {
	if subject == "" {
		return nil, errors.New("nats: subject cannot be empty")
	}
	if handler == nil {
		return nil, errors.New("nats: handler cannot be nil")
	}
	if b.conn == nil || b.conn.IsClosed() {
		return nil, errors.New("nats: connection is closed or nil")
	}

	sub, err := b.conn.Subscribe(subject, func(msg *nats.Msg) {
		var evt ports.Event
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			// Malformed JSON is ignored or logged, avoid handler crash
			return
		}
		_ = handler(context.Background(), evt)
	})
	if err != nil {
		return nil, fmt.Errorf("nats: failed to subscribe: %w", err)
	}

	return sub, nil
}

// SubscribeQueue subscribes to a subject with a queue group for balanced delivery across workers.
func (b *EventBus) SubscribeQueue(ctx context.Context, subject, queueGroup string, handler ports.EventHandler) (ports.SubscriptionCloser, error) {
	if subject == "" {
		return nil, errors.New("nats: subject cannot be empty")
	}
	if queueGroup == "" {
		return nil, errors.New("nats: queueGroup cannot be empty")
	}
	if handler == nil {
		return nil, errors.New("nats: handler cannot be nil")
	}
	if b.conn == nil || b.conn.IsClosed() {
		return nil, errors.New("nats: connection is closed or nil")
	}

	sub, err := b.conn.QueueSubscribe(subject, queueGroup, func(msg *nats.Msg) {
		var evt ports.Event
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			return
		}
		_ = handler(context.Background(), evt)
	})
	if err != nil {
		return nil, fmt.Errorf("nats: failed to queue subscribe: %w", err)
	}

	return sub, nil
}
