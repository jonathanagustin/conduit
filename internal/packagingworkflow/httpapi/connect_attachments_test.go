package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

func TestConnectRunReadsPreserveNativeAttachmentEvidence(t *testing.T) {
	digests := []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}
	stored := domain.RunStatus{RunID: "attached-run", TargetPlanRef: "visible/app", AttachmentSHA256: append([]string(nil), digests...)}
	reader := &catalog{runs: []domain.RunStatus{stored}}
	mux := mountBoth(Config{Authorize: authed(), Reader: reader, Visible: allowEnvironment("visible")})
	for _, index := range []bool{false, true} {
		name, rest, procedure, body := "detail", "/v1/packaging-runs/attached-run", ConnectRoute[5:], `{"runId":"attached-run"}`
		if index {
			name, rest, procedure, body = "index", "/v1/packaging-runs?view=summary&limit=200", listProcedure, `{"view":"summary","limit":200}`
		}
		t.Run(name, func(t *testing.T) {
			native := httptest.NewRecorder()
			mux.ServeHTTP(native, httptest.NewRequest(http.MethodGet, rest, nil))
			rpc := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, procedure, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Connect-Protocol-Version", "1")
			mux.ServeHTTP(rpc, request)
			if native.Code != 200 || rpc.Code != 200 {
				t.Fatalf("native=%d %s rpc=%d %s", native.Code, native.Body.String(), rpc.Code, rpc.Body.String())
			}
			read := func(raw []byte) []string {
				t.Helper()
				var envelope map[string]json.RawMessage
				if err := json.Unmarshal(raw, &envelope); err != nil {
					t.Fatal(err)
				}
				if index {
					var items []map[string]json.RawMessage
					if err := json.Unmarshal(envelope["items"], &items); err != nil {
						t.Fatal(err)
					}
					if len(items) != 1 {
						t.Fatalf("items=%d", len(items))
					}
					envelope = items[0]
				}
				var values []string
				if field, ok := envelope["attachmentSha256"]; ok {
					if err := json.Unmarshal(field, &values); err != nil {
						t.Fatal(err)
					}
				}
				return values
			}
			expected, actual := read(native.Body.Bytes()), read(rpc.Body.Bytes())
			if !reflect.DeepEqual(expected, digests) || !reflect.DeepEqual(actual, expected) {
				t.Fatalf("native attachment evidence=%v RPC=%v expected=%v", expected, actual, digests)
			}
		})
	}
	if !reflect.DeepEqual(reader.runs[0], stored) || reader.mutations != 0 {
		t.Fatal("read changed stored attachment evidence")
	}
}
