package main

import (
	"embed"
	"net/http"
)

//go:embed web/index.html web/assets/microdevicestatus-logo.png
var webFS embed.FS

func (s *server) dashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page, err := webFS.ReadFile("web/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "dashboard unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(page)
}

func (s *server) brandLogo(w http.ResponseWriter, r *http.Request) {
	logo, err := webFS.ReadFile("web/assets/microdevicestatus-logo.png")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "logo unavailable")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(logo)
}
