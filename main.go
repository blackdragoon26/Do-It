package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

func main() {
	addr := env("DOIT_ADDR", ":8080")
	dataDir := env("DOIT_DATA_DIR", "data")
	statePath := filepath.Join(dataDir, "state.json")
	uploadDir := filepath.Join(dataDir, "uploads")

	store, err := NewStore(statePath)
	if err != nil {
		log.Fatalf("load store: %v", err)
	}

	staticRoot, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("load static files: %v", err)
	}

	app := newApp(store, uploadDir, http.FileServer(http.FS(staticRoot)))
	server := &http.Server{
		Addr:              addr,
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Do-It listening on http://localhost:%s", listenPort(addr))
	for _, url := range localNetworkURLs(listenPort(addr)) {
		log.Printf("LAN device URL: %s", url)
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Fatalf("shutdown: %v", err)
		}
		if err := <-serverErr; err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
