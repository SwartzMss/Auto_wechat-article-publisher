package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"auto_wechat_article_publisher/generator"
	"auto_wechat_article_publisher/publisher"
)

//go:embed web/dist web/dist/* web/dist/assets/*
var embeddedStatic embed.FS

type Server struct {
	genAgent *generator.Agent
	pubCfg   publisher.Config
	pub      *publisher.Publisher
	pubMu    sync.Mutex
	store    *sessionStore
	staticFS http.Handler
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*generator.Session
}

func newStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]*generator.Session)}
}

func (s *sessionStore) set(id string, sess *generator.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = sess
}

func (s *sessionStore) get(id string) (*generator.Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func New(genAgent *generator.Agent, pubCfg publisher.Config) (*Server, error) {
	if genAgent == nil {
		return nil, errors.New("generator agent required")
	}

	sub, err := fs.Sub(embeddedStatic, "web/dist")
	if err != nil {
		return nil, err
	}

	return &Server{
		genAgent: genAgent,
		pubCfg:   pubCfg,
		pub:      nil,
		store:    newStore(),
		staticFS: http.FileServer(http.FS(sub)),
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", s.handleSessionCreate)
	mux.HandleFunc("/api/sessions/", s.handleSessionByID)
	mux.HandleFunc("/api/publish", s.handlePublish)
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
	Tone        string   `json:"tone"`
	Audience    string   `json:"audience"`
	Words       int      `json:"words"`
	Constraints []string `json:"constraints"`
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
		Tone:        req.Tone,
		Audience:    req.Audience,
		Words:       req.Words,
		Constraints: req.Constraints,
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
	sess, ok := s.store.get(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, sessionResp{SessionID: id, Draft: sess.Draft, History: sess.History})
	case http.MethodPost:
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
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
