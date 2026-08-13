package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

func main() {
	directory := flag.String("directory", "examples/modules", "directory containing Module YAML definitions")
	hubURL := flag.String("hub-url", os.Getenv("PLATFORM_DELIVERY_URL"), "delivery Hub base URL")
	dryRun := flag.Bool("dry-run", false, "validate and print the publication request without sending it")
	flag.Parse()

	definitions, err := readDefinitions(*directory)
	if err != nil {
		fatal(err)
	}
	body, err := json.MarshalIndent(map[string]any{"definitions": definitions}, "", "  ")
	if err != nil {
		fatal(err)
	}
	if *dryRun {
		_, _ = os.Stdout.Write(append(body, '\n'))
		return
	}
	endpoint, err := releasesURL(*hubURL)
	if err != nil {
		fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	spiffeEndpoint := strings.TrimSpace(os.Getenv("SPIFFE_ENDPOINT_SOCKET"))
	if spiffeEndpoint != "" {
		expectedHubID := strings.TrimSpace(os.Getenv("CONTROL_PLANE_SPIFFE_ID"))
		if expectedHubID == "" {
			fatal(fmt.Errorf("CONTROL_PLANE_SPIFFE_ID is required with SPIFFE_ENDPOINT_SOCKET"))
		}
		source, sourceErr := workloadapi.NewX509Source(
			context.Background(),
			workloadapi.WithClientOptions(workloadapi.WithAddr(spiffeEndpoint)),
		)
		if sourceErr != nil {
			fatal(fmt.Errorf("connect to SPIFFE Workload API: %w", sourceErr))
		}
		defer source.Close()
		client, err = operator.NewSPIFFEHTTPClient(source, expectedHubID)
		if err != nil {
			fatal(fmt.Errorf("configure SPIFFE Hub client: %w", err))
		}
		client.Timeout = 30 * time.Second
	}
	response, err := client.Do(request)
	if err != nil {
		fatal(err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		fatal(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		fatal(fmt.Errorf("Hub returned %s: %s", response.Status, strings.TrimSpace(string(responseBody))))
	}
	_, _ = os.Stdout.Write(responseBody)
}

// releasesURL is the one place the Hub URL is interpreted. The Spoke agent and
// the operator adapter both refuse a base path, so a Hub reachable only under
// one cannot run either of them. Accepting a base path here would not misroute
// anything -- unlike those two, this joins rather than resolves -- but it would
// let an operator configure a Hub that publishes Modules and does nothing else,
// and discover the rest only when a Spoke fails to start.
func releasesURL(hubURL string) (string, error) {
	trimmed := strings.TrimSpace(hubURL)
	if trimmed == "" {
		return "", fmt.Errorf("--hub-url or PLATFORM_DELIVERY_URL is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse Hub URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("Hub URL must use http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("Hub URL must name a host")
	}
	if strings.Trim(parsed.Path, "/") != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("Hub URL must be a scheme and host without a path, query, or fragment")
	}
	parsed.Path = "/v1/modules/releases"
	return parsed.String(), nil
}

func readDefinitions(directory string) ([]controlplane.ModuleDefinition, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read Module directory: %w", err)
	}
	paths := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension == ".yaml" || extension == ".yml" {
			paths = append(paths, filepath.Join(directory, entry.Name()))
		}
	}
	sort.Strings(paths)
	definitions := make([]controlplane.ModuleDefinition, 0, len(paths))
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		definition, decodeErr := controlplane.DecodeModuleDefinition(file)
		closeErr := file.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("%s: %w", path, decodeErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if filepath.Base(path) != definition.Name+filepath.Ext(path) {
			return nil, fmt.Errorf("%s: filename must match Module name %s", path, definition.Name)
		}
		definitions = append(definitions, definition)
	}
	if len(definitions) == 0 {
		return nil, fmt.Errorf("no Module YAML definitions found in %s", directory)
	}
	return definitions, nil
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "platform-module-publisher:", err)
	os.Exit(1)
}
