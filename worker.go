package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Background worker that simulates processing jobs
// Run with: go run worker.go
func main() {
	fmt.Println("Background worker starting...")

	workerID := os.Getenv("WORKER_ID")
	if workerID == "" {
		workerID = "default"
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	jobCount := 0

	fmt.Printf("[Worker %s] Ready to process jobs\n", workerID)

	for {
		select {
		case <-ticker.C:
			jobCount++
			fmt.Printf("[Worker %s] Processed job #%d at %s\n",
				workerID, jobCount, time.Now().Format(time.RFC3339))
		case sig := <-sigChan:
			fmt.Printf("[Worker %s] Received signal %v, shutting down gracefully...\n", workerID, sig)
			fmt.Printf("[Worker %s] Total jobs processed: %d\n", workerID, jobCount)
			return
		}
	}
}
