package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"auto_wechat_article_publisher/generator"
	"auto_wechat_article_publisher/publisher"
)

//go:embed web/dist web/dist/* web/dist/assets/*
var embeddedStatic embed.FS

type Server struct {
	genAgent  *generator.Agent
	pubCfg    publisher.Config
	pub       *publisher.Publisher
	pubMu     sync.Mutex
	store     *sessionStore
	staticFS  http.Handler
	uploadDir string
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*sessionEntry
	ttl      time.Duration
	ticker   *time.Ticker
	done     chan struct{}
}

type sessionEntry struct {
	sess      *generator.Session
	expiresAt time.Time
	uploads   []string
}

func newStore() *sessionStore {
	return &sessionStore{
		sessions: make(map[string]*sessionEntry),
		ttl:      5 * time.Minute,
		done:     make(chan struct{}),
	}
}

// startJanitor launches a background goroutine to purge expired sessions periodically.
// Caller should ensure this is called once.
func (s *sessionStore) startJanitor(interval time.Duration) {
	s.ticker = time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-s.ticker.C:
				s.purgeExpired()
			case <-s.done:
				return
			}
		}
	}()
}

func (s *sessionStore) stopJanitor() {
	if s.ticker != nil {
		s.ticker.Stop()
	}
	close(s.done)
}

func (s *sessionStore) set(id string, sess *generator.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = &sessionEntry{sess: sess, expiresAt: time.Now().Add(s.ttl)}
}

func (s *sessionStore) get(id string) (*generator.Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	entry, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	entry.expiresAt = time.Now().Add(s.ttl) // extend on access
	return entry.sess, true
}

func (s *sessionStore) heartbeat(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	entry, ok := s.sessions[id]
	if !ok {
		return false
	}
	entry.expiresAt = time.Now().Add(s.ttl)
	return true
}

func (s *sessionStore) addUpload(id, path string) {
	if id == "" || path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.sessions[id]
	if !ok {
		return
	}
	entry.uploads = append(entry.uploads, path)
}

func (s *sessionStore) delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteLocked(id)
}

func (s *sessionStore) purgeExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
}

func (s *sessionStore) purgeLocked() {
	now := time.Now()
	for id, entry := range s.sessions {
		if entry.expiresAt.Before(now) {
			s.cleanupUploads(entry.uploads)
			delete(s.sessions, id)
		}
	}
}

func (s *sessionStore) deleteLocked(id string) {
	entry, ok := s.sessions[id]
	if !ok {
		return
	}
	s.cleanupUploads(entry.uploads)
	delete(s.sessions, id)
}

func (s *sessionStore) cleanupUploads(paths []string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

func New(genAgent *generator.Agent, pubCfg publisher.Config) (*Server, error) {
	if genAgent == nil {
		return nil, errors.New("generator agent required")
	}

	store := newStore()
	store.startJanitor(1 * time.Minute)

	uploadDir := "uploads"
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	cleanupUploadsAll(uploadDir)
	cleanupTempDrafts(24 * time.Hour)

	sub, err := fs.Sub(embeddedStatic, "web/dist")
	if err != nil {
		return nil, err
	}

	return &Server{
		genAgent:  genAgent,
		pubCfg:    pubCfg,
		pub:       nil,
		store:     store,
		staticFS:  http.FileServer(http.FS(sub)),
		uploadDir: uploadDir,
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", s.handleSessionCreate)
	mux.HandleFunc("/api/sessions/", s.handleSessionByID)
	mux.HandleFunc("/api/heartbeat/", s.handleHeartbeat)
	mux.HandleFunc("/api/publish", s.handlePublish)
	mux.HandleFunc("/api/uploads", s.handleUpload)
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.uploadDir))))
	mux.Handle("/", s.staticHandler())
	return corsMiddleware(logMiddleware(mux))
}

func (s *Server) staticHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// fall back to index.html for SPA-ish behavior
		upath := r.URL.Path
		if upath == "/" || !strings.HasPrefix(upath, "/api/") {
			p := upath
			if p == "/" {
				p = "/index.html"
			}
			r.URL.Path = p
			s.staticFS.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}

// --- Handlers ---

type sessionCreateReq struct {
	Topic       string   `json:"topic"`
	Outline     []string `json:"outline"`
	Words       int      `json:"words"`
	Constraints []string `json:"constraints"`
	Style       string   `json:"style"`
}

type sessionResp struct {
	SessionID string           `json:"session_id"`
	Draft     generator.Draft  `json:"draft"`
	History   []generator.Turn `json:"history"`
}

type reviseReq struct {
	Comment string `json:"comment"`
}

type publishReq struct {
	SessionID string `json:"session_id"`
	CoverPath string `json:"cover_path,omitempty"`
	Author    string `json:"author,omitempty"`
	Title     string `json:"title,omitempty"`
	Digest    string `json:"digest,omitempty"`
}

type publishResp struct {
	MediaID   string `json:"media_id"`
	Title     string `json:"title"`
	CoverPath string `json:"cover_path"`
}

func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req sessionCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	spec := generator.Spec{
		Topic:       req.Topic,
		Outline:     req.Outline,
		Words:       req.Words,
		Constraints: req.Constraints,
		Style:       req.Style,
	}
	id := newSessionID()
	sess := generator.NewSession(id, spec, s.genAgent)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	draft, err := sess.Propose(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.store.set(id, sess)
	writeJSON(w, sessionResp{SessionID: id, Draft: draft, History: sess.History})
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		sess, ok := s.store.get(id)
		if !ok {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		writeJSON(w, sessionResp{SessionID: id, Draft: sess.Draft, History: sess.History})
	case http.MethodPost:
		sess, ok := s.store.get(id)
		if !ok {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		var req reviseReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		draft, err := sess.Revise(ctx, req.Comment)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, sessionResp{SessionID: id, Draft: draft, History: sess.History})
	case http.MethodDelete:
		s.store.delete(id)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleHeartbeat extends a session's TTL; if not found returns 404.
// Path: /api/heartbeat/{id}
func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/heartbeat/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	if ok := s.store.heartbeat(id); !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req publishReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, "session_id required", http.StatusBadRequest)
		return
	}
	sess, ok := s.store.get(req.SessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if sess.Draft.Markdown == "" {
		http.Error(w, "draft is empty; generate first", http.StatusBadRequest)
		return
	}

	// Resolve cover path (required by WeChat). Use provided path or fallback to samples/cover.jpg if exists.
	coverPath := strings.TrimSpace(req.CoverPath)
	if coverPath == "" {
		if _, err := os.Stat("samples/cover.jpg"); err == nil {
			coverPath = "samples/cover.jpg"
		} else {
			http.Error(w, "cover_path required", http.StatusBadRequest)
			return
		}
	}
	if _, err := os.Stat(coverPath); err != nil {
		http.Error(w, "cover_path not found: "+err.Error(), http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		if sess.Draft.Title != "" {
			title = sess.Draft.Title
		} else {
			title = sess.Spec.Topic
		}
	}
	digest := strings.TrimSpace(req.Digest)
	if digest == "" {
		digest = sess.Draft.Digest
	}

	tmp, err := os.CreateTemp("", "draft-*.md")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(sess.Draft.Markdown); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = tmp.Close()

	pub, err := s.ensurePublisher()
	if err != nil {
		http.Error(w, "publisher init failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	mediaID, err := pub.PublishDraft(ctx, publisher.PublishParams{
		MarkdownPath: tmp.Name(),
		Title:        title,
		CoverPath:    coverPath,
		Author:       req.Author,
		Digest:       digest,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, publishResp{MediaID: mediaID, Title: title, CoverPath: coverPath})
}

// --- Helpers ---

func newSessionID() string {
	return strings.ReplaceAll(time.Now().Format("20060102T150405.000000000"), ".", "")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		path := r.URL.Path
		if path == "" {
			path = "/"
		}
		if strings.HasPrefix(path, "/api/heartbeat/") && rec.status == http.StatusNoContent {
			// 正常心跳不打印，避免刷日志
			return
		}
		log.Printf("[HTTP] %s %s -> %d (%dB) in %v", r.Method, path, rec.status, rec.bytes, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

// corsMiddleware enables simple CORS for API routes so browsers can preflight POST safely.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (s *Server) ensurePublisher() (*publisher.Publisher, error) {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()
	if s.pub != nil {
		return s.pub, nil
	}
	p, err := publisher.New(s.pubCfg, nil, false, log.Default())
	if err != nil {
		return nil, err
	}
	s.pub = p
	return s.pub, nil
}

type uploadResp struct {
	Path     string `json:"path"`
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Usage    string `json:"usage,omitempty"`
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(25 << 20); err != nil { // 25 MB
		http.Error(w, "parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Require a live session so uploaded files can be tracked and cleaned up with the session.
	sessID := strings.TrimSpace(r.FormValue("session_id"))
	if sessID == "" {
		http.Error(w, "session_id required; generate draft first", http.StatusBadRequest)
		return
	}
	if _, ok := s.store.get(sessID); !ok {
		http.Error(w, "session not found or expired; regenerate draft", http.StatusNotFound)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	usage := strings.TrimSpace(r.FormValue("usage"))
	orig := sanitizeFilename(header.Filename)
	if orig == "" {
		orig = "upload"
	}
	ext := filepath.Ext(orig)
	base := strings.TrimSuffix(orig, ext)
	if base == "" {
		base = "upload"
	}
	filename := fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext)
	path := filepath.Join(s.uploadDir, filename)

	dst, err := os.Create(path)
	if err != nil {
		http.Error(w, "save file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	n, err := io.Copy(dst, file)
	if err != nil {
		http.Error(w, "write file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, uploadResp{
		Path:     path,
		URL:      "/uploads/" + filename,
		Filename: header.Filename,
		Size:     n,
		Usage:    usage,
	})

	if sessID != "" {
		s.store.addUpload(sessID, path)
	}
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "..", "")
	return name
}

// cleanupUploadsOlderThan removes files in dir older than maxAge; best-effort.
func cleanupUploadsAll(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("[cleanup] read uploads dir failed: %v", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fp := filepath.Join(dir, e.Name())
		if err := os.Remove(fp); err == nil {
			log.Printf("[cleanup] removed upload %s", fp)
		}
	}
}

func cleanupUploadsOlderThan(dir string, maxAge time.Duration) { // nolint:deadcode
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("[cleanup] read uploads dir failed: %v", err)
		return
	}
	threshold := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(threshold) {
			fp := filepath.Join(dir, e.Name())
			if err := os.Remove(fp); err == nil {
				log.Printf("[cleanup] removed stale upload %s", fp)
			}
		}
	}
}

// cleanupTempDrafts removes old draft-*.md files from system temp dir.
func cleanupTempDrafts(maxAge time.Duration) {
	pattern := filepath.Join(os.TempDir(), "draft-*.md")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		log.Printf("[cleanup] glob temp drafts failed: %v", err)
		return
	}
	threshold := time.Now().Add(-maxAge)
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.ModTime().Before(threshold) {
			if err := os.Remove(p); err == nil {
				log.Printf("[cleanup] removed stale temp draft %s", p)
			}
		}
	}
}
