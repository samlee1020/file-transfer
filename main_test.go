package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
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

func strconvID(id int64) string {
	return strconv.FormatInt(id, 10)
}
