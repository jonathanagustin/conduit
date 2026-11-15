//go:build linux

package filesystem

import (
	"context"
	"testing"
)

func TestFilesystemTelemetryAndMissingPaths(t *testing.T) {
	reader := Reader{Path: t.TempDir()}
	used, total, err := reader.Capacity(t.Context())
	if err != nil || total <= 0 || used < 0 || used > total {
		t.Fatalf("filesystem measurement: %d/%d %v", used, total, err)
	}
	if _, _, err = (Reader{Path: reader.Path + "/missing"}).Capacity(t.Context()); err == nil {
		t.Fatal("missing filesystem returned a measurement")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err = reader.Capacity(ctx); err == nil {
		t.Fatal("canceled measurement ignored cancellation")
	}
}
