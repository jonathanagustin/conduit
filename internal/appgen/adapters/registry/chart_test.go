package registry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPushChartUploadsTheArchiveAndManifest(t *testing.T) {
	var sawManifest bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/blobs/uploads/"):
			writer.Header().Set("Location", "/v2/hello-http/blobs/uploads/1")
			writer.WriteHeader(http.StatusAccepted)
		case request.Method == http.MethodPut && strings.Contains(request.URL.Path, "/blobs/uploads/"):
			if !strings.Contains(request.URL.RawQuery, "digest=sha256:") {
				t.Errorf("blob query = %s", request.URL.RawQuery)
			}
			writer.WriteHeader(http.StatusCreated)
		case request.Method == http.MethodPut && strings.Contains(request.URL.Path, "/manifests/"):
			body, _ := io.ReadAll(request.Body)
			if !strings.Contains(string(body), "application/vnd.cncf.helm.chart.content.v1.tar+gzip") {
				t.Errorf("manifest = %s", body)
			}
			sawManifest = true
			writer.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	digest, err := PushChart(context.Background(), server.URL, "hello-http", []byte("name: hello-http\n"))
	if err != nil || !strings.HasPrefix(digest, "sha256:") || !sawManifest {
		t.Fatalf("digest = %s err %v saw %v", digest, err, sawManifest)
	}
}

func TestDockerPublisherPushesTheLocalImage(t *testing.T) {
	if os.Getenv("CONDUIT_APPGEN_DOCKER") != "1" {
		t.Skip("set CONDUIT_APPGEN_DOCKER=1 to push to the local registry")
	}
	ref, image, chart, err := (DockerPublisher{Registry: "127.0.0.1:5000"}).Push(
		context.Background(), "appgen-hello-http:local", "hello-http", []byte("name: hello-http\nport: 8080\n"),
	)
	if err != nil || !strings.HasPrefix(image, "sha256:") || !strings.HasPrefix(chart, "sha256:") || !strings.Contains(ref, "hello-http:0.1.0") {
		t.Fatalf("ref=%s image=%s chart=%s err=%v", ref, image, chart, err)
	}
}
