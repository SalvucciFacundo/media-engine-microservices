package ports

import (
	"context"
	"time"
)

// Event represents a structured event published or received across services.
type Event struct {
	EventID   string         `json:"event_id"`
	JobID     string         `json:"job_id"`
	EventType string         `json:"event_type"` // created, processing, completed, failed
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload"`
}

// EventHandler processes an incoming event.
type EventHandler func(ctx context.Context, event Event) error

// EventPublisher defines the contract for publishing events to the bus.
type EventPublisher interface {
	Publish(ctx context.Context, subject string, event Event) error
}

// EventSubscriber defines the contract for subscribing to events on the bus.
type EventSubscriber interface {
	Subscribe(ctx context.Context, subject string, handler EventHandler) (SubscriptionCloser, error)
	SubscribeQueue(ctx context.Context, subject, queueGroup string, handler EventHandler) (SubscriptionCloser, error)
}

// SubscriptionCloser allows unsubscribing or closing an active subscription.
type SubscriptionCloser interface {
	Unsubscribe() error
}
