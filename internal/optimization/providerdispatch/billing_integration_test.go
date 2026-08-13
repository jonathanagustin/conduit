package providerdispatch_test

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
	"github.com/jonathanagustin/conduit/internal/optimization/providerbilling"
	"github.com/jonathanagustin/conduit/internal/optimization/providerdispatch"
)

type billingFixtureReader struct {
	observedAt time.Time
	body       []byte
}

func (reader billingFixtureReader) Read(_ context.Context, request optimization.AcquisitionReadRequest) (optimization.AcquisitionReadResponse, error) {
	return optimization.AcquisitionReadResponse{StatusCode: 200, ObservedAt: reader.observedAt.Format(time.RFC3339Nano), SourceRevision: "export-sha256:17", Body: append([]byte(nil), reader.body...)}, nil
}

func TestExactDispatchBillingProjectionProducesPlanScopedBilledFact(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	callID := "provider.invoice"
	endpoint := "/invoices?revision=final-17"
	reader, err := providerdispatch.NewReader([]providerdispatch.ReadBinding{{Source: "provider-billing", Port: billingFixtureReader{observedAt: now, body: []byte(`{"schema":"provider.billing.invoice.v1","revision":"final-17","currency":"USD","period":{"start":"2026-08-11T00:00:00Z","end":"2026-08-12T00:00:00Z"},"lines":[{"lineId":"a","planId":"plan-7","currency":"USD","amountMicros":100,"taxMicros":20,"creditMicros":5}]}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	extractor, err := providerdispatch.NewExtractor([]providerdispatch.ExtractBinding{{CallID: callID, Source: "provider-billing", Endpoint: endpoint, Extractor: providerbilling.InvoiceExtractor{Projection: providerbilling.InvoiceProjection{CallID: callID, RequirementID: "plan.cost", PlanID: "plan-7", Currency: "USD"}}}})
	if err != nil {
		t.Fatal(err)
	}
	store := optimization.FileTwinStore{Directory: t.TempDir()}
	receipt, err := (optimization.AcquisitionExecutor{Reader: reader, Extractor: extractor, Facts: store, CollectorIdentity: "collector:billing-fixture", Now: func() time.Time { return now }}).Execute(context.Background(), []optimization.StateRequirement{{ID: "plan.cost", Plane: optimization.StateBilled, MinimumFidelity: optimization.FidelityExact, MaximumStalenessSeconds: 86400}}, []optimization.AcquisitionCall{{ID: callID, Source: "provider-billing", Method: "GET", Endpoint: endpoint, Consistency: "finalized", CostUnits: 1, Observations: []optimization.FieldObservation{{Requirement: "plan.cost", Plane: optimization.StateBilled, Fidelity: optimization.FidelityExact, MaximumStalenessSeconds: 86400}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Complete || len(receipt.FactIDs) != 1 {
		t.Fatalf("billing receipt = %#v", receipt)
	}
	fact, err := store.GetFact(receipt.FactIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if fact.Plane != optimization.StateBilled || fact.EntityID != "plan-7" || fact.Attribute != "cost.realized-total-micros" || string(fact.Value) != "115" {
		t.Fatalf("billing fact = %#v", fact)
	}
}
