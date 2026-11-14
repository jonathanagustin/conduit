// Package consoleui serves the immutable operator-console assets embedded in
// the Hub binary. It is an inbound HTTP adapter; control-plane domain packages
// do not import it.
package consoleui

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed dist
var embedded embed.FS

//go:embed placeholder.html
var placeholder []byte

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
func Handler(config ...Bootstrap) http.Handler {
	if len(config) > 1 {
		panic("console handler accepts at most one bootstrap")
	}
	bootstrap := DefaultBootstrap()
	if len(config) == 1 {
		bootstrap = config[0]
	}
	if err := bootstrap.Validate(); err != nil {
		panic("invalid console bootstrap: " + err.Error())
	}
	bootstrapJSON, err := json.Marshal(bootstrap)
	if err != nil {
		panic("encode console bootstrap: " + err.Error())
	}
	return handler(assets, bootstrapJSON)
}

// handler serves assets from one filesystem. index.html is the built console
// when the build has run. When it is absent, requests for the shell receive
// placeholder.html instead of a failed process.
func handler(assets fs.FS, bootstrapJSON []byte) http.Handler {
	shell, err := hydratedShell(assets, bootstrapJSON)
	if err != nil {
		panic("hydrate console shell: " + err.Error())
	}

	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		name, ok := assetName(request.URL.Path)
		if !ok {
			http.NotFound(w, request)
			return
		}
		contents, err := fs.ReadFile(assets, name)
		if err != nil && (name == "index.html" || shouldUseApplicationShell(name)) {
			name = "index.html"
			contents, err = fs.ReadFile(assets, name)
			if errors.Is(err, fs.ErrNotExist) {
				contents = shell
				err = nil
			}
		}
		if err == nil && name == "index.html" {
			contents = shell
		}
		if err != nil {
			http.NotFound(w, request)
			return
		}

		setConsoleHeaders(w, name, contents)
		http.ServeContent(w, request, name, time.Time{}, bytes.NewReader(contents))
	})
}

// bootstrapMarker is the placeholder web/index.html carries. It is a comment so
// that the built bundle is valid HTML on its own and so a document served by
// anything other than the Hub arrives unhydrated -- which the console treats as
// a refusal rather than rendering with invented branding.
const bootstrapMarker = "<!--conduit-console-bootstrap-->"

func productionConsoleEmbedded() bool {
	_, err := fs.Stat(assets, "index.html")
	return err == nil
}

// hydratedShell renders the tenant bootstrap into the application shell, once,
// at handler construction.
//
// Once, not per request, and that is a decision rather than an optimization.
// The Hub serves one tenant, chosen from a file at startup by an operator whose
// change rolls the deployment; nothing about a request selects it. Rendering
// per request would invite a later change to key it on a header, and the
// console is mounted with no authentication at all, so that header would be an
// anonymous client choosing what identity the console asserts. Rendering once
// also keeps the ETag stable and avoids needing a Vary the handler does not
// emit.
//
// Escaped as an HTML attribute value rather than interpolated. The bootstrap
// validator rejects "<" and ">" but not the double quote, so a displayName of
// Alpha "Ops -- legal today -- would otherwise close the attribute early. This
// is a bug class the previous fetch-a-JSON-document design structurally could
// not have, and it is bought back here with escaping rather than with hope.
func hydratedShell(assets fs.FS, bootstrapJSON []byte) ([]byte, error) {
	shell, err := fs.ReadFile(assets, "index.html")
	if errors.Is(err, fs.ErrNotExist) {
		shell = placeholder
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return hydrateShellDocument(shell, bootstrapJSON)
}

// hydrateShellDocument is separated from reading the embedded asset so the
// refusal can be tested against a document that has no marker, without
// disturbing the real shell that verify-console-assets.sh pins byte-for-byte.
func hydrateShellDocument(shell, bootstrapJSON []byte) ([]byte, error) {
	if !bytes.Contains(shell, []byte(bootstrapMarker)) {
		// A build that dropped the marker would otherwise serve a shell the
		// console refuses, which reads as a broken deployment rather than a
		// broken build. Fail where the cause is.
		return nil, errors.New("the built console shell carries no " + bootstrapMarker + " marker")
	}
	meta := `<meta name="` + BootstrapMetaName + `" content="` +
		html.EscapeString(string(bootstrapJSON)) + `" />`
	return bytes.Replace(shell, []byte(bootstrapMarker), []byte(meta), 1), nil
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
