package controlplane

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestPublicationOnlyCoordinatesCanBeRetiredWithoutExecutableReleases(t *testing.T) {
	factories := map[string]func(*testing.T) Repository{
		"memory": func(*testing.T) Repository { return NewMemoryRepository() },
		"file": func(t *testing.T) Repository {
			r, e := NewFileRepository(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			return r
		},
		"postgres": func(t *testing.T) Repository {
			dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
			if dsn == "" {
				t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
			}
			r, e := NewPostgresRepository(t.Context(), dsn)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { _ = r.Close() })
			return r
		},
	}
	for kind, factory := range factories {
		t.Run(kind, func(t *testing.T) {
			repository := factory(t)
			for _, scope := range []string{CatalogRetirementRelease, CatalogRetirementProduct} {
				t.Run(scope, func(t *testing.T) {
					product := "retirement.example:metadata-" + kind + "-" + scope
					version := "1.2.3"
					if _, e := repository.CreateProductPublication(t.Context(), ProductPublication{MavenCoordinate: product + ":" + version, Product: product, Version: version, Kind: "download-assets", PublicationDigest: "sha256:declaration", CreatedAt: time.Now().UTC()}); e != nil {
						t.Fatal(e)
					}
					service := NewCatalogRetirementService(repository)
					if _, e := service.Prepare(t.Context(), CatalogRetirementRelease, product, "9.9.9", "operator", "obsolete"); !errors.Is(e, ErrNotFound) {
						t.Fatal("another published version established identity", e)
					}
					selectedVersion := version
					if scope == CatalogRetirementProduct {
						selectedVersion = ""
					}
					prepared, e := service.Prepare(t.Context(), scope, product, selectedVersion, "operator", "obsolete download")
					if e != nil {
						t.Fatal("publication-only identity refused", e)
					}
					prepared.ChangeRequestID = "crq_retire_metadata"
					if _, e := repository.CreateCatalogRetirement(t.Context(), prepared); e != nil {
						t.Fatal("repository disagreed with preparation", e)
					}
					if _, e := repository.GetCatalogRetirement(t.Context(), product, selectedVersion); e != nil {
						t.Fatal(e)
					}
					publications, e := repository.ListProductPublications(t.Context(), product)
					if e != nil || len(publications) != 1 || publications[0].Release != nil {
						t.Fatal("retirement erased metadata or invented execution", publications, e)
					}
				})
			}
		})
	}
}
