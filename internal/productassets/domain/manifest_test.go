package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func fixture() Manifest {
	return Manifest{APIVersion: SchemaVersion, ProductGroup: "io.conduit", ProductName: "cli", Version: "1.2.3", Assets: []Asset{{Name: "cli-linux.tar.gz", Target: "linux/amd64", URI: "https://example.org/cli.tar.gz", SHA256: strings.Repeat("a", 64)}}}
}
func TestCanonicalDigestAndOwnership(t *testing.T) {
	m := fixture()
	size := int64(42)
	m.Assets[0].SizeBytes = &size
	m.Source = &Source{RepositoryURI: "https://example.org/source", Commit: strings.Repeat("b", 40)}
	second := m.Assets[0]
	second.Name = "cli-darwin.tar.gz"
	second.Target = "darwin/arm64"
	m.Assets = append(m.Assets, second)
	first, err := m.Digest()
	if err != nil {
		t.Fatal(err)
	}
	m.Assets[0], m.Assets[1] = m.Assets[1], m.Assets[0]
	reordered, _ := m.Digest()
	if first != reordered {
		t.Fatal("asset ordering changed immutable identity")
	}
	cloned := CloneManifest(m)
	*cloned.Assets[0].SizeBytes = 43
	cloned.Source.Commit = strings.Repeat("c", 40)
	if size != 42 || m.Source.Commit != strings.Repeat("b", 40) {
		t.Fatal("clone shared mutable metadata")
	}
	changed, _ := cloned.Digest()
	if changed == first {
		t.Fatal("changed metadata retained digest")
	}
	record, err := NewPrepared(m, "publisher", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := json.Marshal(record)
	if _, err := DecodePrepared(m.Coordinate(), doc); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][]byte{"unknown": append(append([]byte{}, doc[:len(doc)-1]...), []byte(`,"extra":true}`)...), "second document": append(append([]byte{}, doc...), []byte(` {}`)...), "digest": []byte(strings.Replace(string(doc), record.Digest, "sha256:"+strings.Repeat("0", 64), 1))} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodePrepared(m.Coordinate(), bad); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, err := DecodePrepared("io.conduit:other:1.2.3", doc); !errors.Is(err, ErrCorrupt) {
		t.Fatal("foreign coordinate accepted")
	}
}
func TestManifestRejectsUnsafeDeclarations(t *testing.T) {
	tests := map[string]func(*Manifest){
		"schema":       func(m *Manifest) { m.APIVersion = "" },
		"overflow":     func(m *Manifest) { m.Version = "4294967296.0.0" },
		"leading zero": func(m *Manifest) { m.Version = "01.2.3" },
		"prerelease":   func(m *Manifest) { m.Version = "1.2.3-rc.1" },
		"credentials":  func(m *Manifest) { m.Assets[0].URI = "https://secret:token@example.org/archive" },
		"query":        func(m *Manifest) { m.Assets[0].URI += "?token=secret" },
		"empty query":  func(m *Manifest) { m.Assets[0].URI += "?" },
		"fragment":     func(m *Manifest) { m.Assets[0].URI += "#fragment" },
		"http":         func(m *Manifest) { m.Assets[0].URI = "http://example.org/archive" },
		"path name":    func(m *Manifest) { m.Assets[0].Name = "../archive" },
		"duplicate case": func(m *Manifest) {
			a := m.Assets[0]
			a.Name = strings.ToUpper(a.Name)
			a.Target = ""
			m.Assets = append(m.Assets, a)
		},
		"duplicate target":     func(m *Manifest) { a := m.Assets[0]; a.Name = "second.tar.gz"; m.Assets = append(m.Assets, a) },
		"missing digest":       func(m *Manifest) { m.Assets[0].SHA256 = "" },
		"zero size":            func(m *Manifest) { size := int64(0); m.Assets[0].SizeBytes = &size },
		"evidence credentials": func(m *Manifest) { m.Assets[0].ProvenanceBundleURI = "https://token@example.org/provenance" },
		"short commit":         func(m *Manifest) { m.Source = &Source{RepositoryURI: "https://example.org/repo", Commit: "abcd"} },
		"empty assets":         func(m *Manifest) { m.Assets = nil },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			m := fixture()
			mutate(&m)
			if err := m.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
	m := fixture()
	m.Version = "4294967295.4294967295.4294967295"
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
}
