package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelgovernor/httpapi"
)

// Retained policy charges can exceed reported attempts in ambiguous legacy
// rows. The wire must report the charge without renaming it as observation.
type historicalUsage struct{ modelgovernor.UsageStore }

func (historicalUsage) OwnerDay(context.Context, string, string) (modelgovernor.Totals, error) {
	return modelgovernor.Totals{Calls: 4}, nil
}

func (historicalUsage) Summary(context.Context, modelgovernor.UsageQuery) ([]modelgovernor.UsageRow, error) {
	return []modelgovernor.UsageRow{{Owner: "ada", Calls: 5, BudgetedCalls: 4, Attempts: 1}}, nil
}

func TestUsageJSONNamesConservativePolicyChargesWithoutClaimingHistoricalInvocations(t *testing.T) {
	mux := http.NewServeMux()
	governor := modelgovernor.New(modelgovernor.Config{DailyCallBudget: 10}, modelgovernor.Dependencies{Usage: historicalUsage{}})
	httpapi.Register(mux, httpapi.Config{
		Governor:      governor,
		Caller:        func(*http.Request) (string, bool) { return "ada", true },
		Administrator: func(*http.Request) (string, int) { return "root", 0 },
	})
	for _, path := range []string{"/v1/ai/usage", "/v1/admin/ai/usage"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			var document struct {
				Usage []map[string]json.RawMessage `json:"usage"`
			}
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &document) != nil || len(document.Usage) != 1 {
				t.Fatalf("usage response=%d %s", response.Code, response.Body.String())
			}
			row := document.Usage[0]
			if string(row["calls"]) != "5" || string(row["budgetedCalls"]) != "4" || string(row["attempts"]) != "1" {
				t.Fatalf("reported request/charge/attempt evidence changed: %s", response.Body.String())
			}
			if _, present := row["attemptedCalls"]; present {
				t.Fatalf("policy charge was also presented as historical invocation count: %s", response.Body.String())
			}
		})
	}
}
