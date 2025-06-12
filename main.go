package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/steveoni/microvm/api"
	"github.com/steveoni/microvm/db"
	"github.com/steveoni/microvm/jobs"
)

// In main.go
func main() {
    // signal.Ignore(syscall.SIGTSTP)
    if err := db.InitDB("jobs.db"); err != nil {
        log.Fatal("DB init failed:", err)
    }

    if err := jobs.InitClient("localhost:6379"); err != nil {
        log.Fatal("Redis failed:", err)
    }

    // Create a context with cancellation for graceful shutdown
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Setup signal handling to catch Ctrl+C
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    
    // Use a WaitGroup to track running goroutines
    var wg sync.WaitGroup
    wg.Add(1)

    // Start worker in goroutine
    go func() {
        defer wg.Done()
        s := jobs.NewServer("localhost:6379")
        
        // Use a channel to signal when Run returns
        done := make(chan struct{})
        
        // Handle cancellation in another goroutine
        go func() {
            select {
            case <-ctx.Done():
                log.Println("Worker shutting down...")
                s.Shutdown() // Properly shutdown the worker server
            case <-done:
                return
            }
        }()
        
        if err := s.Run(jobs.Handler()); err != nil {
            if err != asynq.ErrServerClosed {
                log.Printf("Worker error: %v", err)
            }
        }
        close(done)
    }()

    // Create HTTP server with graceful shutdown
    server := &http.Server{
        Addr:    ":8080",
        Handler: api.NewRouter(),
    }

    // Start the server in a goroutine
    go func() {
        log.Println("API running on :8080")
        if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("HTTP server error: %v", err)
        }
    }()

    // Wait for shutdown signal
    <-sigCh
    log.Println("Shutting down gracefully...")
    
    // Stop accepting new HTTP requests
    shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer shutdownCancel()
    if err := server.Shutdown(shutdownCtx); err != nil {
        log.Printf("HTTP server shutdown error: %v", err)
    }
    
    // Signal all goroutines to stop
    cancel()
    
    // Wait for goroutines to finish
    wg.Wait()
    log.Println("All services stopped. Goodbye!")
}