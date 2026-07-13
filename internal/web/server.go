package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/mux"
)

type Server struct {
	router         *mux.Router
	hub            *Hub
	version        string
	runManager     *RunManager
	runManagerOnce sync.Once
	runContextRoot context.Context
	cancelRuns     context.CancelFunc
	runContextOnce sync.Once
	httpServer     *http.Server
	httpServerMu   sync.Mutex
	runLifecycleMu sync.Mutex
	shuttingDown   bool
	serverID       string
	serverIDOnce   sync.Once
}

func NewServer() *Server {
	s := &Server{
		router:     mux.NewRouter(),
		hub:        NewHub(),
		version:    "unknown",
		runManager: NewRunManager(),
	}

	go s.hub.Run()

	s.setupRoutes()
	return s
}

func (s *Server) SetVersion(v string) {
	s.version = v
}

func (s *Server) setupRoutes() {
	s.router.Use(requireLoopbackHost)
	api := s.router.PathPrefix("/api").Subrouter()
	api.Use(requireSameOrigin)
	api.HandleFunc("/version", s.handleVersion).Methods("GET")
	api.HandleFunc("/browse", s.handleBrowse).Methods("GET")
	api.HandleFunc("/config", s.handleGetConfig).Methods("GET")
	api.HandleFunc("/config", s.handleSaveConfig).Methods("POST")
	api.HandleFunc("/run", s.handleRun).Methods("POST")
	api.HandleFunc("/run/cancel", s.handleCancelRun).Methods("POST")
	api.HandleFunc("/run/status", s.handleRunStatus).Methods("GET")
	api.HandleFunc("/ws", s.handleWebSocket)

	// Preset routes
	api.HandleFunc("/presets", s.handleListPresets).Methods("GET")
	api.HandleFunc("/presets", s.handleSavePreset).Methods("POST")
	api.HandleFunc("/presets/load", s.handleLoadPreset).Methods("GET")
	api.HandleFunc("/presets/delete", s.handleDeletePreset).Methods("DELETE")

	// UserData routes (settings, bookmarks, path history, backup history)
	api.HandleFunc("/settings", s.handleGetSettings).Methods("GET")
	api.HandleFunc("/settings", s.handleSaveSettings).Methods("POST")
	api.HandleFunc("/bookmarks", s.handleGetBookmarks).Methods("GET")
	api.HandleFunc("/bookmarks", s.handleSaveBookmarks).Methods("POST")
	api.HandleFunc("/path-history", s.handleGetPathHistory).Methods("GET")
	api.HandleFunc("/path-history", s.handleSavePathHistory).Methods("POST")
	api.HandleFunc("/backup-history", s.handleGetBackupHistory).Methods("GET")

	s.router.PathPrefix("/").Handler(http.FileServer(http.Dir("web/static")))
}

func (s *Server) Start(addr string) error {
	if err := validateListenAddress(addr); err != nil {
		return err
	}
	fmt.Printf("Starting ShutterPipe Web UI at http://%s\n", addr)
	httpServer := &http.Server{Addr: addr, Handler: s.router}
	runContext := s.runContext()
	s.runLifecycleMu.Lock()
	if s.shuttingDown || runContext.Err() != nil {
		s.runLifecycleMu.Unlock()
		return http.ErrServerClosed
	}
	s.httpServerMu.Lock()
	s.httpServer = httpServer
	s.httpServerMu.Unlock()
	s.runLifecycleMu.Unlock()
	return httpServer.ListenAndServe()
}

// Shutdown first cancels and waits for the active backup so its pipeline can
// persist cancellation history, then stops accepting HTTP connections.
func (s *Server) Shutdown(ctx context.Context) error {
	s.runContext()
	// Serialize the shutdown transition with server and run admission. Once this
	// flag is set, no listener or backup can start after the wait snapshot.
	s.runLifecycleMu.Lock()
	s.shuttingDown = true
	s.httpServerMu.Lock()
	httpServer := s.httpServer
	s.httpServerMu.Unlock()
	s.cancelRuns()
	s.runs().CancelActive()
	s.runLifecycleMu.Unlock()
	waitErr := s.runs().Wait(ctx)
	defer func() {
		if s.hub != nil {
			s.hub.Shutdown()
		}
	}()

	if httpServer == nil {
		return waitErr
	}
	if waitErr != nil {
		return errors.Join(waitErr, httpServer.Close())
	}
	if err := httpServer.Shutdown(ctx); err != nil {
		return errors.Join(err, httpServer.Close())
	}
	return nil
}

func (s *Server) startRun(runID string) (context.Context, context.CancelFunc, error) {
	s.runLifecycleMu.Lock()
	defer s.runLifecycleMu.Unlock()
	if s.shuttingDown {
		return nil, nil, http.ErrServerClosed
	}
	runCtx, cancel := context.WithCancel(s.runContext())
	if err := s.runs().TryStart(runID, cancel); err != nil {
		cancel()
		return nil, nil, err
	}
	return runCtx, cancel, nil
}

func (s *Server) isShuttingDown() bool {
	s.runLifecycleMu.Lock()
	defer s.runLifecycleMu.Unlock()
	return s.shuttingDown
}

func (s *Server) instanceID() string {
	s.serverIDOnce.Do(func() {
		if generated, err := newRunID(); err == nil {
			s.serverID = generated
		} else {
			s.serverID = fmt.Sprintf("process-%p", s)
		}
	})
	return s.serverID
}

func (s *Server) runs() *RunManager {
	s.runManagerOnce.Do(func() {
		if s.runManager == nil {
			s.runManager = NewRunManager()
		}
	})
	return s.runManager
}

func (s *Server) runContext() context.Context {
	s.runContextOnce.Do(func() {
		s.runContextRoot, s.cancelRuns = context.WithCancel(context.Background())
	})
	return s.runContextRoot
}

func validateListenAddress(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid server address %q: %w", addr, err)
	}
	if !isLoopbackHostname(host) {
		return fmt.Errorf("server address must use localhost or a loopback IP: %s", addr)
	}
	return nil
}

func requireLoopbackHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if parsedHost, _, err := net.SplitHostPort(r.Host); err == nil {
			host = parsedHost
		}
		if !isLoopbackHostname(host) {
			writeAPIError(w, http.StatusForbidden, "non-loopback host denied")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHostname(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
