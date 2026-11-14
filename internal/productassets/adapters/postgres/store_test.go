package postgres

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/productassets/domain"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
	}
	s, e := New(t.Context(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func fixture(t *testing.T) domain.Prepared {
	t.Helper()
	r, e := domain.NewPrepared(domain.Manifest{APIVersion: domain.SchemaVersion, ProductGroup: "io.conduit", ProductName: "cli", Version: "1.2.3", Assets: []domain.Asset{{Name: "cli.tar.gz", URI: "https://example.org/cli.tar.gz", SHA256: strings.Repeat("a", 64)}}}, "first-publisher", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestIndependentPoolsAndLegacySnapshotsCannotReplaceMetadata(t *testing.T) {
	first, second := openStore(t), openStore(t)
	if _, e := first.db.ExecContext(t.Context(), `TRUNCATE conduit_product_asset_manifests`); e != nil {
		t.Fatal(e)
	}
	original := fixture(t)
	changed := domain.Clone(original)
	changed.Manifest.Assets[0].SHA256 = strings.Repeat("b", 64)
	changed.Digest, _ = changed.Manifest.Digest()
	var accepted atomic.Int32
	var group sync.WaitGroup
	for i := range 20 {
		group.Go(func() {
			s, r := first, original
			if i%2 != 0 {
				s, r = second, changed
			}
			if _, e := s.Prepare(t.Context(), r); e == nil {
				accepted.Add(1)
			} else if !errors.Is(e, domain.ErrConflict) {
				t.Error(e)
			}
		})
	}
	group.Wait()
	if accepted.Load() != 10 {
		t.Fatalf("accepted conflicting declarations: %d", accepted.Load())
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
		t.Fatal("retry replaced provenance", again, e)
	}
	core, e := controlplane.NewPostgresRepository(t.Context(), os.Getenv("CONDUIT_TEST_POSTGRES_DSN"))
	if e != nil {
		t.Fatal(e)
	}
	team := controlplane.Team{Name: "asset-snapshot-writer", Revision: 1}
	if previous, e := core.GetTeam(t.Context(), team.Name); e == nil {
		team.Revision = previous.Revision + 1
	} else if !errors.Is(e, controlplane.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := core.PutTeam(t.Context(), team); e != nil {
		t.Fatal(e)
	}
	if e := core.Close(); e != nil {
		t.Fatal(e)
	}
	reopened := openStore(t)
	again, e = reopened.Get(t.Context(), original.Manifest.Coordinate())
	if e != nil || !reflect.DeepEqual(again, saved) {
		t.Fatal("legacy snapshot erased independent metadata", again, e)
	}
	if _, e := first.db.ExecContext(t.Context(), `UPDATE conduit_product_asset_manifests SET document=$1 WHERE coordinate=$2`, []byte(`{}`), original.Manifest.Coordinate()); e != nil {
		t.Fatal(e)
	}
	if _, e := second.Get(t.Context(), original.Manifest.Coordinate()); !errors.Is(e, domain.ErrCorrupt) {
		t.Fatal("corruption treated as absence", e)
	}
	if _, e := second.Prepare(t.Context(), original); !errors.Is(e, domain.ErrCorrupt) {
		t.Fatal("retry overwrote corruption", e)
	}
}
