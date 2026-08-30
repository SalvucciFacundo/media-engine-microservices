package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/localfs"
	natsadapter "github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/nats"
	pgadapter "github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/postgres"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/core"
	gatewayHttp "github.com/SalvucciFacundo/media-engine-microservices/internal/handlers/http"
	"github.com/nats-io/nats.go"
)

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func main() {
	log.Println("Starting Media Engine Web Gateway...")

	port := getEnv("PORT", "8080")
	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/media_engine?sslmode=disable")
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	storageDir := getEnv("STORAGE_DIR", "./storage/uploads")
	ttlStr := getEnv("DEFAULT_TTL", "1h")
	janitorIntervalStr := getEnv("JANITOR_INTERVAL", "5m")

	defaultTTL, err := time.ParseDuration(ttlStr)
	if err != nil {
		defaultTTL = 1 * time.Hour
	}

	janitorInterval, err := time.ParseDuration(janitorIntervalStr)
	if err != nil {
		janitorInterval = 5 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. PostgreSQL Connection Pool & Repository
	jobRepo, err := pgadapter.NewJobRepository(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL repository: %v", err)
	}
	defer jobRepo.Close()
	log.Println("Connected to PostgreSQL successfully")

	// 2. NATS Connection & Event Bus
	nc, err := nats.Connect(natsURL, nats.MaxReconnects(10), nats.ReconnectWait(2*time.Second))
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()
	log.Println("Connected to NATS successfully")

	eventBus := natsadapter.NewEventBus(nc)

	// 3. Local File Store Adapter
	fileStore, err := localfs.NewFileStore(storageDir)
	if err != nil {
		log.Fatalf("Failed to initialize file store: %v", err)
	}
	log.Printf("File store initialized at: %s", storageDir)

	// 4. Upload Core Service
	uploadService := core.NewUploadService(jobRepo, fileStore, eventBus, defaultTTL)

	// 5. Embedded Janitor Service
	janitorService := core.NewJanitorService(jobRepo, fileStore)
	if janitorInterval > 0 {
		janitorService.Start(ctx, janitorInterval)
		log.Printf("Janitor background cleanup active (interval: %s)", janitorInterval)
	}

	// 6. HTTP Gateway Handler & Server
	gatewayServer := gatewayHttp.NewServer(jobRepo, fileStore, eventBus, uploadService, janitorService)
	httpServer := &http.Server{
		Addr:         ":" + port,
		Handler:      gatewayServer,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("Web Gateway HTTP server listening on http://0.0.0.0:%s", port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server failure: %v", err)
		}
	}()

	// 7. Graceful Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("Received termination signal %s, initiating graceful shutdown...", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	// Stop background Janitor
	janitorService.Stop()

	// Stop HTTP server
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error shutting down HTTP server: %v", err)
	}

	// Drain NATS
	if err := nc.Drain(); err != nil {
		log.Printf("Error draining NATS connection: %v", err)
	}

	log.Println("Gateway shutdown complete.")
}
