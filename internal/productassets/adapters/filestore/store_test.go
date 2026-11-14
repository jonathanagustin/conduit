package filestore

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/productassets/domain"
)

func declaration(t *testing.T) domain.Prepared {
	t.Helper()
	m := domain.Manifest{APIVersion: domain.SchemaVersion, ProductGroup: "io.conduit", ProductName: "cli", Version: "1.2.3", Assets: []domain.Asset{{Name: "cli.tar.gz", URI: "https://example.org/cli.tar.gz", SHA256: strings.Repeat("a", 64)}}}
	r, e := domain.NewPrepared(m, "first-publisher", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestIndependentStoresPreserveImmutableMetadataAndFirstProvenance(t *testing.T) {
	root := t.TempDir()
	first, e := New(root)
	if e != nil {
		t.Fatal(e)
	}
	defer first.Close()
	second, e := New(root)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	original := declaration(t)
	changed := domain.Clone(original)
	changed.Manifest.Assets[0].SHA256 = strings.Repeat("b", 64)
	changed.Digest, _ = changed.Manifest.Digest()
	var accepted atomic.Int32
	var group sync.WaitGroup
	for i := range 20 {
		group.Go(func() {
			store := first
			r := original
			if i%2 != 0 {
				store = second
				r = changed
			}
			if _, err := store.Prepare(t.Context(), r); err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, domain.ErrConflict) {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if accepted.Load() != 10 {
		t.Fatalf("accepted conflicting metadata: %d", accepted.Load())
	}
	saved, e := first.Get(t.Context(), original.Manifest.Coordinate())
	if e != nil {
		t.Fatal(e)
	}
	retry := domain.Clone(saved)
	retry.PreparedBy = "retrying-writer"
	retry.PreparedAt = time.Now().Add(time.Hour)
	again, e := second.Prepare(t.Context(), retry)
	if e != nil || !reflect.DeepEqual(again, saved) {
		t.Fatal("retry replaced first provenance", again, e)
	}
	reopened, e := New(root)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	again, e = reopened.Get(t.Context(), original.Manifest.Coordinate())
	if e != nil || !reflect.DeepEqual(again, saved) {
		t.Fatal("reopen lost metadata", e)
	}
	info, e := os.Stat(filepath.Join(root, key(original.Manifest.Coordinate())))
	if e != nil {
		t.Fatal(e)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatal("metadata is not private")
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 1 {
		t.Fatal("temporary files leaked", entries, e)
	}
}
func TestCorruptDocumentsAndLinksCannotBecomePublishedMetadata(t *testing.T) {
	for _, mode := range []string{"corrupt", "symlink", "directory"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			s, e := New(root)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			r := declaration(t)
			path := filepath.Join(root, key(r.Manifest.Coordinate()))
			switch mode {
			case "corrupt":
				e = os.WriteFile(path, []byte(`{}`), 0o600)
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside")
				e = os.WriteFile(outside, []byte(`{}`), 0o600)
				if e == nil {
					e = os.Symlink(outside, path)
				}
			case "directory":
				e = os.Mkdir(path, 0o700)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e := s.Get(t.Context(), r.Manifest.Coordinate()); !errors.Is(e, domain.ErrCorrupt) {
				t.Fatalf("got %v", e)
			}
			if _, e := s.Prepare(t.Context(), r); !errors.Is(e, domain.ErrCorrupt) {
				t.Fatalf("existing corruption overwritten: %v", e)
			}
		})
	}
}
