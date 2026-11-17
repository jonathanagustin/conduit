// Package registry publishes a generated image and its chart to a local registry.
package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/domain"
)

// PushChart uploads one chart archive and returns its manifest digest.
func PushChart(ctx context.Context, registry, name string, values []byte) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(registry), "/")
	if base == "" {
		return "", fmt.Errorf("registry is not configured")
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	blob, err := chartArchive(name, "0.1.0", values)
	if err != nil {
		return "", err
	}
	config := []byte(fmt.Sprintf(`{"name":%q,"version":"0.1.0"}`, name))
	client := http.Client{Timeout: 20 * time.Second}
	blobDigest := digest(blob)
	configDigest := digest(config)
	if err := upload(ctx, client, base, name, blob, blobDigest); err != nil {
		return "", err
	}
	if err := upload(ctx, client, base, name, config, configDigest); err != nil {
		return "", err
	}
	manifest := map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{
			"mediaType": "application/vnd.cncf.helm.config.v1+json",
			"digest":    configDigest,
			"size":      len(config),
		},
		"layers": []map[string]any{{
			"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
			"digest":    blobDigest,
			"size":      len(blob),
		}},
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	manifestDigest := digest(body)
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, base+"/v2/"+name+"/manifests/0.1.0", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return "", fmt.Errorf("chart manifest was refused: %s", strings.TrimSpace(string(raw)))
	}
	return manifestDigest, nil
}

func upload(ctx context.Context, client http.Client, base, name string, payload []byte, digest string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v2/"+name+"/blobs/uploads/", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	location := response.Header.Get("Location")
	response.Body.Close()
	if response.StatusCode >= 300 || location == "" {
		return fmt.Errorf("chart upload was not accepted")
	}
	if strings.HasPrefix(location, "/") {
		location = base + location
	}
	joiner := "?"
	if strings.Contains(location, "?") {
		joiner = "&"
	}
	put, err := http.NewRequestWithContext(ctx, http.MethodPut, location+joiner+"digest="+digest, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	put.Header.Set("Content-Type", "application/octet-stream")
	putResponse, err := client.Do(put)
	if err != nil {
		return err
	}
	defer putResponse.Body.Close()
	if putResponse.StatusCode >= 300 {
		return fmt.Errorf("chart blob was refused")
	}
	return nil
}

func chartArchive(name, version string, values []byte) ([]byte, error) {
	chart, err := domain.ChartFiles(name, version, values)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressed)
	for _, file := range chart {
		header := &tar.Header{Name: name + "/" + file.Path, Mode: 0o644, Size: int64(len(file.Body))}
		if err := archive.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := archive.Write(file.Body); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	if err := compressed.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
