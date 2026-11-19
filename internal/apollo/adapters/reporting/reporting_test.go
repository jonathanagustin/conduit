package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollo"
	app "github.com/jonathanagustin/conduit/internal/apollo/application"
	domain "github.com/jonathanagustin/conduit/internal/environments/domain"
	ports "github.com/jonathanagustin/conduit/internal/environments/ports"
)

type atomicFixture struct {
	ports.ReportingPort
	snapshot    ports.ReportProjectionSnapshot
	report      *domain.Report
	failure     error
	calls       int
	ctx         context.Context
	environment string
}

func (f *atomicFixture) UpdateReportProjection(ctx context.Context, environment string, project func(ports.ReportProjectionSnapshot) (ports.ReportProjectionInputs, *domain.Report, error)) error {
	f.calls++
	f.ctx = ctx
	f.environment = environment
	if f.failure != nil {
		return f.failure
	}
	raw, report, err := project(f.snapshot)
	if err == nil {
		f.snapshot.Inputs = raw
		f.report = report
	}
	return err
}
func TestAdapterReplacesOnlyExplicitSnapshotsAndUsesTransactionFacts(t *testing.T) {
	fixture := &atomicFixture{snapshot: ports.ReportProjectionSnapshot{
		Environment: "dev", Inputs: ports.ReportProjectionInputs{Info: json.RawMessage(`[{"id":"old"}]`), Health: json.RawMessage(`[{"productVersion":"1.2.3"}]`)}, Plan: domain.PlanProjection{ID: "plan"}, Report: domain.Report{Environment: "dev", AgentID: "prior"},
	}}
	adapter := New(fixture)
	empty := []apollo.EntityInfo{}
	expected := &domain.Report{Environment: "dev", AgentID: "next"}
	ctx := t.Context()
	calls := 0
	err := adapter.UpdateAgentReports(ctx, "dev", app.AgentReportPatch{Info: &empty}, func(snapshot app.AgentReportSnapshot) (*domain.Report, error) {
		calls++
		if snapshot.Environment != "dev" || snapshot.Plan.ID != "plan" || snapshot.Report.AgentID != "prior" || snapshot.Inputs.Info == nil || len(snapshot.Inputs.Info) != 0 || len(snapshot.Inputs.Health) != 1 || snapshot.Inputs.Health[0].ProductVersion != "1.2.3" {
			t.Fatalf("wrong transaction snapshot: %#v", snapshot)
		}
		return expected, nil
	})
	if err != nil || calls != 1 || fixture.calls != 1 || fixture.ctx != ctx || fixture.environment != "dev" || fixture.report != expected || string(fixture.snapshot.Inputs.Info) != "[]" || string(fixture.snapshot.Inputs.Health) != `[{"productVersion":"1.2.3"}]` {
		t.Fatalf("result err=%v fixture=%#v", err, fixture)
	}
	// An absent patch must preserve both committed snapshots, including empty Info.
	before := fixture.snapshot.Inputs
	err = adapter.UpdateAgentReports(ctx, "dev", app.AgentReportPatch{}, func(snapshot app.AgentReportSnapshot) (*domain.Report, error) { return nil, nil })
	if err != nil || !reflect.DeepEqual(fixture.snapshot.Inputs, before) {
		t.Fatalf("absent patch changed retained input: %v %#v", err, fixture.snapshot.Inputs)
	}
}
func TestAdapterDecodeAndProjectionErrorsDoNotCommit(t *testing.T) {
	failure := errors.New("projection refused")
	for _, tc := range []struct {
		name         string
		info, health json.RawMessage
		wantProject  bool
	}{
		{"info decode", json.RawMessage(`{`), nil, false},
		{"health decode", json.RawMessage(`[]`), json.RawMessage(`{`), false},
		{"project failure", json.RawMessage(`[]`), json.RawMessage(`[]`), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &atomicFixture{snapshot: ports.ReportProjectionSnapshot{Environment: "dev", Inputs: ports.ReportProjectionInputs{Info: tc.info, Health: tc.health}}}
			before := fixture.snapshot.Inputs
			projected := false
			err := New(fixture).UpdateAgentReports(t.Context(), "dev", app.AgentReportPatch{}, func(app.AgentReportSnapshot) (*domain.Report, error) { projected = true; return nil, failure })
			if err == nil || projected != tc.wantProject || fixture.report != nil || !reflect.DeepEqual(fixture.snapshot.Inputs, before) {
				t.Fatalf("error=%v projected=%v fixture=%#v", err, projected, fixture)
			}
			if tc.wantProject && !errors.Is(err, failure) {
				t.Fatalf("lost projection error: %v", err)
			}
		})
	}
}
func TestAdapterPreservesAtomicRefusalWithoutProjection(t *testing.T) {
	failure := errors.New("atomic store unavailable")
	fixture := &atomicFixture{failure: failure}
	projected := false
	err := New(fixture).UpdateAgentReports(t.Context(), "dev", app.AgentReportPatch{}, func(app.AgentReportSnapshot) (*domain.Report, error) { projected = true; return nil, nil })
	if err != failure || projected || fixture.calls != 1 {
		t.Fatalf("atomic refusal changed: %v projected=%v", err, projected)
	}
}
