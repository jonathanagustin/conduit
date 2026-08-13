package optimization

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHiGHSProcessSolverBackendPreflightFailureUsesOnlyAuthorizedFallback(t *testing.T) {
	request := highsCostRequest(t, []WorkItem{{ID: "job", Demand: 1, EstimatedCostMicros: map[string]int64{"r1": 1}}})
	marker := filepath.Join(t.TempDir(), "invoked")
	path, digest := writeExternalSolverScript(t, "#!/bin/sh\n/bin/touch '"+shellSingleQuote(marker)+"'\nexit 1\n")
	highs := HiGHSProcessSolverBackend{ExternalProcessSolverBackend: testExternalProcessBackend(path, digest)}
	now := time.Date(2026, 8, 11, 20, 0, 0, 0, time.UTC)

	withoutFallback, err := RunSolverRequest(context.Background(), request, []SolverBackend{highs}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if withoutFallback.Termination != SolverUnknown || withoutFallback.SelectedBackend != nil {
		t.Fatalf("unsupported model without fallback = %#v", withoutFallback)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("HiGHS process was invoked before preflight: %v", err)
	}

	request, err = NewSolverRequest(request.Recommendation, SolverSelection{PreferredBackend: "highs", AuthorizedFallbacks: []string{"fixture"}}, request.Determinism, request.Limits)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{highs, fixtureSolverBackend{outcome: SolverBackendOutcome{Termination: SolverError, Explanation: "fixture fallback reached"}}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != "fixture" || receipt.FallbackFrom != "highs" {
		t.Fatalf("explicit fallback receipt = %#v", receipt)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("HiGHS process was invoked before fallback: %v", err)
	}
}
