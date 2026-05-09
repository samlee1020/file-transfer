package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	app, err := NewApp(Config{
		DataDir:              t.TempDir(),
		MaxUploadBytes:       1 << 20,
		TextPreviewBytes:     16,
		AdminSessionTTL:      time.Hour,
		InitialAdminPassword: initialAdminPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.db.Close() })
	return app
}

func doReq(app *App, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)
	return rr
}

func decodeResp(t *testing.T, body io.Reader) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.NewDecoder(body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func uploadReq(t *testing.T, path, filename, content string) *http.Request {
	return uploadBytesReq(t, path, filename, []byte(content))
}

func uploadBytesReq(t *testing.T, path, filename string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func login(t *testing.T, app *App, password string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := doReq(app, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rr.Code, rr.Body.String())
	}
	got := decodeResp(t, rr.Body)
	data := got["data"].(map[string]any)
	return data["access_token"].(string)
}

func authReq(method, path, token string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func TestUploadDownloadAdminDeleteFlow(t *testing.T) {
	app := newTestApp(t)

	health := doReq(app, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status=%d", health.Code)
	}

	up := doReq(app, uploadReq(t, "/api/v1/files", "hello.md", "# hello\nworld\n"))
	if up.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", up.Code, up.Body.String())
	}
	uploaded := decodeResp(t, up.Body)["data"].(map[string]any)
	code := uploaded["code"].(string)
	if len(code) != 6 || uploaded["preview_kind"] != "markdown" {
		t.Fatalf("bad upload data: %#v", uploaded)
	}

	dl := doReq(app, httptest.NewRequest(http.MethodGet, "/api/v1/files/"+strings.ToLower(code)+"/download", nil))
	if dl.Code != http.StatusOK || dl.Body.String() != "# hello\nworld\n" {
		t.Fatalf("download status=%d body=%q", dl.Code, dl.Body.String())
	}

	token := login(t, app, initialAdminPassword)
	list := doReq(app, authReq(http.MethodGet, "/api/v1/admin/files", token, nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	filesData := decodeResp(t, list.Body)["data"].(map[string]any)
	items := filesData["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 file, got %#v", filesData)
	}
	file := items[0].(map[string]any)
	id := int64(file["id"].(float64))
	if file["download_count"].(float64) != 1 {
		t.Fatalf("download_count not updated: %#v", file)
	}

	prev := doReq(app, authReq(http.MethodGet, "/api/v1/admin/files/"+strconvID(id)+"/preview", token, nil))
	if prev.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", prev.Code, prev.Body.String())
	}
	previewData := decodeResp(t, prev.Body)["data"].(map[string]any)
	if !strings.Contains(previewData["content"].(string), "# hello") {
		t.Fatalf("bad preview: %#v", previewData)
	}

	del := doReq(app, authReq(http.MethodDelete, "/api/v1/admin/files/"+strconvID(id), token, nil))
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", del.Code, del.Body.String())
	}
	miss := doReq(app, httptest.NewRequest(http.MethodGet, "/api/v1/files/"+code+"/download", nil))
	if miss.Code != http.StatusNotFound {
		t.Fatalf("deleted public download status=%d", miss.Code)
	}

	events := doReq(app, authReq(http.MethodGet, "/api/v1/admin/events?page_size=10", token, nil))
	if events.Code != http.StatusOK {
		t.Fatalf("events status=%d body=%s", events.Code, events.Body.String())
	}
	eventsData := decodeResp(t, events.Body)["data"].(map[string]any)
	if eventsData["total"].(float64) < 3 {
		t.Fatalf("expected upload/download/delete events: %#v", eventsData)
	}
}

func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	app := newTestApp(t)
	first := login(t, app, initialAdminPassword)
	second := login(t, app, initialAdminPassword)

	req := authReq(http.MethodPatch, "/api/v1/admin/password", first, strings.NewReader(`{"old_password":"password123","new_password":"new-password-456"}`))
	rr := doReq(app, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("password change status=%d body=%s", rr.Code, rr.Body.String())
	}
	meOld := doReq(app, authReq(http.MethodGet, "/api/v1/admin/me", second, nil))
	if meOld.Code != http.StatusUnauthorized {
		t.Fatalf("old second session status=%d", meOld.Code)
	}
	meCurrent := doReq(app, authReq(http.MethodGet, "/api/v1/admin/me", first, nil))
	if meCurrent.Code != http.StatusOK {
		t.Fatalf("current session status=%d", meCurrent.Code)
	}
	_ = login(t, app, "new-password-456")
}

func TestUploadTooLarge(t *testing.T) {
	app := newTestApp(t)
	app.cfg.MaxUploadBytes = 4
	rr := doReq(app, uploadReq(t, "/api/v1/files", "tiny.txt", "12345"))
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestImageAndVideoPreview(t *testing.T) {
	app := newTestApp(t)
	token := login(t, app, initialAdminPassword)

	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00,
	}
	imageUpload := doReq(app, uploadBytesReq(t, "/api/v1/files", "pixel.png", png))
	if imageUpload.Code != http.StatusCreated {
		t.Fatalf("image upload status=%d body=%s", imageUpload.Code, imageUpload.Body.String())
	}
	imageData := decodeResp(t, imageUpload.Body)["data"].(map[string]any)
	if imageData["preview_kind"] != "image" || imageData["mime_type"] != "image/png" {
		t.Fatalf("bad image upload data: %#v", imageData)
	}

	videoUpload := doReq(app, uploadBytesReq(t, "/api/v1/files", "clip.mp4", []byte("not a real video, extension decides preview in mvp")))
	if videoUpload.Code != http.StatusCreated {
		t.Fatalf("video upload status=%d body=%s", videoUpload.Code, videoUpload.Body.String())
	}
	videoData := decodeResp(t, videoUpload.Body)["data"].(map[string]any)
	if videoData["preview_kind"] != "video" || videoData["mime_type"] != "video/mp4" {
		t.Fatalf("bad video upload data: %#v", videoData)
	}

	list := doReq(app, authReq(http.MethodGet, "/api/v1/admin/files?preview_kind=image", token, nil))
	if list.Code != http.StatusOK {
		t.Fatalf("image list status=%d body=%s", list.Code, list.Body.String())
	}
	filesData := decodeResp(t, list.Body)["data"].(map[string]any)
	items := filesData["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 image item, got %#v", filesData)
	}
	imageID := int64(items[0].(map[string]any)["id"].(float64))
	imagePreview := doReq(app, authReq(http.MethodGet, "/api/v1/admin/files/"+strconvID(imageID)+"/preview", token, nil))
	if imagePreview.Code != http.StatusOK || imagePreview.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("image preview status=%d content-type=%q", imagePreview.Code, imagePreview.Header().Get("Content-Type"))
	}

	listVideo := doReq(app, authReq(http.MethodGet, "/api/v1/admin/files?preview_kind=video", token, nil))
	if listVideo.Code != http.StatusOK {
		t.Fatalf("video list status=%d body=%s", listVideo.Code, listVideo.Body.String())
	}
	videoItems := decodeResp(t, listVideo.Body)["data"].(map[string]any)["items"].([]any)
	if len(videoItems) != 1 {
		t.Fatalf("expected 1 video item, got %#v", videoItems)
	}
	videoID := int64(videoItems[0].(map[string]any)["id"].(float64))
	videoPreview := doReq(app, authReq(http.MethodGet, "/api/v1/admin/files/"+strconvID(videoID)+"/preview", token, nil))
	if videoPreview.Code != http.StatusOK || videoPreview.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("video preview status=%d content-type=%q", videoPreview.Code, videoPreview.Header().Get("Content-Type"))
	}
}

func TestMigratesOldPreviewKindConstraint(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE files (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		code TEXT NOT NULL CHECK (length(code) = 6 AND code GLOB '[0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z]'),
		original_name TEXT NOT NULL CHECK (length(original_name) BETWEEN 1 AND 255),
		stored_name TEXT NOT NULL UNIQUE CHECK (length(stored_name) BETWEEN 1 AND 255),
		storage_path TEXT NOT NULL UNIQUE CHECK (length(storage_path) BETWEEN 1 AND 512),
		size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
		mime_type TEXT NOT NULL DEFAULT 'application/octet-stream' CHECK (length(mime_type) BETWEEN 1 AND 127),
		extension TEXT NOT NULL DEFAULT '' CHECK (length(extension) <= 32),
		sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
		preview_kind TEXT NOT NULL DEFAULT 'none' CHECK (preview_kind IN ('none', 'text', 'markdown', 'pdf')),
		status TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'deleted')),
		uploaded_by_role TEXT NOT NULL DEFAULT 'anonymous' CHECK (uploaded_by_role IN ('anonymous', 'admin')),
		uploaded_at TEXT NOT NULL,
		download_count INTEGER NOT NULL DEFAULT 0 CHECK (download_count >= 0),
		last_downloaded_at TEXT,
		deleted_at TEXT,
		deleted_by_role TEXT CHECK (deleted_by_role IS NULL OR deleted_by_role = 'admin'),
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		CHECK ((status = 'available' AND deleted_at IS NULL AND deleted_by_role IS NULL) OR (status = 'deleted' AND deleted_at IS NOT NULL))
	)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	app, err := NewApp(Config{DataDir: dir, InitialAdminPassword: initialAdminPassword})
	if err != nil {
		t.Fatal(err)
	}
	defer app.db.Close()
	now := nowString()
	_, err = app.db.Exec(`INSERT INTO files(code, original_name, stored_name, storage_path, size_bytes, mime_type, extension, sha256, preview_kind, status, uploaded_by_role, uploaded_at, created_at, updated_at)
		VALUES('IMG001', 'pixel.png', 'pixel.png', 'uploads/pixel.png', 1, 'image/png', 'png', ?, 'image', 'available', 'anonymous', ?, ?, ?)`,
		strings.Repeat("a", 64), now, now, now)
	if err != nil {
		t.Fatalf("image preview_kind should be accepted after migration: %v", err)
	}
}

func strconvID(id int64) string {
	return strconv.FormatInt(id, 10)
}
