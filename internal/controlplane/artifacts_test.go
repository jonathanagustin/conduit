package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const helloDigest = "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

func TestMemoryArtifactStoreIsContentAddressedAndIdempotent(t *testing.T) {
	repository := NewMemoryRepository()
	descriptor, created, err := repository.PutBlob(context.Background(), helloDigest, "text/plain", bytes.NewBufferString("hello"))
	if err != nil || !created {
		t.Fatalf("first put = %#v, %t, %v", descriptor, created, err)
	}
	if descriptor.Digest != helloDigest || descriptor.Size != 5 {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	_, created, err = repository.PutBlob(context.Background(), helloDigest, "text/plain", bytes.NewBufferString("hello"))
	if err != nil || created {
		t.Fatalf("idempotent put created=%t err=%v", created, err)
	}
	if _, _, err := repository.PutBlob(context.Background(), helloDigest, "text/plain", bytes.NewBufferString("changed")); err == nil || !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("digest mismatch = %v", err)
	}
}

func TestFileArtifactStorePersistsVerifiedBytes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := repository.PutBlob(context.Background(), "", "text/plain", bytes.NewBufferString("hello")); err != nil || !created {
		t.Fatalf("put created=%t err=%v", created, err)
	}
	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, reader, err := reopened.OpenBlob(context.Background(), helloDigest)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(reader)
	if err != nil || string(contents) != "hello" || descriptor.Size != 5 {
		t.Fatalf("read descriptor=%#v contents=%q err=%v", descriptor, contents, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reopened.blobDataPath(helloDigest), []byte("HELLO"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, reader, err := reopened.OpenBlob(context.Background(), helloDigest); err == nil || !errors.Is(err, ErrArtifactCorrupt) {
		if reader != nil {
			_ = reader.Close()
		}
		t.Fatalf("corrupt artifact open = %v", err)
	}
}

func TestReleaseContentDigestCommitsOnlyImmutableReleaseContent(t *testing.T) {
	release := Release{
		Product: "com.example:service", Version: "1.2.3",
		Chart:    Artifact{Repository: "registry.example/charts/service", Tag: "1.2.3", Digest: helloDigest},
		Images:   []Artifact{{Repository: "registry.example/images/service", Tag: "1.2.3", Digest: helloDigest}},
		Channels: []string{"RELEASE"},
	}
	first, err := releaseContentDigest(release)
	if err != nil || !digestPattern.MatchString(first) {
		t.Fatalf("release digest = %q, %v", first, err)
	}
	release.Channels = []string{"STABLE"}
	release.VulnerabilityAssessment = &VulnerabilityAssessment{Provider: "scanner", Outcome: "PASS"}
	second, err := releaseContentDigest(release)
	if err != nil || second != first {
		t.Fatalf("mutable evidence changed release digest: %s -> %s (%v)", first, second, err)
	}
	release.Chart.Digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	third, err := releaseContentDigest(release)
	if err != nil || third == first {
		t.Fatalf("artifact mutation did not change release digest: %s (%v)", third, err)
	}
}

func TestArtifactHTTPUploadVerifyAndRead(t *testing.T) {
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{
		AllowInsecureAgentIdentityHeaders: true, AllowInsecureOperatorIdentityHeaders: true,
	})
	upload := httptest.NewRequest(http.MethodPut, "/v1/artifacts/blobs/"+helloDigest, bytes.NewBufferString("hello"))
	upload.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, upload)
	if response.Code != http.StatusCreated || response.Header().Get("Docker-Content-Digest") != helloDigest {
		t.Fatalf("upload status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	var descriptor BlobDescriptor
	if err := json.Unmarshal(response.Body.Bytes(), &descriptor); err != nil || descriptor.Digest != helloDigest {
		t.Fatalf("descriptor=%#v err=%v", descriptor, err)
	}

	get := httptest.NewRequest(http.MethodGet, "/v1/artifacts/blobs/"+helloDigest, nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, get)
	if response.Code != http.StatusOK || response.Body.String() != "hello" ||
		response.Header().Get("Cache-Control") != "private, immutable" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("get status=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}

	bad := httptest.NewRequest(http.MethodPut, "/v1/artifacts/blobs/"+helloDigest, bytes.NewBufferString("not hello"))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, bad)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mismatch status=%d body=%s", response.Code, response.Body.String())
	}
}
