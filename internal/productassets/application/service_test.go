package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/productassets/adapters/memory"
	"github.com/jonathanagustin/conduit/internal/productassets/domain"
	"github.com/jonathanagustin/conduit/internal/productassets/ports"
)

type authority struct {
	subject           string
	writeErr, readErr error
}

func (a *authority) RequirePublication(context.Context, string) (string, error) {
	return a.subject, a.writeErr
}
func (a *authority) RequireRead(context.Context, string) error { return a.readErr }

type catalogue struct {
	receipts            []ports.Receipt
	publishErr, listErr error
	calls               int
}

func (c *catalogue) Publish(_ context.Context, product, version, digest string) (ports.Receipt, error) {
	c.calls++
	if c.publishErr != nil {
		return ports.Receipt{}, c.publishErr
	}
	for _, r := range c.receipts {
		if r.Product == product && r.Version == version {
			return r, nil
		}
	}
	r := ports.Receipt{Product: product, Version: version, Digest: digest, Kind: domain.Kind, PublishedAt: time.Now(), Channels: []string{"RELEASE"}}
	c.receipts = append(c.receipts, r)
	return r, nil
}
func (c *catalogue) Get(_ context.Context, product, version string) (ports.Receipt, error) {
	for _, r := range c.receipts {
		if r.Product == product && r.Version == version {
			return r, nil
		}
	}
	return ports.Receipt{}, domain.ErrNotFound
}
func (c *catalogue) List(context.Context, string) ([]ports.Receipt, error) {
	return c.receipts, c.listErr
}

type grouping struct {
	err   error
	calls int
}

func (g *grouping) Reserve(context.Context, string) error { g.calls++; return g.err }
func manifest() domain.Manifest {
	return domain.Manifest{APIVersion: domain.SchemaVersion, ProductGroup: "io.conduit", ProductName: "cli", Version: "1.2.3", Assets: []domain.Asset{{Name: "cli.tar.gz", URI: "https://example.org/cli.tar.gz", SHA256: strings.Repeat("a", 64)}}}
}
func setup(t *testing.T) (*Service, *catalogue, *authority) {
	t.Helper()
	c := &catalogue{}
	a := &authority{subject: "first-publisher"}
	s, err := New(memory.New(), c, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, c, a
}
func TestPreparedIntentDoesNotImplyPublication(t *testing.T) {
	s, c, a := setup(t)
	m := manifest()
	c.publishErr = errors.New("catalogue unavailable")
	if _, err := s.Publish(context.Background(), m); !errors.Is(err, c.publishErr) {
		t.Fatal(err)
	}
	if _, err := s.store.Get(context.Background(), m.Coordinate()); err != nil {
		t.Fatal("prepared intent lost", err)
	}
	if _, err := s.Get(context.Background(), m.Product(), m.Version); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("uncommitted intent became readable", err)
	}
	c.publishErr = nil
	a.subject = "retrying-writer"
	result, err := s.Publish(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "CATALOGUED_NOT_EXECUTABLE" {
		t.Fatal(result)
	}
	value, err := s.Get(context.Background(), m.Product(), m.Version)
	if err != nil {
		t.Fatal(err)
	}
	if value.Metadata.PreparedBy != "first-publisher" || value.Executable || value.Metadata.DeclarationTier != "PUBLISHER_DECLARED" {
		t.Fatal(value)
	}
	a.readErr = domain.ErrForbidden
	if _, err := s.Publish(context.Background(), m); err != nil {
		t.Fatal("write-only retry failed", err)
	}
	if _, err := s.Get(context.Background(), m.Product(), m.Version); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("read authority bypassed", err)
	}
}
func TestDeniedAndFailedGroupingCannotPublish(t *testing.T) {
	s, c, a := setup(t)
	m := manifest()
	a.writeErr = domain.ErrForbidden
	if _, err := s.Publish(context.Background(), m); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	if c.calls != 0 {
		t.Fatal("denied caller reached catalogue")
	}
	if _, err := s.store.Get(context.Background(), m.Coordinate()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("denied caller wrote intent", err)
	}
	a.writeErr = nil
	g := &grouping{err: domain.ErrConflict}
	s.grouping = g
	if _, err := s.Publish(context.Background(), m); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if c.calls != 0 || g.calls != 1 {
		t.Fatal("failed grouping committed publication")
	}
}
func TestBrokenReceiptIsNeverAnEmptyCatalogue(t *testing.T) {
	mutations := map[string]func(*ports.Receipt){"digest": func(r *ports.Receipt) { r.Digest = "wrong" }, "time": func(r *ports.Receipt) { r.PublishedAt = time.Time{} }, "product": func(r *ports.Receipt) { r.Product = "io.conduit:foreign" }, "empty kind": func(r *ports.Receipt) { r.Kind = "" }}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			s, c, _ := setup(t)
			m := manifest()
			if _, err := s.Publish(context.Background(), m); err != nil {
				t.Fatal(err)
			}
			mutate(&c.receipts[0])
			if _, err := s.List(context.Background(), m.Product()); !errors.Is(err, domain.ErrCorrupt) {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("duplicate", func(t *testing.T) {
		s, c, _ := setup(t)
		m := manifest()
		_, _ = s.Publish(context.Background(), m)
		c.receipts = append(c.receipts, c.receipts[0])
		if _, err := s.List(context.Background(), m.Product()); !errors.Is(err, domain.ErrCorrupt) {
			t.Fatal(err)
		}
	})
	t.Run("missing metadata", func(t *testing.T) {
		s, c, _ := setup(t)
		m := manifest()
		c.receipts = []ports.Receipt{{Product: m.Product(), Version: m.Version, Kind: domain.Kind, PublishedAt: time.Now()}}
		if _, err := s.Get(context.Background(), m.Product(), m.Version); !errors.Is(err, domain.ErrCorrupt) {
			t.Fatal(err)
		}
	})
	t.Run("other kind", func(t *testing.T) {
		s, c, _ := setup(t)
		m := manifest()
		c.receipts = []ports.Receipt{{Product: m.Product(), Version: m.Version, Kind: "helm-chart"}}
		list, err := s.List(context.Background(), m.Product())
		if err != nil || len(list) != 0 {
			t.Fatal(list, err)
		}
		if _, err := s.Get(context.Background(), m.Product(), m.Version); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("catalogue failure", func(t *testing.T) {
		s, c, _ := setup(t)
		c.listErr = errors.New("storage failed")
		if _, err := s.List(context.Background(), manifest().Product()); !errors.Is(err, c.listErr) {
			t.Fatal(err)
		}
	})
}

type faultyStore struct {
	ports.Store
	prepare func(domain.Prepared) (domain.Prepared, error)
	getErr  error
}

func (s faultyStore) Prepare(_ context.Context, r domain.Prepared) (domain.Prepared, error) {
	return s.prepare(r)
}
func (s faultyStore) Get(ctx context.Context, key string) (domain.Prepared, error) {
	if s.getErr != nil {
		return domain.Prepared{}, s.getErr
	}
	return s.Store.Get(ctx, key)
}
func TestStorageFailuresAndForgedAcknowledgementsFailClosed(t *testing.T) {
	for _, mode := range []string{"prepare error", "invalid stored digest", "foreign stored coordinate"} {
		t.Run(mode, func(t *testing.T) {
			s, c, _ := setup(t)
			failure := errors.New("storage unavailable")
			s.store = faultyStore{prepare: func(r domain.Prepared) (domain.Prepared, error) {
				switch mode {
				case "prepare error":
					return domain.Prepared{}, failure
				case "invalid stored digest":
					r.Digest = "invalid"
				case "foreign stored coordinate":
					r.Manifest.ProductName = "foreign"
					r.Digest, _ = r.Manifest.Digest()
				}
				return r, nil
			}}
			_, err := s.Publish(context.Background(), manifest())
			expected := domain.ErrCorrupt
			if mode == "prepare error" {
				expected = failure
			}
			if !errors.Is(err, expected) || c.calls != 0 {
				t.Fatal("invalid preparation reached catalogue", err, c.calls)
			}
		})
	}
	s, c, _ := setup(t)
	m := manifest()
	_, err := s.Publish(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("metadata unavailable")
	s.store = faultyStore{Store: s.store, getErr: failure}
	if _, err := s.Get(context.Background(), m.Product(), m.Version); !errors.Is(err, failure) {
		t.Fatal("storage failure flattened", err)
	}
	c.receipts[0].Digest = "forged"
	s.store = memory.New()
	if _, err := s.Publish(context.Background(), m); !errors.Is(err, domain.ErrCorrupt) {
		t.Fatal("forged catalogue acknowledgement accepted", err)
	}
}
func TestCanceledPublicationCannotPrepareOrCommit(t *testing.T) {
	s, c, _ := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Publish(ctx, manifest()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if c.calls != 0 {
		t.Fatal("canceled publication reached catalogue")
	}
	if _, err := s.store.Get(context.Background(), manifest().Coordinate()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("canceled publication prepared intent", err)
	}
}
