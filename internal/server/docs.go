package server

import (
	"embed"
	"net/http"
)

//go:embed docs/*
var docsFS embed.FS

// registerDocsRoutes serves the API documentation site at /api/docs.
func (s *Server) registerDocsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/docs", func(w http.ResponseWriter, r *http.Request) {
		data, err := docsFS.ReadFile("docs/index.html")
		if err != nil {
			http.Error(w, "docs not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	mux.HandleFunc("GET /api/docs/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		data, err := docsFS.ReadFile("docs/openapi.yaml")
		if err != nil {
			http.Error(w, "spec not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(data)
	})
}
