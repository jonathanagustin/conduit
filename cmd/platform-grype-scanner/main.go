package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/grypescanner"
)

const maximumRequestSize = 1 << 20

func main() {
	if err := run(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, input io.Reader, output io.Writer) error {
	payload, err := io.ReadAll(io.LimitReader(input, maximumRequestSize+1))
	if err != nil {
		return fmt.Errorf("read artifact scan request: %w", err)
	}
	if len(payload) > maximumRequestSize {
		return errors.New("artifact scan request exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request controlplane.ArtifactScanRequest
	if err := decoder.Decode(&request); err != nil {
		return fmt.Errorf("decode artifact scan request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("artifact scan request must contain exactly one JSON value")
	}
	runner, err := grypescanner.NewExecRunner(os.Getenv("GRYPE_BIN"), os.Getenv("GRYPE_CONFIG_FILE"))
	if err != nil {
		return err
	}
	timeout := 10 * time.Minute
	if raw := os.Getenv("GRYPE_SCAN_TIMEOUT"); raw != "" {
		timeout, err = time.ParseDuration(raw)
		if err != nil || timeout < time.Second || timeout > 30*time.Minute {
			return errors.New("GRYPE_SCAN_TIMEOUT must be between 1 second and 30 minutes")
		}
	}
	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := (grypescanner.Scanner{Runner: runner}).Scan(scanCtx, request)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(result)
}
