package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/localfs"
	pgadapter "github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/postgres"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/core"
)

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func main() {
	log.Println("Starting Standalone Media Engine TTL Janitor...")

	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/media_engine?sslmode=disable")
	storageDir := getEnv("STORAGE_DIR", "./storage/uploads")
	intervalStr := getEnv("CLEANUP_INTERVAL", "5m")

	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		interval = 5 * time.Minute
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

	// 2. Local File Store Adapter
	fileStore, err := localfs.NewFileStore(storageDir)
	if err != nil {
		log.Fatalf("Failed to initialize file store: %v", err)
	}
	log.Printf("File store initialized at: %s", storageDir)

	// 3. Janitor Service
	janitor := core.NewJanitorService(jobRepo, fileStore)
	janitor.Start(ctx, interval)
	log.Printf("Janitor service running, executing expiration sweeps every %s...", interval)

	// 4. Graceful Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("Received termination signal %s, initiating graceful shutdown...", sig)

	janitor.Stop()
	log.Println("Janitor standalone service stopped cleanly.")
}
