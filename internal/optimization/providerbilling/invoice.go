// Package providerbilling contains a read-only anti-corruption adapter for
// finalized provider invoice exports. Unlike OpenCost, this adapter may emit
// an exact BILLED fact, but only for one explicitly selected Plan and one
// currency. It never authenticates, fetches, or stores provider credentials.
package providerbilling

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

const maximumInvoiceLines = 10000

type InvoiceProjection struct {
	CallID        string
	RequirementID string
	PlanID        string
	Currency      string
}

type InvoiceExtractor struct{ Projection InvoiceProjection }

type invoiceExport struct {
	Schema   string        `json:"schema"`
	Revision string        `json:"revision"`
	Currency string        `json:"currency"`
	Period   invoicePeriod `json:"period"`
	Lines    []invoiceLine `json:"lines"`
}

type invoicePeriod struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type invoiceLine struct {
	LineID       string `json:"lineId"`
	PlanID       string `json:"planId"`
	Currency     string `json:"currency"`
	AmountMicros int64  `json:"amountMicros"`
	TaxMicros    int64  `json:"taxMicros"`
	CreditMicros int64  `json:"creditMicros"`
}

func (extractor InvoiceExtractor) Extract(request optimization.AcquisitionReadRequest, response optimization.AcquisitionReadResponse) ([]optimization.ExtractedAcquisitionFact, error) {
	p := extractor.Projection
	if request.ID != p.CallID || request.Source != "provider-billing" || request.Method != "GET" || !strings.HasPrefix(request.Endpoint, "/invoices?") {
		return nil, errors.New("provider billing extractor received a foreign request")
	}
	if p.RequirementID == "" || p.PlanID == "" || !validCurrency(p.Currency) || response.SourceRevision == "" {
		return nil, errors.New("provider billing projection is incomplete")
	}
	decoder := json.NewDecoder(bytes.NewReader(response.Body))
	decoder.DisallowUnknownFields()
	var export invoiceExport
	if err := decoder.Decode(&export); err != nil {
		return nil, errors.New("provider billing response is not a strict invoice export")
	}
	if err := decoder.Decode(&struct{}{}); err == nil || !errors.Is(err, io.EOF) {
		return nil, errors.New("provider billing response contains trailing input")
	}
	if export.Schema != "provider.billing.invoice.v1" || export.Revision == "" || len(export.Revision) > 256 || export.Currency != p.Currency || len(export.Lines) == 0 || len(export.Lines) > maximumInvoiceLines {
		return nil, errors.New("provider billing export violates its bounded finalized-invoice contract")
	}
	start, err := time.Parse(time.RFC3339Nano, export.Period.Start)
	if err != nil {
		return nil, errors.New("provider billing period start is invalid")
	}
	end, err := time.Parse(time.RFC3339Nano, export.Period.End)
	if err != nil || !end.After(start) {
		return nil, errors.New("provider billing period end is invalid")
	}
	total := new(big.Int)
	seen := make(map[string]struct{}, len(export.Lines))
	matched := 0
	for _, line := range export.Lines {
		if line.LineID == "" || line.PlanID == "" || line.Currency != export.Currency || line.AmountMicros < 0 || line.TaxMicros < 0 || line.CreditMicros < 0 {
			return nil, errors.New("provider billing line is invalid")
		}
		if _, exists := seen[line.LineID]; exists {
			return nil, errors.New("provider billing export contains duplicate line ids")
		}
		seen[line.LineID] = struct{}{}
		if line.PlanID != p.PlanID {
			continue
		}
		lineTotal := new(big.Int).SetInt64(line.AmountMicros)
		lineTotal.Add(lineTotal, new(big.Int).SetInt64(line.TaxMicros))
		lineTotal.Sub(lineTotal, new(big.Int).SetInt64(line.CreditMicros))
		total.Add(total, lineTotal)
		matched++
	}
	if matched == 0 || total.Sign() < 0 {
		return nil, errors.New("provider billing export has no non-negative finalized lines for the selected Plan")
	}
	value, err := json.Marshal(json.Number(total.String()))
	if err != nil {
		return nil, fmt.Errorf("encode provider billing total: %w", err)
	}
	return []optimization.ExtractedAcquisitionFact{{
		RequirementID: p.RequirementID, EntityID: p.PlanID, Attribute: "cost.realized-total-micros", Value: value,
		Unit: p.Currency, Plane: optimization.StateBilled, Fidelity: optimization.FidelityExact,
		ValidFrom: end.UTC().Format(time.RFC3339Nano),
	}}, nil
}

func validCurrency(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, c := range value {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

var _ optimization.AcquisitionFactExtractor = InvoiceExtractor{}
