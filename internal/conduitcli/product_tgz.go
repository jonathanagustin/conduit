package conduitcli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/apollo/productmanifest"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"go.yaml.in/yaml/v4"
)

const (
	maximumHelmProductArchiveBytes         = int64(3 << 20)
	maximumHelmProductArchiveExpandedBytes = int64(3 << 20)
	maximumHelmProductArchiveEntries       = 8
)

func encodeHelmProductTGZ(bundle controlplane.HelmProductReleaseBundle) ([]byte, error) {
	if err := controlplane.ValidateHelmProductReleaseBundle(bundle); err != nil {
		return nil, err
	}
	manifest, err := yaml.Marshal(bundle.Manifest)
	if err != nil {
		return nil, err
	}
	root := bundle.Manifest.ProductName + "-" + bundle.Manifest.ProductVersion
	files := map[string][]byte{
		path.Join(root, "deployment", "manifest.yml"): manifest,
		path.Join(root, "deployment", "values.yaml"):  bundle.DefaultConfig,
	}
	if len(bundle.ConfigSchema) > 0 {
		files[path.Join(root, "deployment", "values.schema.json")] = bundle.ConfigSchema
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	gz.Header.ModTime = time.Unix(0, 0).UTC()
	gz.Header.OS = 255
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		contents := files[name]
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(contents)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0).UTC()}
		if err := tw.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("write Product archive header: %w", err)
		}
		if _, err := tw.Write(contents); err != nil {
			return nil, fmt.Errorf("write Product archive entry: %w", err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close Product tar archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("close Product gzip archive: %w", err)
	}
	if int64(compressed.Len()) > maximumHelmProductArchiveBytes {
		return nil, fmt.Errorf("Product archive exceeds %d compressed bytes", maximumHelmProductArchiveBytes)
	}
	return compressed.Bytes(), nil
}

func decodeHelmProductTGZ(archivePath string) (controlplane.HelmProductReleaseBundle, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("open Product TGZ: %w", err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("inspect Product TGZ: %w", err)
	}
	if stat.Size() > maximumHelmProductArchiveBytes {
		return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("Product TGZ exceeds %d compressed bytes", maximumHelmProductArchiveBytes)
	}
	compressed, err := io.ReadAll(io.LimitReader(file, maximumHelmProductArchiveBytes+1))
	if err != nil {
		return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("read Product TGZ: %w", err)
	}
	compressedReader := bytes.NewReader(compressed)
	gz, err := gzip.NewReader(compressedReader)
	if err != nil {
		return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("open Product TGZ gzip stream: %w", err)
	}
	gz.Multistream(false)
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := make(map[string][]byte)
	seenEntries := make(map[string]struct{})
	root := ""
	var expanded int64
	entries := 0
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("read Product TGZ: %w", err)
		}
		entries++
		if entries > maximumHelmProductArchiveEntries {
			return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("Product TGZ exceeds %d entries", maximumHelmProductArchiveEntries)
		}
		entryName := header.Name
		if header.Typeflag == tar.TypeDir {
			entryName = strings.TrimSuffix(entryName, "/")
		}
		clean := path.Clean(entryName)
		if clean != entryName || strings.HasPrefix(clean, "/") || clean == "." || strings.HasPrefix(clean, "../") {
			return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("unsafe Product TGZ entry %q", header.Name)
		}
		if _, duplicate := seenEntries[clean]; duplicate {
			return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("duplicate Product TGZ entry %q", clean)
		}
		seenEntries[clean] = struct{}{}
		parts := strings.Split(clean, "/")
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root {
			return controlplane.HelmProductReleaseBundle{}, errors.New("Product TGZ must contain exactly one root directory")
		}
		if header.Typeflag == tar.TypeDir {
			if clean != root && clean != path.Join(root, "deployment") {
				return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("unexpected Product TGZ directory %q", clean)
			}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != 0 {
			return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("Product TGZ entry %q must be a regular file", clean)
		}
		if len(parts) != 3 || parts[1] != "deployment" ||
			(parts[2] != "manifest.yml" && parts[2] != "values.yaml" && parts[2] != "values.schema.json") {
			return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("unexpected Product TGZ entry %q", clean)
		}
		if header.Size < 0 || expanded > maximumHelmProductArchiveExpandedBytes-header.Size {
			return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("Product TGZ exceeds %d expanded bytes", maximumHelmProductArchiveExpandedBytes)
		}
		expanded += header.Size
		contents, err := io.ReadAll(io.LimitReader(tr, header.Size+1))
		if err != nil || int64(len(contents)) != header.Size {
			return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("read Product TGZ entry %q", clean)
		}
		files[parts[2]] = contents
	}
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("verify Product TGZ gzip stream: %w", err)
	}
	if err := gz.Close(); err != nil {
		return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("close Product TGZ gzip stream: %w", err)
	}
	if compressedReader.Len() != 0 {
		return controlplane.HelmProductReleaseBundle{}, errors.New("Product TGZ must contain exactly one gzip stream")
	}
	manifestBytes, manifestPresent := files["manifest.yml"]
	values, valuesPresent := files["values.yaml"]
	if !manifestPresent || !valuesPresent {
		return controlplane.HelmProductReleaseBundle{}, errors.New("Product TGZ requires deployment/manifest.yml and deployment/values.yaml")
	}
	manifest, err := productmanifest.Decode(bytes.NewReader(manifestBytes))
	if err != nil {
		return controlplane.HelmProductReleaseBundle{}, err
	}
	if errs := productmanifest.ValidatePackaging(manifest); len(errs) > 0 {
		return controlplane.HelmProductReleaseBundle{}, controlplane.ValidationErrors(errs)
	}
	if root != manifest.ProductName+"-"+manifest.ProductVersion {
		return controlplane.HelmProductReleaseBundle{}, fmt.Errorf("Product TGZ root %q does not match manifest Product name and version", root)
	}
	bundle := controlplane.HelmProductReleaseBundle{Manifest: manifest, DefaultConfig: values, ConfigSchema: files["values.schema.json"]}
	if err := controlplane.ValidateHelmProductReleaseBundle(bundle); err != nil {
		return controlplane.HelmProductReleaseBundle{}, err
	}
	return bundle, nil
}
