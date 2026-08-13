//go:build highs && cgo

package highsshim

import (
	"bytes"
	"encoding/json"
	"testing"

	optimization "github.com/jonathanagustin/conduit/internal/optimization/solverprotocol"
)

func TestNativeHiGHSPrimaryObjective(t *testing.T) {
	request := testRequest(t, false)
	compiled, err := optimization.CompileHiGHSModel(request)
	if err != nil {
		t.Fatal(err)
	}
	response := solve(request, compiled, NewEngine())
	if response.Termination != optimization.SolverOptimal || response.Assignments == nil || len(*response.Assignments) != 1 || len(response.BackendReportedEvidence.Objectives) != 1 {
		t.Fatalf("native primary solve did not produce an exact optimum: %#v", response)
	}
}

func TestNativeHiGHSStrictJSONRoundTrip(t *testing.T) {
	request := testRequest(t, true)
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run(bytes.NewReader(encoded), &output, NewEngine()); err != nil {
		t.Fatal(err)
	}
	var response optimization.ExternalSolverProcessResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Termination != optimization.SolverOptimal || response.Assignments == nil || (*response.Assignments)[0].Resource != "b" {
		t.Fatalf("real JSON round trip failed: %s", output.String())
	}
}

func TestNativeHiGHSTwoPhaseCostObjective(t *testing.T) {
	request := testRequest(t, true)
	compiled, err := optimization.CompileHiGHSModel(request)
	if err != nil {
		t.Fatal(err)
	}
	response := solve(request, compiled, NewEngine())
	if response.Termination != optimization.SolverOptimal || response.Assignments == nil || (*response.Assignments)[0].Resource != "b" || len(response.BackendReportedEvidence.Objectives) != 2 {
		t.Fatalf("native cost solve did not preserve primary and minimize cost: %#v", response)
	}
}
