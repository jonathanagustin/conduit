// Package consoleui serves the immutable operator-console assets embedded in
// the Hub binary. It is an inbound HTTP adapter; control-plane domain packages
// do not import it.
package consoleui

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed dist
var embedded embed.FS

var assets = mustAssets()

func mustAssets() fs.FS {
	assets, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("open embedded console assets: " + err.Error())
	}
	return assets
}

// Handler returns the read-only console asset handler. The Hub mounts it below
// /console/; API requests remain on the separately authorized /v1 routes.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		name, ok := assetName(request.URL.Path)
		if !ok {
			http.NotFound(w, request)
			return
		}
		contents, err := fs.ReadFile(assets, name)
		if err != nil && shouldUseApplicationShell(name) {
			name = "index.html"
			contents, err = fs.ReadFile(assets, name)
		}
		if err != nil {
			http.NotFound(w, request)
			return
		}

		setConsoleHeaders(w, name, contents)
		http.ServeContent(w, request, name, time.Time{}, bytes.NewReader(contents))
	})
}

func assetName(requestPath string) (string, bool) {
	if !strings.HasPrefix(requestPath, "/console/") {
		return "", false
	}
	name := strings.TrimPrefix(requestPath, "/console/")
	if name == "" {
		return "index.html", true
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != name {
		return "", false
	}
	return clean, true
}

func shouldUseApplicationShell(name string) bool {
	return !strings.HasPrefix(name, "assets/") && path.Ext(name) == ""
}

func setConsoleHeaders(w http.ResponseWriter, name string, contents []byte) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src-elem 'self'; style-src-attr 'unsafe-inline'; font-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	digest := sha256.Sum256(contents)
	w.Header().Set("ETag", `"sha256-`+hex.EncodeToString(digest[:])+`"`)
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
}
