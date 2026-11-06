package api

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"net/http"
)

//go:embed console/index.html
var consoleHTML []byte

// serveConsole returns the operations console with a fresh CSP nonce, so only
// the console's own script can run on the page.
func serveConsole(w http.ResponseWriter, r *http.Request) {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	nonce := base64.StdEncoding.EncodeToString(b)
	page := bytes.ReplaceAll(consoleHTML, []byte("{{NONCE}}"), []byte(nonce))

	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy",
		"default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+"' https://fonts.googleapis.com; "+
			"font-src https://fonts.gstatic.com; connect-src 'self'; img-src 'self' data:; "+
			"base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(page)
}
