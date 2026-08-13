package providerbilling

import (
	"testing"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

func TestInvoiceExtractorProjectsExactPlanScopedBilledFact(t *testing.T) {
	extractor := InvoiceExtractor{Projection: InvoiceProjection{CallID: "invoice", RequirementID: "plan.cost", PlanID: "plan-7", Currency: "USD"}}
	request := optimization.AcquisitionReadRequest{ID: "invoice", Source: "provider-billing", Method: "GET", Endpoint: "/invoices?revision=final-17", Consistency: "finalized"}
	response := optimization.AcquisitionReadResponse{SourceRevision: "export-sha256:17", Body: []byte(`{"schema":"provider.billing.invoice.v1","revision":"final-17","currency":"USD","period":{"start":"2026-08-01T00:00:00Z","end":"2026-08-02T00:00:00Z"},"lines":[{"lineId":"a","planId":"plan-7","currency":"USD","amountMicros":100,"taxMicros":20,"creditMicros":5},{"lineId":"b","planId":"other","currency":"USD","amountMicros":999,"taxMicros":0,"creditMicros":0}]}`)}
	facts, err := extractor.Extract(request, response)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || string(facts[0].Value) != "115" || facts[0].Plane != optimization.StateBilled || facts[0].Fidelity != optimization.FidelityExact || facts[0].EntityID != "plan-7" {
		t.Fatalf("invoice projection = %#v", facts)
	}
}

func TestInvoiceExtractorFailsClosedOnAmbiguousOrUntrustedExports(t *testing.T) {
	extractor := InvoiceExtractor{Projection: InvoiceProjection{CallID: "invoice", RequirementID: "plan.cost", PlanID: "plan-7", Currency: "USD"}}
	request := optimization.AcquisitionReadRequest{ID: "invoice", Source: "provider-billing", Method: "GET", Endpoint: "/invoices?revision=final-17"}
	base := `{"schema":"provider.billing.invoice.v1","revision":"final-17","currency":"USD","period":{"start":"2026-08-01T00:00:00Z","end":"2026-08-02T00:00:00Z"},"lines":[{"lineId":"a","planId":"plan-7","currency":"USD","amountMicros":100,"taxMicros":0,"creditMicros":0}]}`
	for name, body := range map[string]string{
		"duplicate line":    `{"schema":"provider.billing.invoice.v1","revision":"final-17","currency":"USD","period":{"start":"2026-08-01T00:00:00Z","end":"2026-08-02T00:00:00Z"},"lines":[{"lineId":"a","planId":"plan-7","currency":"USD","amountMicros":1,"taxMicros":0,"creditMicros":0},{"lineId":"a","planId":"plan-7","currency":"USD","amountMicros":2,"taxMicros":0,"creditMicros":0}]}`,
		"negative credit":   `{"schema":"provider.billing.invoice.v1","revision":"final-17","currency":"USD","period":{"start":"2026-08-01T00:00:00Z","end":"2026-08-02T00:00:00Z"},"lines":[{"lineId":"a","planId":"plan-7","currency":"USD","amountMicros":1,"taxMicros":0,"creditMicros":-1}]}`,
		"foreign plan only": `{"schema":"provider.billing.invoice.v1","revision":"final-17","currency":"USD","period":{"start":"2026-08-01T00:00:00Z","end":"2026-08-02T00:00:00Z"},"lines":[{"lineId":"a","planId":"other","currency":"USD","amountMicros":1,"taxMicros":0,"creditMicros":0}]}`,
		"trailing":          base + ` {}`,
		"unknown field":     `{"schema":"provider.billing.invoice.v1","revision":"final-17","currency":"USD","period":{"start":"2026-08-01T00:00:00Z","end":"2026-08-02T00:00:00Z"},"lines":[{"lineId":"a","planId":"plan-7","currency":"USD","amountMicros":1,"taxMicros":0,"creditMicros":0,"secret":"no"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := extractor.Extract(request, optimization.AcquisitionReadResponse{SourceRevision: "export-sha256:17", Body: []byte(body)}); err == nil {
				t.Fatal("accepted unsafe invoice export")
			}
		})
	}
}
