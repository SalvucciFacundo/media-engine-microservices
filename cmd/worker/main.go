package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/localfs"
	natsadapter "github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/nats"
	pgadapter "github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/postgres"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/core"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/handlers/media"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
	"github.com/nats-io/nats.go"
)

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func main() {
	log.Println("Starting Media Engine Worker Service...")

	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/media_engine?sslmode=disable")
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	storageDir := getEnv("STORAGE_DIR", "./storage/uploads")
	queueGroup := getEnv("WORKER_QUEUE_GROUP", natsadapter.DefaultWorkerQueueGroup())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Initialize PostgreSQL Connection Pool & Repository
	jobRepo, err := pgadapter.NewJobRepository(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to initialize database repository: %v", err)
	}
	defer jobRepo.Close()
	log.Println("Connected to PostgreSQL successfully")

	// 2. Initialize NATS Connection
	nc, err := nats.Connect(natsURL, nats.MaxReconnects(10), nats.ReconnectWait(2*time.Second))
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()
	log.Println("Connected to NATS successfully")

	eventBus := natsadapter.NewEventBus(nc)

	// 3. Initialize File Store
	fileStore, err := localfs.NewFileStore(storageDir)
	if err != nil {
		log.Fatalf("Failed to initialize file store: %v", err)
	}
	log.Printf("File store initialized at: %s", storageDir)

	// 4. Initialize Polymorphic Media Processors
	imageProcessor := media.NewImageProcessor(fileStore)
	pdfProcessor := media.NewPDFProcessor(fileStore)
	processors := []ports.MediaProcessor{
		imageProcessor,
		pdfProcessor,
	}

	// 5. Initialize Worker Service
	workerService := core.NewWorkerService(jobRepo, eventBus, eventBus, processors, queueGroup)
	if err := workerService.Start(ctx); err != nil {
		log.Fatalf("Failed to start worker service: %v", err)
	}

	log.Printf("Worker service running, waiting for media processing jobs...")

	// 6. Graceful Shutdown on SIGINT / SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("Received termination signal %s, initiating graceful shutdown...", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	// Unsubscribe from NATS queue
	if err := workerService.Stop(); err != nil {
		log.Printf("Error stopping worker service: %v", err)
	}

	// Drain NATS connection
	if err := nc.Drain(); err != nil {
		log.Printf("Error draining NATS connection: %v", err)
	}

	<-shutdownCtx.Done()
	log.Println("Worker shutdown complete.")
}
