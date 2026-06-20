package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateTaskWithUpload(t *testing.T) {
	dataDir := t.TempDir()
	uploadDir := filepath.Join(dataDir, "uploads")
	store, err := NewStore(filepath.Join(dataDir, "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	app := newApp(store, uploadDir, http.NotFoundHandler())

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("title", "Upload a design note"); err != nil {
		t.Fatalf("write title: %v", err)
	}
	file, err := writer.CreateFormFile("files", "../screen shot.txt")
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := file.Write([]byte("image placeholder")); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/tasks", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", response.Code, response.Body.String())
	}

	var task Task
	if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(task.Attachments) != 1 {
		t.Fatalf("expected one attachment, got %d", len(task.Attachments))
	}
	attachment := task.Attachments[0]
	if attachment.Name != "screen-shot.txt" {
		t.Fatalf("expected sanitized name, got %q", attachment.Name)
	}
	if strings.Contains(attachment.URL, "..") {
		t.Fatalf("attachment URL should not contain traversal: %q", attachment.URL)
	}

	uploadedPath := filepath.Join(uploadDir, strings.TrimPrefix(attachment.URL, "/uploads/"))
	bytes, err := os.ReadFile(uploadedPath)
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(bytes) != "image placeholder" {
		t.Fatalf("unexpected uploaded file contents: %q", string(bytes))
	}
}

func TestEventHubTracksConnectedDevices(t *testing.T) {
	hub := newEventHub()
	request := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	request.RemoteAddr = "192.168.1.50:49152"
	request.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 Chrome/120 Safari/537.36")

	first, err := hub.subscribe(request)
	if err != nil {
		t.Fatalf("subscribe first client: %v", err)
	}
	second, err := hub.subscribe(request)
	if err != nil {
		t.Fatalf("subscribe second client: %v", err)
	}

	snapshot := hub.Devices()
	if len(snapshot.Devices) != 1 {
		t.Fatalf("expected one grouped device, got %d", len(snapshot.Devices))
	}
	device := snapshot.Devices[0]
	if device.Address != "192.168.1.50" {
		t.Fatalf("expected device address, got %q", device.Address)
	}
	if device.Connections != 2 {
		t.Fatalf("expected two connections, got %d", device.Connections)
	}
	if device.Name != "Android Chrome" {
		t.Fatalf("expected Android Chrome, got %q", device.Name)
	}

	hub.unsubscribe(first.ID)
	snapshot = hub.Devices()
	if len(snapshot.Devices) != 1 || snapshot.Devices[0].Connections != 1 {
		t.Fatalf("expected one remaining connection, got %+v", snapshot.Devices)
	}

	hub.unsubscribe(second.ID)
	snapshot = hub.Devices()
	if len(snapshot.Devices) != 0 {
		t.Fatalf("expected no devices after disconnect, got %+v", snapshot.Devices)
	}
}

func TestEventHubTracksClientHealth(t *testing.T) {
	hub := newEventHub()
	request := httptest.NewRequest(http.MethodGet, "/api/events?client=phone-1", nil)
	request.RemoteAddr = "192.168.1.51:49152"
	request.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 Chrome/120 Safari/537.36")

	client, err := hub.subscribe(request)
	if err != nil {
		t.Fatalf("subscribe client: %v", err)
	}
	defer hub.unsubscribe(client.ID)

	online := true
	battery := 71
	charging := true
	downlink := 18.7
	rtt := 40
	statusRequest := httptest.NewRequest(http.MethodPost, "/api/client-status", nil)
	statusRequest.RemoteAddr = "192.168.1.51:50100"
	statusRequest.Header.Set("User-Agent", request.UserAgent())

	updated := hub.updateHealth("phone-1", statusRequest, DeviceHealth{
		Online:         &online,
		BatteryPercent: &battery,
		Charging:       &charging,
		EffectiveType:  "4g",
		DownlinkMbps:   &downlink,
		RTTMs:          &rtt,
	})
	if !updated {
		t.Fatal("expected health update to match connected client")
	}

	snapshot := hub.Devices()
	if len(snapshot.Devices) != 1 {
		t.Fatalf("expected one device, got %d", len(snapshot.Devices))
	}
	health := snapshot.Devices[0].Health
	if health.Online == nil || !*health.Online {
		t.Fatalf("expected online health, got %+v", health)
	}
	if health.BatteryPercent == nil || *health.BatteryPercent != 71 {
		t.Fatalf("expected battery percent, got %+v", health)
	}
	if health.Charging == nil || !*health.Charging {
		t.Fatalf("expected charging status, got %+v", health)
	}
	if health.DownlinkMbps == nil || *health.DownlinkMbps != 18.7 {
		t.Fatalf("expected downlink, got %+v", health)
	}
	if health.RTTMs == nil || *health.RTTMs != 40 {
		t.Fatalf("expected rtt, got %+v", health)
	}
}
