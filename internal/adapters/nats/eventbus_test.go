package nats_test

import (
	"context"
	"sync"
	"testing"
	"time"

	natsadapter "github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/nats"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func runEmbeddedNatsServer(t *testing.T) (*natsserver.Server, string) {
	t.Helper()
	opts := &natsserver.Options{
		Port: -1, // Random available port
	}
	ns, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("failed to create embedded nats server: %v", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats server failed to start in time")
	}
	t.Cleanup(func() {
		ns.Shutdown()
	})
	return ns, ns.ClientURL()
}

func TestSubjectHelpers(t *testing.T) {
	if got := natsadapter.SubjectJobCreated(); got != "jobs.created" {
		t.Errorf("expected jobs.created, got %s", got)
	}
	if got := natsadapter.SubjectJobCompleted(); got != "jobs.completed" {
		t.Errorf("expected jobs.completed, got %s", got)
	}
	if got := natsadapter.SubjectJobFailed(); got != "jobs.failed" {
		t.Errorf("expected jobs.failed, got %s", got)
	}
	if got := natsadapter.SubjectJobStatus("job-123"); got != "jobs.status.job-123" {
		t.Errorf("expected jobs.status.job-123, got %s", got)
	}
	if got := natsadapter.DefaultWorkerQueueGroup(); got != "worker-engine-group" {
		t.Errorf("expected worker-engine-group, got %s", got)
	}
}

func TestEventBus_PublishAndSubscribe(t *testing.T) {
	_, url := runEmbeddedNatsServer(t)

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("failed to connect to nats: %v", err)
	}
	defer nc.Close()

	bus := natsadapter.NewEventBus(nc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	receivedCh := make(chan ports.Event, 1)

	sub, err := bus.Subscribe(ctx, "jobs.test.subscribe", func(ctx context.Context, evt ports.Event) error {
		receivedCh <- evt
		return nil
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	now := time.Now().UTC().Truncate(time.Millisecond)
	eventToSend := ports.Event{
		EventID:   "evt-001",
		JobID:     "job-001",
		EventType: "created",
		Timestamp: now,
		Payload: map[string]any{
			"media_type": "image/png",
			"filename":   "test.png",
		},
	}

	err = bus.Publish(ctx, "jobs.test.subscribe", eventToSend)
	if err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	select {
	case received := <-receivedCh:
		if received.EventID != eventToSend.EventID {
			t.Errorf("expected event_id %s, got %s", eventToSend.EventID, received.EventID)
		}
		if received.JobID != eventToSend.JobID {
			t.Errorf("expected job_id %s, got %s", eventToSend.JobID, received.JobID)
		}
		if received.EventType != eventToSend.EventType {
			t.Errorf("expected event_type %s, got %s", eventToSend.EventType, received.EventType)
		}
		if received.Payload["media_type"] != "image/png" {
			t.Errorf("expected media_type image/png, got %v", received.Payload["media_type"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventBus_SubscribeQueue_LoadBalancing(t *testing.T) {
	_, url := runEmbeddedNatsServer(t)

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("failed to connect to nats: %v", err)
	}
	defer nc.Close()

	bus := natsadapter.NewEventBus(nc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var count1, count2 int
	var mu sync.Mutex

	sub1, err := bus.SubscribeQueue(ctx, "jobs.queue.test", "test-workers", func(ctx context.Context, evt ports.Event) error {
		mu.Lock()
		count1++
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("sub1 failed: %v", err)
	}
	defer sub1.Unsubscribe()

	sub2, err := bus.SubscribeQueue(ctx, "jobs.queue.test", "test-workers", func(ctx context.Context, evt ports.Event) error {
		mu.Lock()
		count2++
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("sub2 failed: %v", err)
	}
	defer sub2.Unsubscribe()

	// Publish 10 events
	for i := 0; i < 10; i++ {
		evt := ports.Event{
			EventID:   "evt-q",
			JobID:     "job-q",
			EventType: "created",
			Timestamp: time.Now().UTC(),
		}
		if err := bus.Publish(ctx, "jobs.queue.test", evt); err != nil {
			t.Fatalf("publish failed: %v", err)
		}
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	total := count1 + count2
	mu.Unlock()

	if total != 10 {
		t.Errorf("expected total 10 events handled across queue group, got %d (worker1: %d, worker2: %d)", total, count1, count2)
	}
}

func TestEventBus_ValidationAndErrors(t *testing.T) {
	_, url := runEmbeddedNatsServer(t)

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("failed to connect to nats: %v", err)
	}
	defer nc.Close()

	bus := natsadapter.NewEventBus(nc)
	ctx := context.Background()

	// Empty subject
	err = bus.Publish(ctx, "", ports.Event{EventID: "1"})
	if err == nil {
		t.Error("expected error when publishing to empty subject")
	}

	// Nil connection or closed connection
	nc.Close()
	err = bus.Publish(ctx, "jobs.test", ports.Event{EventID: "1"})
	if err == nil {
		t.Error("expected error publishing to closed connection")
	}
}

func TestEventBus_MalformedJSONHandling(t *testing.T) {
	_, url := runEmbeddedNatsServer(t)

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("failed to connect to nats: %v", err)
	}
	defer nc.Close()

	bus := natsadapter.NewEventBus(nc)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	called := false
	sub, err := bus.Subscribe(ctx, "jobs.malformed", func(ctx context.Context, evt ports.Event) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	// Publish raw invalid JSON
	if err := nc.Publish("jobs.malformed", []byte("{not-valid-json")); err != nil {
		t.Fatalf("failed to publish raw bytes: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if called {
		t.Error("handler should not be called for malformed JSON")
	}
}
