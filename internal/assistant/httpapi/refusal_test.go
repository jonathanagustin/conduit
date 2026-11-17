package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

type waitHint struct{ wait time.Duration }

func (hint waitHint) Error() string                 { return "refused" }
func (hint waitHint) RetryAfterHint() time.Duration { return hint.wait }

// A model call refused by a limit is a 429 with Retry-After from this handler
// itself, whatever wraps it.
func TestARefusedModelCallIsA429WithRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		err   error
		code  string
		after string
	}{
		{fmt.Errorf("%w: %w", domain.ErrModelBusy, waitHint{1500 * time.Millisecond}), "MODEL_BUSY", "2"},
		{fmt.Errorf("%w: %w", domain.ErrModelBudgetExceeded, waitHint{time.Hour}), "MODEL_BUDGET_EXCEEDED", "3600"},
		{domain.ErrModelBusy, "MODEL_BUSY", "1"},
	} {
		recorder := httptest.NewRecorder()
		writeTurnError(recorder, application.Turn{}, tc.err)
		if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != tc.after ||
			!strings.Contains(recorder.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatalf("%v = %d Retry-After %q %s", tc.err, recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
		}
	}
	recorder := httptest.NewRecorder()
	writeTurnError(recorder, application.Turn{}, domain.ErrModelFailed)
	if recorder.Code != http.StatusBadGateway || recorder.Header().Get("Retry-After") != "" {
		t.Fatalf("a model failure = %d Retry-After %q", recorder.Code, recorder.Header().Get("Retry-After"))
	}
}
