package controlplane

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// Deliberately no embedded Repository: broadening the catalog input to include
// writes, transactions or other aggregate operations must fail compilation.
type catalogReadOnlySource struct {
	plansFailure, catalogFailure error
	calls                        []string
	contexts                     []context.Context
}

func (s *catalogReadOnlySource) ListPlans(ctx context.Context, environment string) ([]PlanRecord, error) {
	s.calls = append(s.calls, "plans:"+environment)
	s.contexts = append(s.contexts, ctx)
	return nil, s.plansFailure
}
func (s *catalogReadOnlySource) ListEnvironmentReports(ctx context.Context) ([]EnvironmentReport, error) {
	s.calls = append(s.calls, "reports")
	s.contexts = append(s.contexts, ctx)
	return nil, s.catalogFailure
}
func (s *catalogReadOnlySource) ListProductPublications(ctx context.Context, product string) ([]ProductPublication, error) {
	s.calls = append(s.calls, "publications:"+product)
	s.contexts = append(s.contexts, ctx)
	return nil, s.catalogFailure
}

func TestCatalogReadersAcceptNarrowSourcesAndPreserveReadFailures(t *testing.T) {
	failure := errors.New("read unavailable")
	for _, kind := range []string{"environment", "product"} {
		for _, stage := range []string{"plans", "catalog", "empty"} {
			t.Run(kind+"/"+stage, func(t *testing.T) {
				source := &catalogReadOnlySource{}
				if stage == "plans" {
					source.plansFailure = failure
				}
				if stage == "catalog" {
					source.catalogFailure = failure
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var err error
				var empty bool
				if kind == "environment" {
					var records []EnvironmentCatalogRecord
					records, err = ListEnvironmentCatalog(ctx, source)
					empty = records != nil && len(records) == 0
				} else {
					var records []ProductCatalogRecord
					records, err = ListProductCatalog(ctx, source)
					empty = records != nil && len(records) == 0
				}
				if stage == "empty" {
					if err != nil || !empty {
						t.Fatalf("empty catalog changed: empty=%v error=%v", empty, err)
					}
				} else if err != failure {
					t.Fatalf("failure identity changed: %v", err)
				}
				want := []string{"plans:"}
				if stage != "plans" {
					if kind == "environment" {
						want = append(want, "reports")
					} else {
						want = append(want, "publications:")
					}
				}
				if !reflect.DeepEqual(source.calls, want) {
					t.Fatalf("reads=%v want=%v", source.calls, want)
				}
				for _, got := range source.contexts {
					if got != ctx {
						t.Fatal("caller context lost")
					}
				}
			})
		}
	}
}
