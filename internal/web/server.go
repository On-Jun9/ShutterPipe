package web

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

type Server struct {
	router  *mux.Router
	hub     *Hub
	version string
}

func NewServer() *Server {
	s := &Server{
		router:  mux.NewRouter(),
		hub:     NewHub(),
		version: "unknown",
	}

	go s.hub.Run()

	s.setupRoutes()
	return s
}

func (s *Server) SetVersion(v string) {
	s.version = v
}

func (s *Server) setupRoutes() {
	api := s.router.PathPrefix("/api").Subrouter()
	api.HandleFunc("/version", s.handleVersion).Methods("GET")
	api.HandleFunc("/browse", s.handleBrowse).Methods("GET")
	api.HandleFunc("/config", s.handleGetConfig).Methods("GET")
	api.HandleFunc("/config", s.handleSaveConfig).Methods("POST")
	api.HandleFunc("/run", s.handleRun).Methods("POST")
	api.HandleFunc("/ws", s.handleWebSocket)

	// Preset routes
	api.HandleFunc("/presets", s.handleListPresets).Methods("GET")
	api.HandleFunc("/presets", s.handleSavePreset).Methods("POST")
	api.HandleFunc("/presets/load", s.handleLoadPreset).Methods("GET")
	api.HandleFunc("/presets/delete", s.handleDeletePreset).Methods("DELETE")

	// UserData routes (settings, bookmarks, path history)
	api.HandleFunc("/settings", s.handleGetSettings).Methods("GET")
	api.HandleFunc("/settings", s.handleSaveSettings).Methods("POST")
	api.HandleFunc("/bookmarks", s.handleGetBookmarks).Methods("GET")
	api.HandleFunc("/bookmarks", s.handleSaveBookmarks).Methods("POST")
	api.HandleFunc("/path-history", s.handleGetPathHistory).Methods("GET")
	api.HandleFunc("/path-history", s.handleSavePathHistory).Methods("POST")

	s.router.PathPrefix("/").Handler(http.FileServer(http.Dir("web/static")))
}

func (s *Server) Start(addr string) error {
	fmt.Printf("Starting ShutterPipe Web UI at http://%s\n", addr)
	return http.ListenAndServe(addr, s.router)
}
