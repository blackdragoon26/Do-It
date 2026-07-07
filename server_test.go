package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	addCSRF(request)
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

func TestCreateTaskRejectsExecutableUploadExtension(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(filepath.Join(dataDir, "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	app := newApp(store, filepath.Join(dataDir, "uploads"), http.NotFoundHandler())

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("title", "Upload a script"); err != nil {
		t.Fatalf("write title: %v", err)
	}
	file, err := writer.CreateFormFile("files", "payload.html")
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := file.Write([]byte("<script>alert(1)</script>")); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/tasks", &body)
	addCSRF(request)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", response.Code, response.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "uploads"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read upload dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected rejected upload to avoid stored files, got %d", len(entries))
	}
}

func TestCreateTaskRejectsMissingCSRFToken(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(filepath.Join(dataDir, "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	app := newApp(store, filepath.Join(dataDir, "uploads"), http.NotFoundHandler())

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("title", "Missing token"); err != nil {
		t.Fatalf("write title: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/tasks", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestUnsupportedAPIMethodRequiresCSRFBeforeMethodCheck(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(filepath.Join(dataDir, "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	app := newApp(store, filepath.Join(dataDir, "uploads"), http.NotFoundHandler())

	request := httptest.NewRequest(http.MethodPut, "/api/tasks", nil)
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPut, "/api/tasks", nil)
	addCSRF(request)
	response = httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCSRFCookieIsOnlyIssuedForSafeMethods(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	app := newApp(store, t.TempDir(), http.NotFoundHandler())

	postRequest := httptest.NewRequest(http.MethodPost, "/", nil)
	postResponse := httptest.NewRecorder()
	app.routes().ServeHTTP(postResponse, postRequest)
	if cookieByName(postResponse.Result().Cookies(), csrfCookieName) != nil {
		t.Fatal("expected unsafe non-API request not to receive a CSRF cookie")
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	getResponse := httptest.NewRecorder()
	app.routes().ServeHTTP(getResponse, getRequest)
	if cookieByName(getResponse.Result().Cookies(), csrfCookieName) == nil {
		t.Fatal("expected safe request to receive a CSRF cookie")
	}
}

func TestCSRFCookieUsesForwardedHTTPSForSecureAttribute(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	app := newApp(store, t.TempDir(), http.NotFoundHandler())

	request := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)

	cookie := cookieByName(response.Result().Cookies(), csrfCookieName)
	if cookie == nil {
		t.Fatal("expected CSRF cookie")
	}
	if !cookie.Secure {
		t.Fatal("expected forwarded HTTPS request to set Secure cookie")
	}
}

func TestCreateTaskCleansEarlierUploadsWhenLaterFileFails(t *testing.T) {
	dataDir := t.TempDir()
	uploadDir := filepath.Join(dataDir, "uploads")
	store, err := NewStore(filepath.Join(dataDir, "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	app := newApp(store, uploadDir, http.NotFoundHandler())

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("title", "Mixed upload"); err != nil {
		t.Fatalf("write title: %v", err)
	}
	first, err := writer.CreateFormFile("files", "safe.txt")
	if err != nil {
		t.Fatalf("create first file: %v", err)
	}
	if _, err := first.Write([]byte("safe")); err != nil {
		t.Fatalf("write first file: %v", err)
	}
	second, err := writer.CreateFormFile("files", "unsafe.sh")
	if err != nil {
		t.Fatalf("create second file: %v", err)
	}
	if _, err := second.Write([]byte("echo unsafe")); err != nil {
		t.Fatalf("write second file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/tasks", &body)
	addCSRF(request)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", response.Code, response.Body.String())
	}
	entries, err := os.ReadDir(uploadDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read upload dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected failed multi-file upload to remove earlier files, got %d", len(entries))
	}
}

func TestAllowedUploadTypeReportsUnknownExtension(t *testing.T) {
	_, err := allowedUploadType("script.sh")
	if err == nil {
		t.Fatal("expected .sh to be rejected")
	}
	if !strings.Contains(err.Error(), ".sh uploads are not allowed") {
		t.Fatalf("expected unknown extension message, got %q", err.Error())
	}
}

func TestDeleteTaskRemovesUploadedFiles(t *testing.T) {
	dataDir := t.TempDir()
	uploadDir := filepath.Join(dataDir, "uploads")
	store, err := NewStore(filepath.Join(dataDir, "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	app := newApp(store, uploadDir, http.NotFoundHandler())
	task := createUploadedTask(t, app, "notes.txt", "temporary notes")

	uploadedPath := filepath.Join(uploadDir, strings.TrimPrefix(task.Attachments[0].URL, "/uploads/"))
	if _, err := os.Stat(uploadedPath); err != nil {
		t.Fatalf("expected uploaded file before delete: %v", err)
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/tasks/"+task.ID, nil)
	addCSRF(request)
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d: %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(uploadedPath); !os.IsNotExist(err) {
		t.Fatalf("expected uploaded file to be removed, got %v", err)
	}
}

func TestMutationRateLimit(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(filepath.Join(dataDir, "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	app := newApp(store, filepath.Join(dataDir, "uploads"), http.NotFoundHandler())
	app.limiter = newRateLimiter(time.Minute, 1)

	for i, want := range []int{http.StatusCreated, http.StatusTooManyRequests} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("title", "Limited task"); err != nil {
			t.Fatalf("write title: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close multipart writer: %v", err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/tasks", &body)
		addCSRF(request)
		request.RemoteAddr = "203.0.113.10:4000"
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		app.routes().ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("request %d: expected status %d, got %d: %s", i+1, want, response.Code, response.Body.String())
		}
	}
}

func TestMutationRateLimitPrunesExpiredClients(t *testing.T) {
	limiter := newRateLimiter(time.Minute, 1)
	limiter.clients["expired"] = rateWindow{
		start: time.Now().Add(-2 * time.Minute),
		count: 1,
	}

	if !limiter.Allow("fresh") {
		t.Fatal("expected fresh client to be allowed")
	}
	if _, ok := limiter.clients["expired"]; ok {
		t.Fatal("expected expired client to be pruned")
	}
}

func TestMutationRateLimitCapsClientMap(t *testing.T) {
	limiter := newRateLimiter(time.Minute, 1)
	now := time.Now()
	for i := 0; i < maxRateLimitClients; i++ {
		limiter.clients[fmt.Sprintf("client-%d", i)] = rateWindow{start: now, count: 1}
	}

	if limiter.Allow("overflow") {
		t.Fatal("expected new client to be rejected when limiter map is full")
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

func TestVirtualInterfaceNamesAreSkippedForLANURLs(t *testing.T) {
	for _, name := range []string{"docker0", "br-1a2b3c", "veth123", "virbr0"} {
		if !isLikelyVirtualInterface(name) {
			t.Fatalf("expected %q to be treated as virtual", name)
		}
	}
	for _, name := range []string{"en0", "wlan0", "eth0"} {
		if isLikelyVirtualInterface(name) {
			t.Fatalf("expected %q to be treated as a physical candidate", name)
		}
	}
}

func createUploadedTask(t *testing.T, app *app, name, contents string) Task {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("title", "Upload a file"); err != nil {
		t.Fatalf("write title: %v", err)
	}
	file, err := writer.CreateFormFile("files", name)
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := file.Write([]byte(contents)); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/tasks", &body)
	addCSRF(request)
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
	return task
}

func addCSRF(request *http.Request) {
	const token = "test-csrf-token"
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	request.Header.Set(csrfHeaderName, token)
}

func cookieByName(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
