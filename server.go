package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxRequestBytes       = 64 << 20
	maxUploadBytes        = 32 << 20
	maxFilesPerTask       = 8
	maxMutationsPerMinute = 60
	maxRateLimitClients   = 4096
	csrfCookieName        = "doit_csrf"
	csrfHeaderName        = "X-CSRF-Token"
)

type app struct {
	store     *Store
	hub       *eventHub
	uploadDir string
	static    http.Handler
	limiter   *rateLimiter
}

type eventHub struct {
	mu      sync.Mutex
	clients map[string]*clientSession
}

type clientSession struct {
	ID          string
	Key         string
	BrowserID   string
	Ch          chan serverEvent
	Address     string
	Name        string
	UserAgent   string
	Health      DeviceHealth
	ConnectedAt time.Time
	LastSeen    time.Time
}

type serverEvent struct {
	Name string
	Data []byte
}

type rateLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	limit   int
	clients map[string]rateWindow
}

type rateWindow struct {
	start time.Time
	count int
}

type ConnectedDevice struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Address     string       `json:"address"`
	UserAgent   string       `json:"userAgent,omitempty"`
	Connections int          `json:"connections"`
	Health      DeviceHealth `json:"health"`
	ConnectedAt time.Time    `json:"connectedAt"`
	LastSeen    time.Time    `json:"lastSeen"`
}

type DeviceSnapshot struct {
	Devices   []ConnectedDevice `json:"devices"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

type DeviceHealth struct {
	Online         *bool      `json:"online,omitempty"`
	BatteryPercent *int       `json:"batteryPercent,omitempty"`
	Charging       *bool      `json:"charging,omitempty"`
	EffectiveType  string     `json:"effectiveType,omitempty"`
	DownlinkMbps   *float64   `json:"downlinkMbps,omitempty"`
	RTTMs          *int       `json:"rttMs,omitempty"`
	UpdatedAt      *time.Time `json:"updatedAt,omitempty"`
}

func newApp(store *Store, uploadDir string, static http.Handler) *app {
	return &app{
		store:     store,
		hub:       newEventHub(),
		uploadDir: uploadDir,
		static:    static,
		limiter:   newRateLimiter(time.Minute, maxMutationsPerMinute),
	}
}

func newEventHub() *eventHub {
	return &eventHub{clients: make(map[string]*clientSession)}
}

func newRateLimiter(window time.Duration, limit int) *rateLimiter {
	return &rateLimiter{
		window:  window,
		limit:   limit,
		clients: make(map[string]rateWindow),
	}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks", a.handleTasks)
	mux.HandleFunc("/api/tasks/", a.handleTaskByID)
	mux.HandleFunc("/api/events", a.handleEvents)
	mux.HandleFunc("/api/devices", a.handleDevices)
	mux.HandleFunc("/api/client-status", a.handleClientStatus)
	mux.HandleFunc("/api/network", a.handleNetwork)
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(a.uploadDir))))
	mux.Handle("/", a.static)
	return withSecurityHeaders(withCSRFCookie(withCSRFProtection(a.withMutationRateLimit(mux))))
}

func (a *app) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/tasks" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.store.Snapshot())
	case http.MethodPost:
		a.createTask(w, r)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (a *app) createTask(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		httpError(w, http.StatusBadRequest, "could not read form")
		return
	}

	if strings.TrimSpace(r.FormValue("title")) == "" {
		httpError(w, http.StatusBadRequest, "title is required")
		return
	}

	attachments, err := a.saveUploadedFiles(r)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	snapshot, task, err := a.store.AddTask(
		r.FormValue("title"),
		r.FormValue("notes"),
		r.FormValue("parentId"),
		attachments,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	a.hub.broadcast(snapshot)
	writeJSON(w, http.StatusCreated, task)
}

func (a *app) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/tasks/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var input struct {
			Title    *string `json:"title"`
			Notes    *string `json:"notes"`
			ParentID *string `json:"parentId"`
			Done     *bool   `json:"done"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		snapshot, task, err := a.store.PatchTask(id, TaskPatch{
			Title:    input.Title,
			Notes:    input.Notes,
			ParentID: input.ParentID,
			Done:     input.Done,
		})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		a.hub.broadcast(snapshot)
		writeJSON(w, http.StatusOK, task)
	case http.MethodDelete:
		task, err := a.store.Task(id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		snapshot, err := a.store.DeleteTask(id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		a.removeUploadedAttachments(task.Attachments)
		a.hub.broadcast(snapshot)
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, http.MethodPatch, http.MethodDelete)
	}
}

func (a *app) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpError(w, http.StatusInternalServerError, "streaming is not available")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")

	client, err := a.hub.subscribe(r)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "could not track client")
		return
	}
	a.hub.broadcastDevices()
	defer func() {
		a.hub.unsubscribe(client.ID)
		a.hub.broadcastDevices()
	}()

	if err := writeEvent(w, "snapshot", a.store.Snapshot()); err != nil {
		return
	}
	if err := writeEvent(w, "devices", a.hub.Devices()); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case event, ok := <-client.Ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Name, event.Data); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			a.hub.touch(client.ID)
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (a *app) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, a.hub.Devices())
}

func (a *app) handleClientStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var input struct {
		BrowserID      string   `json:"browserId"`
		Online         *bool    `json:"online"`
		BatteryPercent *int     `json:"batteryPercent"`
		Charging       *bool    `json:"charging"`
		EffectiveType  string   `json:"effectiveType"`
		DownlinkMbps   *float64 `json:"downlinkMbps"`
		RTTMs          *int     `json:"rttMs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	health := sanitizeDeviceHealth(DeviceHealth{
		Online:         input.Online,
		BatteryPercent: input.BatteryPercent,
		Charging:       input.Charging,
		EffectiveType:  input.EffectiveType,
		DownlinkMbps:   input.DownlinkMbps,
		RTTMs:          input.RTTMs,
	})
	if a.hub.updateHealth(cleanBrowserID(input.BrowserID), r, health) {
		a.hub.broadcastDevices()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) handleNetwork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	_, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		port = listenPort(r.Host)
	}
	if port == "" {
		port = "8080"
	}

	host, _ := os.Hostname()
	writeJSON(w, http.StatusOK, struct {
		Host string   `json:"host"`
		URLs []string `json:"urls"`
	}{
		Host: host,
		URLs: localNetworkURLs(port),
	})
}

func (a *app) saveUploadedFiles(r *http.Request) ([]Attachment, error) {
	if r.MultipartForm == nil || len(r.MultipartForm.File["files"]) == 0 {
		return nil, nil
	}

	files := r.MultipartForm.File["files"]
	if len(files) > maxFilesPerTask {
		return nil, fmt.Errorf("attach at most %d files", maxFilesPerTask)
	}
	if err := os.MkdirAll(a.uploadDir, 0o755); err != nil {
		return nil, err
	}

	attachments := make([]Attachment, 0, len(files))
	createdPaths := make([]string, 0, len(files))
	fail := func(err error) ([]Attachment, error) {
		removeUploadedPaths(createdPaths)
		return nil, err
	}
	for _, header := range files {
		if header.Size > maxUploadBytes {
			return fail(fmt.Errorf("%s is larger than %s", header.Filename, humanBytes(maxUploadBytes)))
		}

		source, err := header.Open()
		if err != nil {
			return fail(err)
		}

		fileID, err := newID("file")
		if err != nil {
			_ = source.Close()
			return fail(err)
		}
		name := sanitizeFilename(header.Filename)
		if name == "" {
			name = fileID
		}
		contentType, err := allowedUploadType(name)
		if err != nil {
			_ = source.Close()
			return fail(err)
		}
		storedName := fileID + "_" + name
		targetPath := filepath.Join(a.uploadDir, storedName)
		target, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			_ = source.Close()
			return fail(err)
		}
		createdPaths = append(createdPaths, targetPath)

		written, copyErr := io.Copy(target, io.LimitReader(source, maxUploadBytes+1))
		closeErr := errors.Join(source.Close(), target.Close())
		if copyErr != nil {
			return fail(copyErr)
		}
		if closeErr != nil {
			return fail(closeErr)
		}
		if written > maxUploadBytes {
			return fail(fmt.Errorf("%s is larger than %s", header.Filename, humanBytes(maxUploadBytes)))
		}

		attachments = append(attachments, Attachment{
			ID:        fileID,
			Name:      name,
			URL:       "/uploads/" + storedName,
			Type:      contentType,
			Size:      written,
			CreatedAt: time.Now().UTC(),
		})
	}
	return attachments, nil
}

func (a *app) removeUploadedAttachments(attachments []Attachment) {
	for _, attachment := range attachments {
		storedName := filepath.Base(strings.TrimPrefix(attachment.URL, "/uploads/"))
		if storedName == "." || storedName == string(filepath.Separator) || storedName == "" {
			continue
		}
		removeUploadedPaths([]string{filepath.Join(a.uploadDir, storedName)})
	}
}

func removeUploadedPaths(paths []string) {
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("remove upload %s: %v", path, err)
		}
	}
}

func (h *eventHub) subscribe(r *http.Request) (*clientSession, error) {
	id, err := newID("client")
	if err != nil {
		return nil, err
	}
	browserID := cleanBrowserID(r.URL.Query().Get("client"))
	address := remoteAddress(r)
	userAgent := strings.TrimSpace(r.UserAgent())
	now := time.Now().UTC()
	client := &clientSession{
		ID:          id,
		Key:         deviceKey(browserID, address, userAgent),
		BrowserID:   browserID,
		Ch:          make(chan serverEvent, 8),
		Address:     address,
		Name:        deviceName(userAgent, address),
		UserAgent:   userAgent,
		ConnectedAt: now,
		LastSeen:    now,
	}

	h.mu.Lock()
	h.clients[client.ID] = client
	h.mu.Unlock()
	return client, nil
}

func (h *eventHub) unsubscribe(id string) {
	h.mu.Lock()
	if client, ok := h.clients[id]; ok {
		delete(h.clients, id)
		close(client.Ch)
	}
	h.mu.Unlock()
}

func (h *eventHub) updateHealth(browserID string, r *http.Request, health DeviceHealth) bool {
	key := deviceKey(browserID, remoteAddress(r), strings.TrimSpace(r.UserAgent()))
	now := time.Now().UTC()
	health.UpdatedAt = &now

	h.mu.Lock()
	defer h.mu.Unlock()
	updated := false
	for _, client := range h.clients {
		if client.Key != key {
			continue
		}
		client.Health = health
		client.LastSeen = now
		updated = true
	}
	return updated
}

func (h *eventHub) touch(id string) {
	h.mu.Lock()
	if client, ok := h.clients[id]; ok {
		client.LastSeen = time.Now().UTC()
	}
	h.mu.Unlock()
}

func (h *eventHub) broadcast(snapshot Snapshot) {
	h.broadcastEvent("snapshot", snapshot)
}

func (h *eventHub) broadcastDevices() {
	h.broadcastEvent("devices", h.Devices())
}

func (h *eventHub) broadcastEvent(name string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		select {
		case h.clients[client].Ch <- serverEvent{Name: name, Data: data}:
		default:
		}
	}
}

func (h *eventHub) Devices() DeviceSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.devicesLocked(time.Now().UTC())
}

func (h *eventHub) devicesLocked(now time.Time) DeviceSnapshot {
	byKey := make(map[string]*ConnectedDevice)
	for _, client := range h.clients {
		device, ok := byKey[client.Key]
		if !ok {
			byKey[client.Key] = &ConnectedDevice{
				ID:          deviceID(client.Key),
				Name:        client.Name,
				Address:     client.Address,
				UserAgent:   client.UserAgent,
				Health:      client.Health,
				ConnectedAt: client.ConnectedAt,
				LastSeen:    client.LastSeen,
				Connections: 1,
			}
			continue
		}
		device.Connections++
		if client.ConnectedAt.Before(device.ConnectedAt) {
			device.ConnectedAt = client.ConnectedAt
		}
		if client.LastSeen.After(device.LastSeen) {
			device.LastSeen = client.LastSeen
		}
		if healthIsNewer(client.Health, device.Health) {
			device.Health = client.Health
		}
	}

	devices := make([]ConnectedDevice, 0, len(byKey))
	for _, device := range byKey {
		devices = append(devices, *device)
	}
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].ConnectedAt.Before(devices[j].ConnectedAt)
	})
	return DeviceSnapshot{
		Devices:   devices,
		UpdatedAt: now,
	}
}

func writeEvent(w io.Writer, event string, payload any) error {
	bytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, bytes)
	return err
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func httpError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBadInput):
		httpError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errNotFound):
		httpError(w, http.StatusNotFound, "task not found")
	default:
		httpError(w, http.StatusInternalServerError, "server error")
	}
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	httpError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func (a *app) withMutationRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMutation(r) && !a.limiter.Allow(remoteAddress(r)) {
			httpError(w, http.StatusTooManyRequests, "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withCSRFProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requiresCSRF(r) && !validCSRFToken(r) {
			httpError(w, http.StatusForbidden, "invalid CSRF token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isMutation(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodPut:
		return strings.HasPrefix(r.URL.Path, "/api/")
	default:
		return false
	}
}

func withCSRFCookie(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			ensureCSRFCookie(w, r)
		}
		next.ServeHTTP(w, r)
	})
}

func requiresCSRF(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPatch, http.MethodDelete:
		return strings.HasPrefix(r.URL.Path, "/api/")
	default:
		return false
	}
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return
	}
	token, err := newCSRFToken()
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
		Secure:   requestIsHTTPS(r),
	})
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	for _, part := range strings.Split(r.Header.Get("Forwarded"), ";") {
		part = strings.TrimSpace(part)
		if strings.EqualFold(part, "proto=https") {
			return true
		}
	}
	return false
}

func validCSRFToken(r *http.Request) bool {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil {
		return false
	}
	cookieToken := strings.TrimSpace(cookie.Value)
	headerToken := strings.TrimSpace(r.Header.Get(csrfHeaderName))
	if cookieToken == "" || headerToken == "" || len(cookieToken) != len(headerToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookieToken), []byte(headerToken)) == 1
}

func newCSRFToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (l *rateLimiter) Allow(key string) bool {
	if key == "" {
		key = "unknown"
	}
	now := time.Now().UTC()

	l.mu.Lock()
	defer l.mu.Unlock()

	for client, window := range l.clients {
		if now.Sub(window.start) >= l.window {
			delete(l.clients, client)
		}
	}

	window := l.clients[key]
	if window.start.IsZero() || now.Sub(window.start) >= l.window {
		if len(l.clients) >= maxRateLimitClients {
			return false
		}
		l.clients[key] = rateWindow{start: now, count: 1}
		return true
	}
	if window.count >= l.limit {
		return false
	}
	window.count++
	l.clients[key] = window
	return true
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	var builder strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		case r == ' ':
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), ".-_")
}

func allowedUploadType(name string) (string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	allowed := map[string]string{
		".csv":  "text/csv; charset=utf-8",
		".gif":  "image/gif",
		".jpeg": "image/jpeg",
		".jpg":  "image/jpeg",
		".json": "application/json",
		".md":   "text/markdown; charset=utf-8",
		".pdf":  "application/pdf",
		".png":  "image/png",
		".txt":  "text/plain; charset=utf-8",
		".webp": "image/webp",
	}
	if contentType, ok := allowed[ext]; ok {
		return contentType, nil
	}
	if ext == "" {
		return "", fmt.Errorf("file extension is required")
	}
	return "", fmt.Errorf("%s uploads are not allowed", ext)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func listenPort(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return strings.TrimPrefix(addr, ":")
	}
	_, port, err := net.SplitHostPort(addr)
	if err == nil {
		return port
	}
	if strings.Contains(addr, ":") {
		return addr[strings.LastIndex(addr, ":")+1:]
	}
	return addr
}

func localNetworkURLs(port string) []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var urls []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isLikelyVirtualInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := ipFromAddr(addr)
			if ip == nil || ip.To4() == nil || !ip.IsPrivate() {
				continue
			}
			urls = append(urls, "http://"+ip.String()+":"+port)
		}
	}
	sort.Strings(urls)
	return urls
}

func isLikelyVirtualInterface(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	prefixes := []string{"br-", "docker", "veth", "virbr"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func remoteAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func deviceKey(browserID, address, userAgent string) string {
	if browserID != "" {
		return "browser:" + browserID
	}
	return address + "|" + userAgent
}

func deviceID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "dev_" + hex.EncodeToString(sum[:8])
}

func cleanBrowserID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 80 {
		value = value[:80]
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-', r == '_':
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func sanitizeDeviceHealth(health DeviceHealth) DeviceHealth {
	health.EffectiveType = strings.ToLower(strings.TrimSpace(health.EffectiveType))
	if len(health.EffectiveType) > 24 {
		health.EffectiveType = health.EffectiveType[:24]
	}
	if health.BatteryPercent != nil {
		value := clampInt(*health.BatteryPercent, 0, 100)
		health.BatteryPercent = &value
	}
	if health.DownlinkMbps != nil {
		value := clampFloat(*health.DownlinkMbps, 0, 10000)
		health.DownlinkMbps = &value
	}
	if health.RTTMs != nil {
		value := clampInt(*health.RTTMs, 0, 60000)
		health.RTTMs = &value
	}
	return health
}

func healthIsNewer(candidate, current DeviceHealth) bool {
	if candidate.UpdatedAt == nil {
		return false
	}
	if current.UpdatedAt == nil {
		return true
	}
	return candidate.UpdatedAt.After(*current.UpdatedAt)
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func deviceName(userAgent, fallback string) string {
	ua := strings.ToLower(userAgent)
	platform := "Device"
	switch {
	case strings.Contains(ua, "android"):
		platform = "Android"
	case strings.Contains(ua, "iphone"):
		platform = "iPhone"
	case strings.Contains(ua, "ipad"):
		platform = "iPad"
	case strings.Contains(ua, "mac os x") || strings.Contains(ua, "macintosh"):
		platform = "macOS"
	case strings.Contains(ua, "windows"):
		platform = "Windows"
	case strings.Contains(ua, "linux"):
		platform = "Linux"
	}

	browser := "browser"
	switch {
	case strings.Contains(ua, "edg/"):
		browser = "Edge"
	case strings.Contains(ua, "firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "chrome/") || strings.Contains(ua, "crios/"):
		browser = "Chrome"
	case strings.Contains(ua, "safari/"):
		browser = "Safari"
	}

	if strings.TrimSpace(userAgent) == "" && fallback != "" {
		return fallback
	}
	return platform + " " + browser
}

func ipFromAddr(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP
	case *net.IPAddr:
		return value.IP
	default:
		return nil
	}
}
