package application

import (
	"context"
	"fmt"
	"testing"
)

func mustCases(t *testing.T, service *Service) []Case {
	t.Helper()
	cases, err := service.Cases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return cases
}

// A restarted Hub is a new Service over the same store. The case an operator
// added before the restart is listed and scored after it.
func TestAddedCasesComeFromTheStoreAndSurviveANewService(t *testing.T) {
	store := NewMemoryCases()
	before := New("", "")
	before.Stored = store
	added, err := before.AddCase(context.Background(), "operator rejected 2Gi", "sample-app-1", "2Gi", "FAIL_REQUIRED")
	if err != nil {
		t.Fatal(err)
	}

	after := New("", "")
	after.Stored = store
	cases := mustCases(t, after)
	if len(cases) != 4 || cases[3] != added || cases[0].ID != "mem-512" {
		t.Fatalf("cases after a restart = %+v", cases)
	}
	results, _, err := after.Run(context.Background(), "ada")
	if err != nil || len(results) != 4 || results[3].Case.ID != added.ID || !results[3].Columns[0].Passed {
		t.Fatalf("the stored case was not scored after a restart: %+v, %v", results, err)
	}
	// The same case added after the restart is the stored one, not a copy.
	again, err := after.AddCase(context.Background(), "operator rejected 2Gi", "sample-app-1", "2Gi", "FAIL_REQUIRED")
	if err != nil || again != added || len(mustCases(t, after)) != 4 {
		t.Fatalf("re-adding after a restart = %+v, %v", again, err)
	}
}

type failingCases struct{ err error }

func (store failingCases) Add(context.Context, Case, int) (Case, error) { return Case{}, store.err }
func (store failingCases) List(context.Context) ([]Case, error)         { return nil, store.err }

func TestAStoreFaultIsReportedNotHidden(t *testing.T) {
	service := New("", "")
	service.Stored = failingCases{err: fmt.Errorf("database is down")}
	if _, err := service.Cases(context.Background()); err == nil {
		t.Fatal("a store fault listed the built-in cases as if nothing were wrong")
	}
	if _, _, err := service.Run(context.Background(), "ada"); err == nil {
		t.Fatal("a store fault ran the evals on a partial case list")
	}
	if _, err := service.AddCase(context.Background(), "s", "b", "512Mi", "PASS"); err == nil {
		t.Fatal("a store fault reported the case as added")
	}
}
