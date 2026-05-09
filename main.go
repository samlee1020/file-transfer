package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
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
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	defaultMaxUploadBytes   = int64(50 << 20)
	defaultTextPreviewBytes = int64(1 << 20)
	defaultScratchpadBytes  = int64(1 << 20)
	defaultSessionTTLHours  = 24
	codeAlphabet            = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	initialAdminPassword    = "password123"
	scratchpadFileName      = "scratchpad.txt"
)

type Config struct {
	Addr                 string
	DataDir              string
	MaxUploadBytes       int64
	TextPreviewBytes     int64
	ScratchpadMaxBytes   int64
	AdminSessionTTL      time.Duration
	PublicBaseURL        string
	InitialAdminPassword string
}

type App struct {
	cfg Config
	db  *sql.DB
}

type adminContext struct {
	ID        int64
	SessionID int64
	TokenHash string
}

type fileRecord struct {
	ID               int64
	Code             string
	OriginalName     string
	StoredName       string
	StoragePath      string
	SizeBytes        int64
	MimeType         string
	Extension        string
	SHA256           string
	PreviewKind      string
	Status           string
	UploadedByRole   string
	UploadedAt       string
	DownloadCount    int64
	LastDownloadedAt sql.NullString
	DeletedAt        sql.NullString
}

type eventRecord struct {
	ID           int64
	FileID       sql.NullInt64
	FileCode     string
	OriginalName sql.NullString
	EventType    string
	ActorRole    string
	AdminID      sql.NullInt64
	Result       string
	ErrorCode    sql.NullString
	IPAddress    sql.NullString
	UserAgent    sql.NullString
	Message      sql.NullString
	OccurredAt   string
}

func main() {
	cfg := loadConfig()
	app, err := NewApp(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer app.db.Close()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("file-transfer-backend listening on %s, data_dir=%s", cfg.Addr, cfg.DataDir)
	log.Fatal(srv.ListenAndServe())
}

func loadConfig() Config {
	return Config{
		Addr:                 envString("ADDR", ":8080"),
		DataDir:              envString("DATA_DIR", "/data"),
		MaxUploadBytes:       envInt64("MAX_UPLOAD_BYTES", defaultMaxUploadBytes),
		TextPreviewBytes:     envInt64("TEXT_PREVIEW_BYTES", defaultTextPreviewBytes),
		ScratchpadMaxBytes:   envInt64("SCRATCHPAD_MAX_BYTES", defaultScratchpadBytes),
		AdminSessionTTL:      time.Duration(envInt64("ADMIN_SESSION_TTL_HOURS", defaultSessionTTLHours)) * time.Hour,
		PublicBaseURL:        strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"),
		InitialAdminPassword: envString("INITIAL_ADMIN_PASSWORD", initialAdminPassword),
	}
}

func envString(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envInt64(k string, def int64) int64 {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func NewApp(cfg Config) (*App, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = "/data"
	}
	if cfg.MaxUploadBytes <= 0 {
		cfg.MaxUploadBytes = defaultMaxUploadBytes
	}
	if cfg.TextPreviewBytes <= 0 {
		cfg.TextPreviewBytes = defaultTextPreviewBytes
	}
	if cfg.ScratchpadMaxBytes <= 0 {
		cfg.ScratchpadMaxBytes = defaultScratchpadBytes
	}
	if cfg.AdminSessionTTL <= 0 {
		cfg.AdminSessionTTL = defaultSessionTTLHours * time.Hour
	}
	if cfg.InitialAdminPassword == "" {
		cfg.InitialAdminPassword = initialAdminPassword
	}
	for _, dir := range []string{cfg.DataDir, filepath.Join(cfg.DataDir, "uploads"), filepath.Join(cfg.DataDir, "tmp"), filepath.Join(cfg.DataDir, "trash")} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(cfg.DataDir, "app.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	app := &App{cfg: cfg, db: db}
	if err := app.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := app.initAdmin(); err != nil {
		db.Close()
		return nil, err
	}
	app.cleanup()
	return app, nil
}

func (a *App) migrate() error {
	stmts := []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = NORMAL`,
		`PRAGMA busy_timeout = 5000`,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		)`,
		`CREATE TABLE IF NOT EXISTS files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			code TEXT NOT NULL CHECK (length(code) = 6 AND code GLOB '[0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z]'),
			original_name TEXT NOT NULL CHECK (length(original_name) BETWEEN 1 AND 255),
			stored_name TEXT NOT NULL UNIQUE CHECK (length(stored_name) BETWEEN 1 AND 255),
			storage_path TEXT NOT NULL UNIQUE CHECK (length(storage_path) BETWEEN 1 AND 512),
			size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
			mime_type TEXT NOT NULL DEFAULT 'application/octet-stream' CHECK (length(mime_type) BETWEEN 1 AND 127),
			extension TEXT NOT NULL DEFAULT '' CHECK (length(extension) <= 32),
			sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
			preview_kind TEXT NOT NULL DEFAULT 'none' CHECK (preview_kind IN ('none', 'text', 'markdown', 'pdf', 'image', 'video')),
			status TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'deleted')),
			uploaded_by_role TEXT NOT NULL DEFAULT 'anonymous' CHECK (uploaded_by_role IN ('anonymous', 'admin')),
			uploaded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			download_count INTEGER NOT NULL DEFAULT 0 CHECK (download_count >= 0),
			last_downloaded_at TEXT,
			deleted_at TEXT,
			deleted_by_role TEXT CHECK (deleted_by_role IS NULL OR deleted_by_role = 'admin'),
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			CHECK ((status = 'available' AND deleted_at IS NULL AND deleted_by_role IS NULL) OR (status = 'deleted' AND deleted_at IS NOT NULL))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_files_status_uploaded_at ON files (status, uploaded_at DESC, id DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_files_available_code ON files (code) WHERE status = 'available'`,
		`CREATE INDEX IF NOT EXISTS idx_files_code ON files (code)`,
		`CREATE INDEX IF NOT EXISTS idx_files_original_name ON files (original_name)`,
		`CREATE INDEX IF NOT EXISTS idx_files_preview_kind ON files (preview_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_files_sha256 ON files (sha256)`,
		`CREATE TABLE IF NOT EXISTS admin_users (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT NOT NULL UNIQUE DEFAULT 'admin' CHECK (username = 'admin'),
			password_hash TEXT NOT NULL CHECK (length(password_hash) BETWEEN 20 AND 255),
			password_algo TEXT NOT NULL CHECK (password_algo IN ('argon2id', 'bcrypt')),
			password_changed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		)`,
		`CREATE TABLE IF NOT EXISTS admin_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			admin_id INTEGER NOT NULL,
			token_hash TEXT NOT NULL UNIQUE CHECK (length(token_hash) = 64),
			expires_at TEXT NOT NULL,
			revoked_at TEXT,
			last_used_at TEXT,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			FOREIGN KEY (admin_id) REFERENCES admin_users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_sessions_token_hash ON admin_sessions (token_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_sessions_admin_expires ON admin_sessions (admin_id, expires_at DESC)`,
		`CREATE TABLE IF NOT EXISTS file_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_id INTEGER,
			file_code TEXT NOT NULL CHECK (length(file_code) = 6 AND file_code GLOB '[0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z]'),
			original_name TEXT CHECK (original_name IS NULL OR length(original_name) BETWEEN 1 AND 255),
			event_type TEXT NOT NULL CHECK (event_type IN ('upload', 'download', 'delete')),
			actor_role TEXT NOT NULL CHECK (actor_role IN ('anonymous', 'admin', 'system')),
			admin_id INTEGER,
			result TEXT NOT NULL DEFAULT 'success' CHECK (result IN ('success', 'failed')),
			error_code TEXT,
			ip_address TEXT,
			user_agent TEXT,
			message TEXT,
			occurred_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			deleted_at TEXT,
			deleted_by_admin_id INTEGER,
			FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE SET NULL,
			FOREIGN KEY (admin_id) REFERENCES admin_users(id) ON DELETE SET NULL,
			FOREIGN KEY (deleted_by_admin_id) REFERENCES admin_users(id) ON DELETE SET NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_file_events_visible_occurred ON file_events (deleted_at, occurred_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_file_events_type_occurred ON file_events (event_type, occurred_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_file_events_file_id ON file_events (file_id)`,
		`CREATE INDEX IF NOT EXISTS idx_file_events_file_code ON file_events (file_code)`,
		`INSERT OR IGNORE INTO schema_migrations(version, name) VALUES (1, 'initial')`,
	}
	for _, stmt := range stmts {
		if _, err := a.db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w: %s", err, stmt)
		}
	}
	if err := a.migratePreviewKinds(); err != nil {
		return err
	}
	return nil
}

func (a *App) migratePreviewKinds() error {
	var tableSQL string
	if err := a.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='files'`).Scan(&tableSQL); err != nil {
		return err
	}
	if strings.Contains(tableSQL, "'image'") && strings.Contains(tableSQL, "'video'") {
		_, _ = a.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version, name) VALUES (2, 'image_video_preview')`)
		return nil
	}
	if _, err := a.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() { _, _ = a.db.Exec(`PRAGMA foreign_keys = ON`) }()
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	stmts := []string{
		`DROP INDEX IF EXISTS idx_files_status_uploaded_at`,
		`DROP INDEX IF EXISTS idx_files_available_code`,
		`DROP INDEX IF EXISTS idx_files_code`,
		`DROP INDEX IF EXISTS idx_files_original_name`,
		`DROP INDEX IF EXISTS idx_files_preview_kind`,
		`DROP INDEX IF EXISTS idx_files_sha256`,
		`CREATE TABLE files_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			code TEXT NOT NULL CHECK (length(code) = 6 AND code GLOB '[0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z]'),
			original_name TEXT NOT NULL CHECK (length(original_name) BETWEEN 1 AND 255),
			stored_name TEXT NOT NULL UNIQUE CHECK (length(stored_name) BETWEEN 1 AND 255),
			storage_path TEXT NOT NULL UNIQUE CHECK (length(storage_path) BETWEEN 1 AND 512),
			size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
			mime_type TEXT NOT NULL DEFAULT 'application/octet-stream' CHECK (length(mime_type) BETWEEN 1 AND 127),
			extension TEXT NOT NULL DEFAULT '' CHECK (length(extension) <= 32),
			sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
			preview_kind TEXT NOT NULL DEFAULT 'none' CHECK (preview_kind IN ('none', 'text', 'markdown', 'pdf', 'image', 'video')),
			status TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'deleted')),
			uploaded_by_role TEXT NOT NULL DEFAULT 'anonymous' CHECK (uploaded_by_role IN ('anonymous', 'admin')),
			uploaded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			download_count INTEGER NOT NULL DEFAULT 0 CHECK (download_count >= 0),
			last_downloaded_at TEXT,
			deleted_at TEXT,
			deleted_by_role TEXT CHECK (deleted_by_role IS NULL OR deleted_by_role = 'admin'),
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			CHECK ((status = 'available' AND deleted_at IS NULL AND deleted_by_role IS NULL) OR (status = 'deleted' AND deleted_at IS NOT NULL))
		)`,
		`INSERT INTO files_new(id, code, original_name, stored_name, storage_path, size_bytes, mime_type, extension, sha256, preview_kind, status, uploaded_by_role, uploaded_at, download_count, last_downloaded_at, deleted_at, deleted_by_role, created_at, updated_at)
		 SELECT id, code, original_name, stored_name, storage_path, size_bytes, mime_type, extension, sha256, preview_kind, status, uploaded_by_role, uploaded_at, download_count, last_downloaded_at, deleted_at, deleted_by_role, created_at, updated_at FROM files`,
		`DROP TABLE files`,
		`ALTER TABLE files_new RENAME TO files`,
		`CREATE INDEX idx_files_status_uploaded_at ON files (status, uploaded_at DESC, id DESC)`,
		`CREATE UNIQUE INDEX idx_files_available_code ON files (code) WHERE status = 'available'`,
		`CREATE INDEX idx_files_code ON files (code)`,
		`CREATE INDEX idx_files_original_name ON files (original_name)`,
		`CREATE INDEX idx_files_preview_kind ON files (preview_kind)`,
		`CREATE INDEX idx_files_sha256 ON files (sha256)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate preview kinds: %w: %s", err, stmt)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, _ = a.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version, name) VALUES (2, 'image_video_preview')`)
	return nil
}

func (a *App) initAdmin() error {
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM admin_users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(a.cfg.InitialAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := nowString()
	_, err = a.db.Exec(`INSERT INTO admin_users(id, username, password_hash, password_algo, password_changed_at, created_at, updated_at)
		VALUES(1, 'admin', ?, 'bcrypt', ?, ?, ?)`, string(hash), now, now, now)
	return err
}

func (a *App) cleanup() {
	cutoff := time.Now().Add(-24 * time.Hour)
	cleanDir(filepath.Join(a.cfg.DataDir, "tmp"), cutoff)
	cleanDir(filepath.Join(a.cfg.DataDir, "trash"), cutoff)
	_, _ = a.db.Exec(`DELETE FROM admin_sessions WHERE expires_at < ? OR revoked_at IS NOT NULL`, nowString())
}

func cleanDir(dir string, cutoff time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err == nil && !info.IsDir() && info.ModTime().Before(cutoff) {
			_ = os.Remove(p)
		}
	}
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	switch {
	case path == "/healthz" && r.Method == http.MethodGet:
		writeRawJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case path == "/api/v1/files" && r.Method == http.MethodPost:
		a.handleUpload(w, r)
	case strings.HasPrefix(path, "/api/v1/files/") && strings.HasSuffix(path, "/download") && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		a.handlePublicDownload(w, r)
	case path == "/api/v1/admin/login" && r.Method == http.MethodPost:
		a.handleLogin(w, r)
	case strings.HasPrefix(path, "/api/v1/admin/"):
		a.handleAdmin(w, r, path)
	default:
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "not found", nil)
	}
}

func (a *App) handleAdmin(w http.ResponseWriter, r *http.Request, path string) {
	admin, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	switch {
	case path == "/api/v1/admin/me" && r.Method == http.MethodGet:
		a.handleMe(w, r)
	case path == "/api/v1/admin/password" && r.Method == http.MethodPatch:
		a.handlePassword(w, r, admin)
	case path == "/api/v1/admin/logout" && r.Method == http.MethodPost:
		_, _ = a.db.Exec(`UPDATE admin_sessions SET revoked_at=?, updated_at=? WHERE id=?`, nowString(), nowString(), admin.SessionID)
		w.WriteHeader(http.StatusNoContent)
	case path == "/api/v1/admin/scratchpad" && r.Method == http.MethodGet:
		a.handleGetScratchpad(w, r)
	case path == "/api/v1/admin/scratchpad" && r.Method == http.MethodPut:
		a.handlePutScratchpad(w, r)
	case path == "/api/v1/admin/files" && r.Method == http.MethodGet:
		a.handleListFiles(w, r)
	case path == "/api/v1/admin/files/batch-delete" && r.Method == http.MethodPost:
		a.handleBatchDeleteFiles(w, r, admin)
	case strings.HasPrefix(path, "/api/v1/admin/files/"):
		a.handleAdminFile(w, r, path, admin)
	case path == "/api/v1/admin/events" && r.Method == http.MethodGet:
		a.handleListEvents(w, r)
	case path == "/api/v1/admin/events/batch-delete" && r.Method == http.MethodPost:
		a.handleBatchDeleteEvents(w, r, admin)
	case strings.HasPrefix(path, "/api/v1/admin/events/") && r.Method == http.MethodDelete:
		id, err := parseID(strings.TrimPrefix(path, "/api/v1/admin/events/"))
		if err != nil {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "event not found", nil)
			return
		}
		a.deleteEvent(w, r, id, admin.ID)
	default:
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "not found", nil)
	}
}

func (a *App) handleAdminFile(w http.ResponseWriter, r *http.Request, path string, admin adminContext) {
	rest := strings.TrimPrefix(path, "/api/v1/admin/files/")
	parts := strings.Split(rest, "/")
	id, err := parseID(parts[0])
	if err != nil {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "file not found", nil)
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			rec, err := a.getFileByID(id)
			if err != nil {
				writeError(w, r, http.StatusNotFound, "NOT_FOUND", "file not found", nil)
				return
			}
			writeJSON(w, r, http.StatusOK, fileAdminJSON(rec))
		case http.MethodDelete:
			res := a.deleteFileByID(r, id, admin.ID)
			if res.Status == "not_found" {
				writeError(w, r, http.StatusNotFound, "NOT_FOUND", "file not found", nil)
				return
			}
			if res.Status == "failed" {
				writeError(w, r, http.StatusInternalServerError, "INTERNAL", res.Message, nil)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
		}
		return
	}
	if len(parts) == 2 && parts[1] == "download" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		a.handleAdminDownload(w, r, id, admin)
		return
	}
	if len(parts) == 2 && parts[1] == "preview" && r.Method == http.MethodGet {
		a.handlePreview(w, r, id)
		return
	}
	writeError(w, r, http.StatusNotFound, "NOT_FOUND", "not found", nil)
}

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		writeError(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "multipart/form-data is required", nil)
		return
	}
	admin, isAdmin := a.optionalAdmin(r)
	role := "anonymous"
	var adminID sql.NullInt64
	if isAdmin {
		role = "admin"
		adminID = sql.NullInt64{Int64: admin.ID, Valid: true}
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxUploadBytes+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid multipart body", nil)
		return
	}
	var partFound bool
	var tmpPath string
	var meta uploadMeta
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid multipart body", nil)
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		partFound = true
		meta.OriginalName = sanitizeFilename(part.FileName())
		if meta.OriginalName == "" || len(meta.OriginalName) > 255 {
			writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "file name is invalid", map[string]string{"field": "file"})
			return
		}
		tmp, err := os.CreateTemp(filepath.Join(a.cfg.DataDir, "tmp"), "upload-*")
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot create temporary file", nil)
			return
		}
		tmpPath = tmp.Name()
		meta, err = streamUpload(part, tmp, meta, a.cfg.MaxUploadBytes)
		_ = tmp.Close()
		_ = part.Close()
		if err != nil {
			_ = os.Remove(tmpPath)
			if errors.Is(err, errTooLarge) {
				writeError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "file exceeds max upload size", nil)
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot store uploaded file", nil)
			return
		}
		break
	}
	if !partFound {
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "file is required", map[string]string{"field": "file"})
		return
	}
	rec, err := a.commitUpload(r, tmpPath, meta, role, adminID)
	if err != nil {
		_ = os.Remove(tmpPath)
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot save file metadata", nil)
		return
	}
	writeJSON(w, r, http.StatusCreated, map[string]any{
		"code":          rec.Code,
		"original_name": rec.OriginalName,
		"size_bytes":    rec.SizeBytes,
		"mime_type":     rec.MimeType,
		"sha256":        rec.SHA256,
		"preview_kind":  rec.PreviewKind,
		"uploaded_at":   rec.UploadedAt,
		"download_url":  a.downloadURL(rec.Code),
	})
}

var errTooLarge = errors.New("file too large")

type uploadMeta struct {
	OriginalName string
	SizeBytes    int64
	MimeType     string
	Extension    string
	SHA256       string
	PreviewKind  string
	FirstBytes   []byte
}

func streamUpload(src io.Reader, dst io.Writer, meta uploadMeta, max int64) (uploadMeta, error) {
	h := sha256.New()
	var first []byte
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, er := src.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > max {
				return meta, errTooLarge
			}
			if len(first) < 512 {
				take := n
				if len(first)+take > 512 {
					take = 512 - len(first)
				}
				first = append(first, buf[:take]...)
			}
			if _, err := dst.Write(buf[:n]); err != nil {
				return meta, err
			}
			if _, err := h.Write(buf[:n]); err != nil {
				return meta, err
			}
		}
		if errors.Is(er, io.EOF) {
			break
		}
		if er != nil {
			return meta, er
		}
	}
	meta.SizeBytes = total
	meta.SHA256 = hex.EncodeToString(h.Sum(nil))
	meta.FirstBytes = first
	meta.Extension = strings.TrimPrefix(strings.ToLower(filepath.Ext(meta.OriginalName)), ".")
	meta.MimeType = detectMime(meta.Extension, first)
	meta.PreviewKind = previewKind(meta.Extension, meta.MimeType)
	return meta, nil
}

func detectMime(ext string, first []byte) string {
	switch ext {
	case "txt":
		return "text/plain"
	case "md", "markdown":
		return "text/markdown"
	case "pdf":
		return "application/pdf"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "bmp":
		return "image/bmp"
	case "avif":
		return "image/avif"
	case "mp4", "m4v":
		return "video/mp4"
	case "webm":
		return "video/webm"
	case "ogv", "ogg":
		return "video/ogg"
	case "mov":
		return "video/quicktime"
	}
	if len(first) == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(first)
}

func previewKind(ext, mt string) string {
	mt = strings.ToLower(strings.Split(mt, ";")[0])
	switch {
	case ext == "txt" || mt == "text/plain":
		return "text"
	case ext == "md" || ext == "markdown" || mt == "text/markdown":
		return "markdown"
	case ext == "pdf" || mt == "application/pdf":
		return "pdf"
	case isImagePreview(ext, mt):
		return "image"
	case isVideoPreview(ext, mt):
		return "video"
	default:
		return "none"
	}
}

func isImagePreview(ext, mt string) bool {
	switch ext {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "avif":
		return true
	}
	return strings.HasPrefix(mt, "image/") && mt != "image/svg+xml"
}

func isVideoPreview(ext, mt string) bool {
	switch ext {
	case "mp4", "m4v", "webm", "ogv", "ogg", "mov":
		return true
	}
	return strings.HasPrefix(mt, "video/")
}

func (a *App) commitUpload(r *http.Request, tmpPath string, meta uploadMeta, role string, adminID sql.NullInt64) (fileRecord, error) {
	var lastErr error
	for i := 0; i < 8; i++ {
		code, err := randomCode()
		if err != nil {
			return fileRecord{}, err
		}
		suffix, err := randomString(8)
		if err != nil {
			return fileRecord{}, err
		}
		now := nowString()
		day := time.Now().UTC().Format("2006/01/02")
		storedName := code + "-" + suffix
		if meta.Extension != "" {
			storedName += "." + meta.Extension
		}
		storagePath := filepath.ToSlash(filepath.Join("uploads", day, storedName))
		finalPath := filepath.Join(a.cfg.DataDir, filepath.FromSlash(storagePath))
		if err := os.MkdirAll(filepath.Dir(finalPath), 0750); err != nil {
			return fileRecord{}, err
		}
		if err := os.Rename(tmpPath, finalPath); err != nil {
			return fileRecord{}, err
		}
		tx, err := a.db.Begin()
		if err != nil {
			_ = os.Rename(finalPath, tmpPath)
			return fileRecord{}, err
		}
		res, err := tx.Exec(`INSERT INTO files(code, original_name, stored_name, storage_path, size_bytes, mime_type, extension, sha256, preview_kind, status, uploaded_by_role, uploaded_at, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, 'available', ?, ?, ?, ?)`,
			code, meta.OriginalName, storedName, storagePath, meta.SizeBytes, meta.MimeType, meta.Extension, meta.SHA256, meta.PreviewKind, role, now, now, now)
		if err != nil {
			_ = tx.Rollback()
			_ = os.Rename(finalPath, tmpPath)
			lastErr = err
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				continue
			}
			return fileRecord{}, err
		}
		id, _ := res.LastInsertId()
		if err := insertEventTx(tx, r, id, code, meta.OriginalName, "upload", role, adminID, "success", "", ""); err != nil {
			_ = tx.Rollback()
			_ = os.Rename(finalPath, tmpPath)
			return fileRecord{}, err
		}
		if err := tx.Commit(); err != nil {
			_ = os.Rename(finalPath, tmpPath)
			return fileRecord{}, err
		}
		return fileRecord{
			ID: id, Code: code, OriginalName: meta.OriginalName, StoredName: storedName, StoragePath: storagePath,
			SizeBytes: meta.SizeBytes, MimeType: meta.MimeType, Extension: meta.Extension, SHA256: meta.SHA256,
			PreviewKind: meta.PreviewKind, Status: "available", UploadedByRole: role, UploadedAt: now,
		}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("could not generate unique code")
	}
	return fileRecord{}, lastErr
}

func (a *App) downloadURL(code string) string {
	path := "/api/v1/files/" + code + "/download"
	if a.cfg.PublicBaseURL == "" {
		return path
	}
	return a.cfg.PublicBaseURL + path
}

func (a *App) handlePublicDownload(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/"), "/api/v1/files/")
	code := strings.TrimSuffix(rest, "/download")
	code = strings.Trim(strings.TrimSuffix(code, "/"), "/")
	code = normalizeCode(code)
	if !validCode(code) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "file not found", nil)
		return
	}
	rec, err := a.getAvailableFileByCode(code)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "file not found", nil)
		return
	}
	a.serveFile(w, r, rec, false, adminContext{})
}

func (a *App) handleAdminDownload(w http.ResponseWriter, r *http.Request, id int64, admin adminContext) {
	rec, err := a.getFileByID(id)
	if err != nil || rec.Status != "available" {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "file not found", nil)
		return
	}
	a.serveFile(w, r, rec, true, admin)
}

func (a *App) serveFile(w http.ResponseWriter, r *http.Request, rec fileRecord, adminDownload bool, admin adminContext) {
	f, err := os.Open(filepath.Join(a.cfg.DataDir, filepath.FromSlash(rec.StoragePath)))
	if err != nil {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "file not found", nil)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", rec.MimeType)
	w.Header().Set("Content-Disposition", contentDisposition("attachment", rec.OriginalName))
	w.Header().Set("ETag", strconv.Quote("sha256-"+rec.SHA256))
	w.Header().Set("Accept-Ranges", "bytes")
	if r.Method == http.MethodGet {
		role := "anonymous"
		var adminID sql.NullInt64
		if adminDownload {
			role = "admin"
			adminID = sql.NullInt64{Int64: admin.ID, Valid: true}
		}
		a.recordDownload(r, rec, role, adminID)
	}
	http.ServeContent(w, r, rec.OriginalName, time.Now(), f)
}

func (a *App) recordDownload(r *http.Request, rec fileRecord, role string, adminID sql.NullInt64) {
	now := nowString()
	_, _ = a.db.Exec(`UPDATE files SET download_count = download_count + 1, last_downloaded_at=?, updated_at=? WHERE id=? AND status='available'`, now, now, rec.ID)
	_, _ = a.db.Exec(`INSERT INTO file_events(file_id, file_code, original_name, event_type, actor_role, admin_id, result, ip_address, user_agent, occurred_at)
		VALUES(?, ?, ?, 'download', ?, ?, 'success', ?, ?, ?)`, rec.ID, rec.Code, rec.OriginalName, role, nullIntValue(adminID), clientIP(r), r.UserAgent(), now)
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var id int64
	var hash, changedAt string
	err := a.db.QueryRow(`SELECT id, password_hash, password_changed_at FROM admin_users WHERE username='admin' AND id=1`).Scan(&id, &hash, &changedAt)
	if err != nil || req.Username != "admin" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "invalid username or password", nil)
		return
	}
	token, err := randomToken()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot create session", nil)
		return
	}
	expires := time.Now().UTC().Add(a.cfg.AdminSessionTTL)
	th := tokenHash(token)
	now := nowString()
	if _, err := a.db.Exec(`INSERT INTO admin_sessions(admin_id, token_hash, expires_at, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`, id, th, timeString(expires), now, now); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot create session", nil)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_at":   timeString(expires),
		"admin": map[string]any{
			"id":                  id,
			"username":            "admin",
			"password_changed_at": changedAt,
		},
	})
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	var changedAt string
	if err := a.db.QueryRow(`SELECT password_changed_at FROM admin_users WHERE id=1`).Scan(&changedAt); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot load admin", nil)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"id": 1, "username": "admin", "password_changed_at": changedAt})
}

func (a *App) handlePassword(w http.ResponseWriter, r *http.Request, admin adminContext) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.NewPassword) < 8 || len(req.NewPassword) > 128 {
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "new password length must be between 8 and 128", map[string]string{"field": "new_password"})
		return
	}
	var oldHash string
	if err := a.db.QueryRow(`SELECT password_hash FROM admin_users WHERE id=1`).Scan(&oldHash); err != nil ||
		bcrypt.CompareHashAndPassword([]byte(oldHash), []byte(req.OldPassword)) != nil {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "old password is invalid", nil)
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot update password", nil)
		return
	}
	now := nowString()
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot update password", nil)
		return
	}
	_, err = tx.Exec(`UPDATE admin_users SET password_hash=?, password_changed_at=?, updated_at=? WHERE id=1`, string(newHash), now, now)
	if err == nil {
		_, err = tx.Exec(`UPDATE admin_sessions SET revoked_at=?, updated_at=? WHERE admin_id=1 AND id<>? AND revoked_at IS NULL`, now, now, admin.SessionID)
	}
	if err != nil {
		_ = tx.Rollback()
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot update password", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot update password", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleGetScratchpad(w http.ResponseWriter, r *http.Request) {
	content, updatedAt, err := a.readScratchpad()
	if err != nil {
		if errors.Is(err, errScratchpadTooLarge) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "scratchpad exceeds max size", nil)
			return
		}
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot read scratchpad", nil)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"content":    content,
		"updated_at": nullableString(updatedAt),
		"size_bytes": len([]byte(content)),
		"max_bytes":  a.cfg.ScratchpadMaxBytes,
	})
}

func (a *App) handlePutScratchpad(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		writeError(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "application/json is required", nil)
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, a.cfg.ScratchpadMaxBytes+4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid json body", nil)
		return
	}
	if int64(len([]byte(req.Content))) > a.cfg.ScratchpadMaxBytes {
		writeError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "scratchpad exceeds max size", nil)
		return
	}
	updatedAt, err := a.writeScratchpad(req.Content)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot save scratchpad", nil)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"content":    req.Content,
		"updated_at": updatedAt,
		"size_bytes": len([]byte(req.Content)),
		"max_bytes":  a.cfg.ScratchpadMaxBytes,
	})
}

var errScratchpadTooLarge = errors.New("scratchpad too large")

func (a *App) scratchpadPath() string {
	return filepath.Join(a.cfg.DataDir, scratchpadFileName)
}

func (a *App) readScratchpad() (string, sql.NullString, error) {
	path := a.scratchpadPath()
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", sql.NullString{}, nil
	}
	if err != nil {
		return "", sql.NullString{}, err
	}
	if info.Size() > a.cfg.ScratchpadMaxBytes {
		return "", sql.NullString{}, errScratchpadTooLarge
	}
	f, err := os.Open(path)
	if err != nil {
		return "", sql.NullString{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, a.cfg.ScratchpadMaxBytes+1))
	if err != nil {
		return "", sql.NullString{}, err
	}
	if int64(len(data)) > a.cfg.ScratchpadMaxBytes {
		return "", sql.NullString{}, errScratchpadTooLarge
	}
	return string(data), sql.NullString{String: timeString(info.ModTime().UTC()), Valid: true}, nil
}

func (a *App) writeScratchpad(content string) (string, error) {
	tmp, err := os.CreateTemp(filepath.Join(a.cfg.DataDir, "tmp"), "scratchpad-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	finalPath := a.scratchpadPath()
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", err
	}
	cleanup = false
	info, err := os.Stat(finalPath)
	if err != nil {
		return nowString(), nil
	}
	return timeString(info.ModTime().UTC()), nil
}

func (a *App) handleListFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, pageSize := pageParams(q.Get("page"), q.Get("page_size"))
	var where []string
	var args []any
	status := q.Get("status")
	if status == "" {
		status = "available"
	}
	switch status {
	case "available", "deleted":
		where = append(where, "status = ?")
		args = append(args, status)
	case "all":
	default:
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid status", map[string]string{"field": "status"})
		return
	}
	if s := strings.TrimSpace(q.Get("q")); s != "" {
		where = append(where, "(original_name LIKE ? OR code LIKE ?)")
		args = append(args, "%"+s+"%", "%"+normalizeCode(s)+"%")
	}
	if pk := q.Get("preview_kind"); pk != "" {
		if !in(pk, "none", "text", "markdown", "pdf", "image", "video") {
			writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid preview_kind", nil)
			return
		}
		where = append(where, "preview_kind = ?")
		args = append(args, pk)
	}
	if mt := q.Get("mime_type"); mt != "" {
		where = append(where, "mime_type = ?")
		args = append(args, mt)
	}
	if from := q.Get("uploaded_from"); from != "" {
		where = append(where, "uploaded_at >= ?")
		args = append(args, from)
	}
	if to := q.Get("uploaded_to"); to != "" {
		where = append(where, "uploaded_at <= ?")
		args = append(args, to)
	}
	clause := whereClause(where)
	var total int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM files `+clause, args...).Scan(&total); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot list files", nil)
		return
	}
	order := fileOrder(q.Get("sort"))
	args2 := append(args, pageSize, (page-1)*pageSize)
	rows, err := a.db.Query(`SELECT id, code, original_name, stored_name, storage_path, size_bytes, mime_type, extension, sha256, preview_kind, status, uploaded_by_role, uploaded_at, download_count, last_downloaded_at, deleted_at
		FROM files `+clause+` ORDER BY `+order+` LIMIT ? OFFSET ?`, args2...)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot list files", nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		rec, err := scanFile(rows)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot list files", nil)
			return
		}
		items = append(items, fileAdminJSON(rec))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "page": page, "page_size": pageSize, "total": total, "has_more": page*pageSize < total})
}

func (a *App) handlePreview(w http.ResponseWriter, r *http.Request, id int64) {
	rec, err := a.getFileByID(id)
	if err != nil || rec.Status != "available" {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "file not found", nil)
		return
	}
	path := filepath.Join(a.cfg.DataDir, filepath.FromSlash(rec.StoragePath))
	switch rec.PreviewKind {
	case "text", "markdown":
		f, err := os.Open(path)
		if err != nil {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "file not found", nil)
			return
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, a.cfg.TextPreviewBytes+1))
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot read preview", nil)
			return
		}
		truncated := int64(len(data)) > a.cfg.TextPreviewBytes
		if truncated {
			data = data[:a.cfg.TextPreviewBytes]
		}
		content := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
		if !utf8.ValidString(content) {
			writeError(w, r, http.StatusUnprocessableEntity, "UNSUPPORTED_PREVIEW", "text is not valid utf-8", nil)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"kind": rec.PreviewKind, "encoding": "utf-8", "content": content, "truncated": truncated, "bytes_read": len(data),
		})
	case "pdf", "image", "video":
		a.serveInlinePreview(w, r, rec, path)
	default:
		writeError(w, r, http.StatusUnprocessableEntity, "UNSUPPORTED_PREVIEW", "file preview is not supported", nil)
	}
}

func (a *App) serveInlinePreview(w http.ResponseWriter, r *http.Request, rec fileRecord, path string) {
	f, err := os.Open(path)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "file not found", nil)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", rec.MimeType)
	w.Header().Set("Content-Disposition", contentDisposition("inline", rec.OriginalName))
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, rec.OriginalName, time.Now(), f)
}

func (a *App) handleBatchDeleteFiles(w http.ResponseWriter, r *http.Request, admin adminContext) {
	var req struct {
		FileIDs []int64 `json:"file_ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ids, ok := normalizeIDs(req.FileIDs)
	if !ok {
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "file_ids must contain 1 to 100 positive ids", nil)
		return
	}
	items := make([]any, 0, len(ids))
	summary := map[string]int{"deleted": 0, "already_deleted": 0, "not_found": 0, "failed": 0}
	for _, id := range ids {
		res := a.deleteFileByID(r, id, admin.ID)
		summary[res.Status]++
		item := map[string]any{"id": id, "code": nil, "status": res.Status}
		if res.Code != "" {
			item["code"] = res.Code
		}
		if res.Message != "" {
			item["message"] = res.Message
		}
		items = append(items, item)
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "summary": summary})
}

type deleteResult struct {
	Code    string
	Status  string
	Message string
}

func (a *App) deleteFileByID(r *http.Request, id int64, adminID int64) deleteResult {
	rec, err := a.getFileByID(id)
	if err != nil {
		return deleteResult{Status: "not_found"}
	}
	if rec.Status == "deleted" {
		return deleteResult{Code: rec.Code, Status: "already_deleted"}
	}
	tx, err := a.db.Begin()
	if err != nil {
		return deleteResult{Code: rec.Code, Status: "failed", Message: err.Error()}
	}
	now := nowString()
	trashPath := filepath.Join(a.cfg.DataDir, "trash", fmt.Sprintf("%d-%s", id, filepath.Base(rec.StoragePath)))
	srcPath := filepath.Join(a.cfg.DataDir, filepath.FromSlash(rec.StoragePath))
	moved := false
	if err := os.MkdirAll(filepath.Dir(trashPath), 0750); err == nil {
		if err := os.Rename(srcPath, trashPath); err == nil {
			moved = true
		} else if !errors.Is(err, os.ErrNotExist) {
			_ = tx.Rollback()
			return deleteResult{Code: rec.Code, Status: "failed", Message: err.Error()}
		}
	}
	_, err = tx.Exec(`UPDATE files SET status='deleted', deleted_at=?, deleted_by_role='admin', updated_at=? WHERE id=? AND status='available'`, now, now, id)
	if err == nil {
		err = insertEventTx(tx, r, rec.ID, rec.Code, rec.OriginalName, "delete", "admin", sql.NullInt64{Int64: adminID, Valid: true}, "success", "", "")
	}
	if err != nil {
		_ = tx.Rollback()
		if moved {
			_ = os.Rename(trashPath, srcPath)
		}
		return deleteResult{Code: rec.Code, Status: "failed", Message: err.Error()}
	}
	if err := tx.Commit(); err != nil {
		if moved {
			_ = os.Rename(trashPath, srcPath)
		}
		return deleteResult{Code: rec.Code, Status: "failed", Message: err.Error()}
	}
	if moved {
		_ = os.Remove(trashPath)
	}
	return deleteResult{Code: rec.Code, Status: "deleted"}
}

func (a *App) handleListEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, pageSize := pageParams(q.Get("page"), q.Get("page_size"))
	var where []string
	var args []any
	if q.Get("include_deleted") != "true" {
		where = append(where, "deleted_at IS NULL")
	}
	for _, f := range []struct{ key, col string }{{"event_type", "event_type"}, {"actor_role", "actor_role"}, {"result", "result"}, {"file_code", "file_code"}} {
		if v := strings.TrimSpace(q.Get(f.key)); v != "" {
			if f.key == "file_code" {
				v = normalizeCode(v)
			}
			where = append(where, f.col+" = ?")
			args = append(args, v)
		}
	}
	if fid := q.Get("file_id"); fid != "" {
		id, err := parseID(fid)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid file_id", nil)
			return
		}
		where = append(where, "file_id = ?")
		args = append(args, id)
	}
	if from := q.Get("occurred_from"); from != "" {
		where = append(where, "occurred_at >= ?")
		args = append(args, from)
	}
	if to := q.Get("occurred_to"); to != "" {
		where = append(where, "occurred_at <= ?")
		args = append(args, to)
	}
	clause := whereClause(where)
	var total int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM file_events `+clause, args...).Scan(&total); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot list events", nil)
		return
	}
	order := "occurred_at DESC, id DESC"
	if q.Get("sort") == "occurred_at" {
		order = "occurred_at ASC, id ASC"
	}
	args2 := append(args, pageSize, (page-1)*pageSize)
	rows, err := a.db.Query(`SELECT id, file_id, file_code, original_name, event_type, actor_role, admin_id, result, error_code, ip_address, user_agent, message, occurred_at
		FROM file_events `+clause+` ORDER BY `+order+` LIMIT ? OFFSET ?`, args2...)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot list events", nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var ev eventRecord
		if err := rows.Scan(&ev.ID, &ev.FileID, &ev.FileCode, &ev.OriginalName, &ev.EventType, &ev.ActorRole, &ev.AdminID, &ev.Result, &ev.ErrorCode, &ev.IPAddress, &ev.UserAgent, &ev.Message, &ev.OccurredAt); err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL", "cannot list events", nil)
			return
		}
		items = append(items, eventJSON(ev))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "page": page, "page_size": pageSize, "total": total, "has_more": page*pageSize < total})
}

func (a *App) deleteEvent(w http.ResponseWriter, r *http.Request, id, adminID int64) {
	var exists int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM file_events WHERE id=?`, id).Scan(&exists); err != nil || exists == 0 {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "event not found", nil)
		return
	}
	now := nowString()
	_, _ = a.db.Exec(`UPDATE file_events SET deleted_at=?, deleted_by_admin_id=? WHERE id=? AND deleted_at IS NULL`, now, adminID, id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleBatchDeleteEvents(w http.ResponseWriter, r *http.Request, admin adminContext) {
	var req struct {
		EventIDs []int64 `json:"event_ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ids, ok := normalizeIDs(req.EventIDs)
	if !ok {
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "event_ids must contain 1 to 100 positive ids", nil)
		return
	}
	items := make([]any, 0, len(ids))
	summary := map[string]int{"deleted": 0, "already_deleted": 0, "not_found": 0, "failed": 0}
	now := nowString()
	for _, id := range ids {
		var deletedAt sql.NullString
		err := a.db.QueryRow(`SELECT deleted_at FROM file_events WHERE id=?`, id).Scan(&deletedAt)
		status := "deleted"
		if errors.Is(err, sql.ErrNoRows) {
			status = "not_found"
		} else if err != nil {
			status = "failed"
		} else if deletedAt.Valid {
			status = "already_deleted"
		} else if _, err := a.db.Exec(`UPDATE file_events SET deleted_at=?, deleted_by_admin_id=? WHERE id=?`, now, admin.ID, id); err != nil {
			status = "failed"
		}
		summary[status]++
		items = append(items, map[string]any{"id": id, "status": status})
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "summary": summary})
}

func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request) (adminContext, bool) {
	admin, ok := a.optionalAdmin(r)
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "admin authorization is required", nil)
		return adminContext{}, false
	}
	return admin, true
}

func (a *App) optionalAdmin(r *http.Request) (adminContext, bool) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return adminContext{}, false
	}
	token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	if token == "" {
		return adminContext{}, false
	}
	th := tokenHash(token)
	var admin adminContext
	err := a.db.QueryRow(`SELECT id, admin_id FROM admin_sessions WHERE token_hash=? AND revoked_at IS NULL AND expires_at > ?`, th, nowString()).Scan(&admin.SessionID, &admin.ID)
	if err != nil {
		return adminContext{}, false
	}
	admin.TokenHash = th
	_, _ = a.db.Exec(`UPDATE admin_sessions SET last_used_at=?, updated_at=? WHERE id=?`, nowString(), nowString(), admin.SessionID)
	return admin, true
}

func (a *App) getAvailableFileByCode(code string) (fileRecord, error) {
	return a.getFileWhere(`code=? AND status='available'`, code)
}

func (a *App) getFileByID(id int64) (fileRecord, error) {
	return a.getFileWhere(`id=?`, id)
}

func (a *App) getFileWhere(where string, arg any) (fileRecord, error) {
	row := a.db.QueryRow(`SELECT id, code, original_name, stored_name, storage_path, size_bytes, mime_type, extension, sha256, preview_kind, status, uploaded_by_role, uploaded_at, download_count, last_downloaded_at, deleted_at FROM files WHERE `+where, arg)
	return scanFile(row)
}

type fileScanner interface {
	Scan(dest ...any) error
}

func scanFile(s fileScanner) (fileRecord, error) {
	var rec fileRecord
	err := s.Scan(&rec.ID, &rec.Code, &rec.OriginalName, &rec.StoredName, &rec.StoragePath, &rec.SizeBytes, &rec.MimeType, &rec.Extension, &rec.SHA256, &rec.PreviewKind, &rec.Status, &rec.UploadedByRole, &rec.UploadedAt, &rec.DownloadCount, &rec.LastDownloadedAt, &rec.DeletedAt)
	return rec, err
}

func insertEventTx(tx *sql.Tx, r *http.Request, fileID int64, code, name, eventType, role string, adminID sql.NullInt64, result, errCode, msg string) error {
	var fid any = fileID
	if fileID == 0 {
		fid = nil
	}
	_, err := tx.Exec(`INSERT INTO file_events(file_id, file_code, original_name, event_type, actor_role, admin_id, result, error_code, ip_address, user_agent, message, occurred_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, fid, code, name, eventType, role, nullIntValue(adminID), result, nullString(errCode), clientIP(r), r.UserAgent(), nullString(msg), nowString())
	return err
}

func fileAdminJSON(rec fileRecord) map[string]any {
	return map[string]any{
		"id":                 rec.ID,
		"code":               rec.Code,
		"original_name":      rec.OriginalName,
		"size_bytes":         rec.SizeBytes,
		"mime_type":          rec.MimeType,
		"extension":          rec.Extension,
		"sha256":             rec.SHA256,
		"preview_kind":       rec.PreviewKind,
		"status":             rec.Status,
		"uploaded_by_role":   rec.UploadedByRole,
		"uploaded_at":        rec.UploadedAt,
		"download_count":     rec.DownloadCount,
		"last_downloaded_at": nullableString(rec.LastDownloadedAt),
		"deleted_at":         nullableString(rec.DeletedAt),
	}
}

func eventJSON(ev eventRecord) map[string]any {
	return map[string]any{
		"id":            ev.ID,
		"file_id":       nullableInt(ev.FileID),
		"file_code":     ev.FileCode,
		"original_name": nullableString(ev.OriginalName),
		"event_type":    ev.EventType,
		"actor_role":    ev.ActorRole,
		"admin_id":      nullableInt(ev.AdminID),
		"result":        ev.Result,
		"error_code":    nullableString(ev.ErrorCode),
		"ip_address":    nullableString(ev.IPAddress),
		"user_agent":    nullableString(ev.UserAgent),
		"message":       nullableString(ev.Message),
		"occurred_at":   ev.OccurredAt,
	}
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	writeRawJSON(w, status, map[string]any{"request_id": requestID(r), "data": data})
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	body := map[string]any{"request_id": requestID(r), "error": map[string]any{"code": code, "message": message}}
	if details != nil {
		body["error"].(map[string]any)["details"] = details
	}
	writeRawJSON(w, status, body)
}

func writeRawJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		writeError(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "application/json is required", nil)
		return false
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid json body", nil)
		return false
	}
	return true
}

func requestID(r *http.Request) string {
	if v := r.Header.Get("X-Request-ID"); v != "" {
		return v
	}
	s, _ := randomString(16)
	return "req_" + s
}

func randomCode() (string, error) { return randomFromAlphabet(6, codeAlphabet) }

func randomString(n int) (string, error) { return randomFromAlphabet(n, codeAlphabet) }

func randomFromAlphabet(n int, alphabet string) (string, error) {
	b := make([]byte, n)
	rb := make([]byte, n)
	if _, err := rand.Read(rb); err != nil {
		return "", err
	}
	for i := range rb {
		b[i] = alphabet[int(rb[i])%len(alphabet)]
	}
	return string(b), nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "adm_sess_" + hex.EncodeToString(b), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func normalizeCode(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

func validCode(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	return strings.TrimSpace(name)
}

func contentDisposition(kind, filename string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '"' || r == '\\' || r == ';' {
			return '_'
		}
		if r > 126 {
			return '_'
		}
		return r
	}, filename)
	if ascii == "" {
		ascii = "download"
	}
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, kind, ascii, urlPathEscape(filename))
}

func urlPathEscape(s string) string {
	const hexChars = "0123456789ABCDEF"
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hexChars[c>>4])
			b.WriteByte(hexChars[c&15])
		}
	}
	return b.String()
}

func nowString() string { return timeString(time.Now().UTC()) }

func timeString(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func pageParams(pageS, sizeS string) (int, int) {
	page, _ := strconv.Atoi(pageS)
	size, _ := strconv.Atoi(sizeS)
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func whereClause(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(parts, " AND ")
}

func fileOrder(sort string) string {
	switch sort {
	case "uploaded_at":
		return "uploaded_at ASC, id ASC"
	case "original_name":
		return "original_name ASC, id ASC"
	case "size_bytes":
		return "size_bytes ASC, id ASC"
	default:
		return "uploaded_at DESC, id DESC"
	}
}

func normalizeIDs(ids []int64) ([]int64, bool) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, false
	}
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, false
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out, true
}

func in(v string, vals ...string) bool {
	for _, x := range vals {
		if v == x {
			return true
		}
	}
	return false
}

func nullableString(ns sql.NullString) any {
	if ns.Valid {
		return ns.String
	}
	return nil
}

func nullableInt(ni sql.NullInt64) any {
	if ni.Valid {
		return ni.Int64
	}
	return nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIntValue(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}
