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
