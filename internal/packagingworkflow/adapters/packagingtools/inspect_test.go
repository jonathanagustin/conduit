package packagingtools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInspectImageReadsConfigWithoutEnvValues(t *testing.T) {
	config := map[string]any{
		"config": map[string]any{
			"User":         "65532",
			"ExposedPorts": map[string]any{"8080/tcp": map[string]any{}},
			"Env":          []string{"PATH=/usr/bin", "HOME=/home/nonroot"},
			"Entrypoint":   []string{"/server"},
			"Cmd":          []string{"--listen", ":8080"},
			"Labels":       map[string]string{"app": "static-web"},
		},
	}
	configBody, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(configBody)
	configDigest := "sha256:" + hex.EncodeToString(sum[:])
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config":        map[string]any{"digest": configDigest, "size": len(configBody)},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestSum := sha256.Sum256(manifest)
	manifestDigest := "sha256:" + hex.EncodeToString(manifestSum[:])
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/v2/apps/static-web/manifests/"+manifestDigest || strings.HasSuffix(request.URL.Path, "/manifests/1.0.0"):
			writer.Header().Set("Docker-Content-Digest", manifestDigest)
			writer.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			_, _ = writer.Write(manifest)
		case request.URL.Path == "/v2/apps/static-web/blobs/"+configDigest:
			writer.Header().Set("Content-Type", "application/vnd.oci.image.config.v1+json")
			_, _ = writer.Write(configBody)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	ref := fmt.Sprintf("%s/apps/static-web:1.0.0@%s", host, manifestDigest)
	got, err := New(WithRegistryBaseURL(server.URL)).InspectImage(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.User != "65532" || strings.Join(got.ExposedPorts, ",") != "8080/tcp" {
		t.Fatalf("inspect = %#v", got)
	}
	if strings.Join(got.Env, ",") != "PATH,HOME" || strings.Contains(strings.Join(got.Env, ","), "/usr") {
		t.Fatalf("env values leaked: %#v", got.Env)
	}
	if strings.Join(got.Entrypoint, " ") != "/server" || got.Labels["app"] != "static-web" {
		t.Fatalf("inspect = %#v", got)
	}
}

func TestParseImageRefRejectsABadDigest(t *testing.T) {
	if _, err := parseImageRef("registry.example/apps/web@sha256:not-a-digest"); err == nil {
		t.Fatal("accepted a bad digest")
	}
}

func TestInspectImageVerifiesRegistryContent(t *testing.T) {
	digest := func(raw []byte) string { sum := sha256.Sum256(raw); return fmt.Sprintf("sha256:%x", sum) }
	config := []byte(`{"config":{"User":"65532"}}`)
	configDigest := digest(config)
	manifest := []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"digest":%q}}`, configDigest))
	manifestDigest := digest(manifest)
	index := []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"digest":%q}]}`, manifestDigest))
	indexDigest := digest(index)
	other := "sha256:" + strings.Repeat("0", 64)
	for _, tc := range []struct {
		name                                             string
		indexed, tagged, headers                         bool
		rootBody, childBody, configBody                  []byte
		rootHeader, childHeader, configHeader, requested string
		wantErr                                          string
	}{
		{name: "pinned manifest without header"},
		{name: "tag without header", tagged: true},
		{name: "matching headers", headers: true},
		{name: "pinned index without headers", indexed: true},
		{name: "matching index headers", indexed: true, headers: true},
		{name: "false manifest header", rootHeader: other, wantErr: "digest header"},
		{name: "different pinned bytes", rootBody: append(append([]byte{}, manifest...), '\n'), wantErr: "requested content"},
		{name: "wrong requested digest", requested: other, wantErr: "requested content"},
		{name: "tampered index", indexed: true, rootBody: append(append([]byte{}, index...), '\n'), wantErr: "requested content"},
		{name: "tampered child", indexed: true, childBody: append(append([]byte{}, manifest...), '\n'), wantErr: "child image manifest"},
		{name: "false child header", indexed: true, childHeader: other, wantErr: "child image manifest"},
		{name: "tampered config", configBody: []byte(`{"config":{"User":"0"}}`), wantErr: "verify image config"},
		{name: "false config header", configHeader: other, wantErr: "verify image config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, expected := manifest, manifestDigest
			if tc.indexed {
				root, expected = index, indexDigest
			}
			if tc.requested != "" {
				expected = tc.requested
			}
			if tc.rootBody != nil {
				root = tc.rootBody
			}
			child, blob := manifest, config
			if tc.childBody != nil {
				child = tc.childBody
			}
			if tc.configBody != nil {
				blob = tc.configBody
			}
			rootHeader, childHeader, configHeader := tc.rootHeader, tc.childHeader, tc.configHeader
			if tc.headers {
				rootHeader = expected
				childHeader = manifestDigest
				configHeader = configDigest
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/apps/web/manifests/" + expected, "/v2/apps/web/manifests/latest":
					if rootHeader != "" {
						w.Header().Set("Docker-Content-Digest", rootHeader)
					}
					_, _ = w.Write(root)
				case "/v2/apps/web/manifests/" + manifestDigest:
					if childHeader != "" {
						w.Header().Set("Docker-Content-Digest", childHeader)
					}
					_, _ = w.Write(child)
				case "/v2/apps/web/blobs/" + configDigest:
					if configHeader != "" {
						w.Header().Set("Docker-Content-Digest", configHeader)
					}
					_, _ = w.Write(blob)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			ref := strings.TrimPrefix(server.URL, "http://") + "/apps/web@" + expected
			if tc.tagged {
				ref = strings.TrimPrefix(server.URL, "http://") + "/apps/web:latest"
			}
			got, err := New(WithRegistryBaseURL(server.URL)).InspectImage(t.Context(), ref)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %#v, %v; want %s", got, err, tc.wantErr)
				}
				if got.Digest != "" || got.User != "" {
					t.Fatalf("failed inspection returned facts: %#v", got)
				}
				return
			}
			if err != nil || got.Digest != expected || got.User != "65532" {
				t.Fatalf("got %#v, %v; expected manifest digest %s", got, err, expected)
			}
		})
	}
}
