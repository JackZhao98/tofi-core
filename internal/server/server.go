package server

import (
	"fmt"
	"log"
	"net/http"
	"time"
	"tofi-core/internal/storage"
)

type Config struct {
	Port    int
	HomeDir string
}

type Server struct {
	config   Config
	registry *ExecutionRegistry
	db       *storage.DB
}

func NewServer(config Config) (*Server, error) {
	db, err := storage.InitDB(config.HomeDir)
	if err != nil {
		return nil, err
	}

	return &Server{
		config:   config,
		registry: NewExecutionRegistry(),
		db:       db,
	}, nil
}

func (s *Server) Start() error {
	defer s.db.Close()
	mux := http.NewServeMux()

	// 注册路由
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /api/v1/run", s.handleRunWorkflow)
	mux.HandleFunc("GET /api/v1/executions/{id}", s.handleGetExecution)
	mux.HandleFunc("GET /api/v1/executions/{id}/logs", s.handleGetExecutionLogs)
	mux.HandleFunc("GET /api/v1/executions/{id}/artifacts", s.handleListArtifacts)
	mux.HandleFunc("GET /api/v1/executions/{id}/artifacts/{filename}", s.handleDownloadArtifact)

	// 配置 Server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	log.Printf("🚀 Tofi Server listening on port %d", s.config.Port)
	return srv.ListenAndServe()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
