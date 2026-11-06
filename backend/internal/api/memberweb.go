package api

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"net/http"
)

// The member web app: installable from the browser on Android and iPhone ("Add to Home screen").

//go:embed webapp
var webappFiles embed.FS

var memberAppHTML = mustRead("webapp/index.html")

func mustRead(name string) []byte {
	b, err := webappFiles.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return b
}

func (s *Server) memberAppRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /app/{$}", serveMemberApp)
	static := map[string]string{
		"/app/manifest.webmanifest":  "application/manifest+json",
		"/app/sw.js":                 "text/javascript; charset=utf-8",
		"/app/icon-192.png":          "image/png",
		"/app/icon-512.png":          "image/png",
		"/app/icon-maskable-512.png": "image/png",
		"/app/apple-touch-icon.png":  "image/png",
	}
	for path, ctype := range static {
		data := mustRead("webapp/" + path[len("/app/"):])
		ct := ctype
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			if ct == "text/javascript; charset=utf-8" {
				w.Header().Set("Service-Worker-Allowed", "/app/")
			}
			_, _ = w.Write(data)
		})
	}
}

func serveMemberApp(w http.ResponseWriter, r *http.Request) {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	nonce := base64.StdEncoding.EncodeToString(b)
	page := bytes.ReplaceAll(memberAppHTML, []byte("{{NONCE}}"), []byte(nonce))
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy",
		"default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+"' https://fonts.googleapis.com; "+
			"font-src https://fonts.gstatic.com; connect-src 'self'; img-src 'self' data:; manifest-src 'self'; worker-src 'self'; "+
			"base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(page)
}
