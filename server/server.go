package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
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
		store:    newStore(),
		staticFS: http.FileServer(http.FS(sub)),
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", s.handleSessionCreate)
	mux.HandleFunc("/api/sessions/", s.handleSessionByID)
	mux.Handle("/", s.staticHandler())
	return logMiddleware(mux)
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
		next.ServeHTTP(w, r)
		// minimal log to stdout
		path := r.URL.Path
		if path == "" {
			path = "/"
		}
		// Use DefaultLogger
		_ = time.Since(start)
	})
}
