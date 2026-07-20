package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/web"
)

var (
	version = "dev" // set by ldflags during build
)

func main() {
	addr := flag.String("addr", "localhost:8080", "HTTP server address")
	flag.Parse()

	server := web.NewServer()
	server.SetVersion(version)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(*addr)
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-signalCtx.Done():
		// Restore the default signal behavior before graceful shutdown so a
		// second SIGINT/SIGTERM can force an immediate exit if shutdown stalls.
		stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server stopped with error: %v", err)
		}
	}
}
