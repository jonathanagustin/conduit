package conduitcli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func productTGZBundle() controlplane.HelmProductReleaseBundle {
	digest := "sha256:" + strings.Repeat("a", 64)
	return controlplane.HelmProductReleaseBundle{
		Manifest: controlplane.ApolloProductManifest{
			ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "application",
			ProductVersion: "2.0.0", ProductType: "helm-chart.v1",
			Extensions: controlplane.ApolloProductManifestExtensions{
				HelmChart: &controlplane.ApolloManifestHelmChart{Name: "application", Version: "7.4.1", RepositoryURL: "oci://registry.example/charts/application:7.4.1@" + digest},
				Artifacts: []controlplane.ApolloManifestArtifact{{Type: "oci", URI: "registry.example/application:2.0.0@" + digest}},
			},
		},
		DefaultConfig: []byte("replicaCount: 3\n"), ConfigSchema: []byte(`{"type":"object"}`),
	}
}

func TestHelmProductTGZRoundTripsDeterministically(t *testing.T) {
	t.Parallel()
	bundle := productTGZBundle()
	first, err := encodeHelmProductTGZ(bundle)
	if err != nil {
		t.Fatal(err)
	}
	second, err := encodeHelmProductTGZ(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("same semantic bundle produced different TGZ bytes")
	}
	archivePath := filepath.Join(t.TempDir(), "application-2.0.0.config.tgz")
	if err := os.WriteFile(archivePath, first, 0o600); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeHelmProductTGZ(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Manifest.ProductVersion != "2.0.0" || decoded.Manifest.Extensions.HelmChart.Version != "7.4.1" ||
		!bytes.Equal(decoded.DefaultConfig, bundle.DefaultConfig) || !bytes.Equal(decoded.ConfigSchema, bundle.ConfigSchema) {
		t.Fatalf("decoded=%#v", decoded)
	}

	service := &fakeService{}
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, service))
	root.SetArgs([]string{
		"--server", "http://127.0.0.1:8081", "--development-subject", "developer",
		"product-release", "create", "--product-tgz", archivePath,
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(service.helmBundle.DefaultConfig, bundle.DefaultConfig) {
		t.Fatalf("published bundle=%#v", service.helmBundle)
	}
}

type testTarEntry struct {
	name     string
	typeflag byte
	contents []byte
}

func writeTestTGZ(t *testing.T, entries []testTarEntry) string {
	t.Helper()
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Typeflag: entry.typeflag, Mode: 0o644, Size: int64(len(entry.contents))}
		if entry.typeflag == tar.TypeSymlink {
			header.Linkname = "manifest.yml"
			header.Size = 0
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.typeflag == tar.TypeReg {
			if _, err := tw.Write(entry.contents); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.tgz")
	if err := os.WriteFile(path, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHelmProductTGZRejectsUnsafeOrAmbiguousEntries(t *testing.T) {
	t.Parallel()
	bundle := productTGZBundle()
	valid, err := encodeHelmProductTGZ(bundle)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var canonical []testTarEntry
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		canonical = append(canonical, testTarEntry{name: header.Name, typeflag: tar.TypeReg, contents: contents})
	}

	tests := []struct {
		name    string
		entries []testTarEntry
		want    string
	}{
		{"traversal", []testTarEntry{{name: "../deployment/manifest.yml", typeflag: tar.TypeReg}}, "unsafe"},
		{"absolute", []testTarEntry{{name: "/deployment/manifest.yml", typeflag: tar.TypeReg}}, "unsafe"},
		{"symlink", []testTarEntry{{name: "application-2.0.0/deployment/manifest.yml", typeflag: tar.TypeSymlink}}, "regular file"},
		{"duplicate", append(append([]testTarEntry{}, canonical...), canonical[0]), "duplicate"},
		{"unexpected", append(append([]testTarEntry{}, canonical...), testTarEntry{name: "application-2.0.0/deployment/secret.txt", typeflag: tar.TypeReg}), "unexpected"},
		{"second root", append(append([]testTarEntry{}, canonical...), testTarEntry{name: "other/deployment/values.yaml", typeflag: tar.TypeReg}), "one root"},
		{"wrong root", []testTarEntry{{name: "wrong/deployment/manifest.yml", typeflag: tar.TypeReg, contents: canonical[0].contents}, {name: "wrong/deployment/values.yaml", typeflag: tar.TypeReg, contents: bundle.DefaultConfig}}, "does not match"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := decodeHelmProductTGZ(writeTestTGZ(t, test.entries))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want %q", err, test.want)
			}
		})
	}

	t.Run("concatenated gzip streams", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "concatenated.tgz")
		if err := os.WriteFile(path, append(append([]byte(nil), valid...), valid...), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := decodeHelmProductTGZ(path); err == nil || !strings.Contains(err.Error(), "exactly one gzip stream") {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("invalid gzip checksum", func(t *testing.T) {
		t.Parallel()
		corrupt := append([]byte(nil), valid...)
		corrupt[len(corrupt)-5] ^= 0xff
		path := filepath.Join(t.TempDir(), "corrupt.tgz")
		if err := os.WriteFile(path, corrupt, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := decodeHelmProductTGZ(path); err == nil || !strings.Contains(err.Error(), "verify Product TGZ") {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("compressed size bound", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "oversized.tgz")
		if err := os.WriteFile(path, bytes.Repeat([]byte("x"), int(maximumHelmProductArchiveBytes)+1), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := decodeHelmProductTGZ(path); err == nil || !strings.Contains(err.Error(), "compressed bytes") {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("expanded size bound", func(t *testing.T) {
		t.Parallel()
		entry := testTarEntry{
			name: "application-2.0.0/deployment/values.yaml", typeflag: tar.TypeReg,
			contents: bytes.Repeat([]byte("x"), int(maximumHelmProductArchiveExpandedBytes)+1),
		}
		if _, err := decodeHelmProductTGZ(writeTestTGZ(t, []testTarEntry{entry})); err == nil || !strings.Contains(err.Error(), "expanded bytes") {
			t.Fatalf("error=%v", err)
		}
	})
}
