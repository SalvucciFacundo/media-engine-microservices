package core

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
	"github.com/google/uuid"
)

// WorkerService coordinates background media processing from the event bus.
type WorkerService struct {
	repo         ports.JobRepository
	publisher    ports.EventPublisher
	subscriber   ports.EventSubscriber
	processors   []ports.MediaProcessor
	queueGroup   string
	subscription ports.SubscriptionCloser
	mu           sync.Mutex
}

// NewWorkerService creates a new WorkerService instance.
func NewWorkerService(
	repo ports.JobRepository,
	publisher ports.EventPublisher,
	subscriber ports.EventSubscriber,
	processors []ports.MediaProcessor,
	queueGroup string,
) *WorkerService {
	if queueGroup == "" {
		queueGroup = "worker-engine-group"
	}
	return &WorkerService{
		repo:       repo,
		publisher:  publisher,
		subscriber: subscriber,
		processors: processors,
		queueGroup: queueGroup,
	}
}

// Start subscribes to the `jobs.created` NATS queue group and processes incoming jobs.
func (w *WorkerService) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	sub, err := w.subscriber.SubscribeQueue(ctx, "jobs.created", w.queueGroup, w.HandleJobCreated)
	if err != nil {
		return fmt.Errorf("worker: failed to subscribe to queue: %w", err)
	}

	w.subscription = sub
	log.Printf("worker: started and subscribed to jobs.created [queueGroup: %s]", w.queueGroup)
	return nil
}

// Stop gracefully unsubscribes from the event bus.
func (w *WorkerService) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.subscription != nil {
		if err := w.subscription.Unsubscribe(); err != nil {
			return fmt.Errorf("worker: error during unsubscribe: %w", err)
		}
		w.subscription = nil
	}
	return nil
}

// HandleJobCreated processes a single `jobs.created` event.
func (w *WorkerService) HandleJobCreated(ctx context.Context, event ports.Event) (err error) {
	jobID := event.JobID
	if jobID == "" {
		if val, ok := event.Payload["job_id"].(string); ok {
			jobID = val
		}
	}

	if jobID == "" {
		return fmt.Errorf("worker: event missing job_id")
	}

	// 1. Fetch Job from Repository
	job, err := w.repo.GetJobByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("worker: failed to fetch job %s: %w", jobID, err)
	}

	// 2. Transition state to 'processing'
	if err := w.repo.UpdateJobStatus(ctx, jobID, domain.StatusProcessing, nil); err != nil {
		return fmt.Errorf("worker: failed to update job status to processing: %w", err)
	}

	// Publish processing status event
	_ = w.publishStatusEvent(ctx, jobID, "processing", map[string]any{
		"progress":   10,
		"message":    "Processing started",
		"media_type": job.MediaType,
	})

	// 3. Find matching polymorphic media processor
	var matchedProcessor ports.MediaProcessor
	for _, p := range w.processors {
		if p.CanProcess(job.MediaType) {
			matchedProcessor = p
			break
		}
	}

	if matchedProcessor == nil {
		errStr := domain.ErrUnsupportedMediaType.Error()
		_ = w.repo.UpdateJobStatus(ctx, jobID, domain.StatusFailed, &errStr)
		_ = w.publishFailureEvent(ctx, jobID, errStr)
		return domain.ErrUnsupportedMediaType
	}

	// 4. Execute Processor with panic recovery
	defer func() {
		if r := recover(); r != nil {
			panicErr := fmt.Sprintf("panic during media processing: %v", r)
			_ = w.repo.UpdateJobStatus(ctx, jobID, domain.StatusFailed, &panicErr)
			_ = w.publishFailureEvent(ctx, jobID, panicErr)
			err = fmt.Errorf("%s", panicErr)
		}
	}()

	_ = w.publishStatusEvent(ctx, jobID, "processing", map[string]any{
		"progress": 50,
		"message":  "Transforming media and generating artifacts",
	})

	artifacts, procErr := matchedProcessor.Process(ctx, job)
	if procErr != nil {
		errStr := procErr.Error()
		_ = w.repo.UpdateJobStatus(ctx, jobID, domain.StatusFailed, &errStr)
		_ = w.publishFailureEvent(ctx, jobID, errStr)
		return procErr
	}

	// 5. Persist generated artifacts to database
	for _, art := range artifacts {
		if err := w.repo.AddArtifact(ctx, art); err != nil {
			log.Printf("worker: warning, failed to persist artifact %s: %v", art.ID, err)
		}
	}

	// 6. Transition state to 'completed'
	if err := w.repo.UpdateJobStatus(ctx, jobID, domain.StatusCompleted, nil); err != nil {
		return fmt.Errorf("worker: failed to update job status to completed: %w", err)
	}

	// 7. Publish completion event
	_ = w.publishSuccessEvent(ctx, jobID, artifacts)

	return nil
}

func (w *WorkerService) publishStatusEvent(ctx context.Context, jobID, eventType string, payload map[string]any) error {
	evt := ports.Event{
		EventID:   uuid.New().String(),
		JobID:     jobID,
		EventType: eventType,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	subject := fmt.Sprintf("jobs.status.%s", jobID)
	return w.publisher.Publish(ctx, subject, evt)
}

func (w *WorkerService) publishFailureEvent(ctx context.Context, jobID, errorMsg string) error {
	payload := map[string]any{
		"error":   errorMsg,
		"message": "Processing failed",
	}
	_ = w.publishStatusEvent(ctx, jobID, "failed", payload)

	evt := ports.Event{
		EventID:   uuid.New().String(),
		JobID:     jobID,
		EventType: "failed",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	return w.publisher.Publish(ctx, "jobs.failed", evt)
}

func (w *WorkerService) publishSuccessEvent(ctx context.Context, jobID string, artifacts []domain.Artifact) error {
	payload := map[string]any{
		"message":         "Processing completed successfully",
		"artifacts_count": len(artifacts),
		"artifacts":       artifacts,
	}
	_ = w.publishStatusEvent(ctx, jobID, "completed", payload)

	evt := ports.Event{
		EventID:   uuid.New().String(),
		JobID:     jobID,
		EventType: "completed",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	return w.publisher.Publish(ctx, "jobs.completed", evt)
}
