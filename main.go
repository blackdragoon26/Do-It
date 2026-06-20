package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
