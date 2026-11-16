package composition

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/appgen/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/attachments/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

func TestAppGenerationResultsStayWithTheOwningOperator(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	handler, err := NewHub(
		repository,
		controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true},
		Options{
			AppGenerator:    recorded.Adapter{},
			AppgenSandbox:   &testAppgenSandbox{},
			AppgenPublisher: &testAppgenPublisher{},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	uploaded := planningRequest(t, handler, "ada", http.MethodPost, "/v1/intake-attachments", `{"filename":"notes.txt","content":"private note\n"}`)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("upload %d %s", uploaded.Code, uploaded.Body)
	}
	var attachment domain.Attachment
	if err := json.Unmarshal(uploaded.Body.Bytes(), &attachment); err != nil || attachment.SHA256 == "" {
		t.Fatalf("attachment %s %v", uploaded.Body, err)
	}
	body := `{"name":"hello-http","description":"a private page","confirm":true,"attachmentSha256":["` + attachment.SHA256 + `"]}`
	created := planningRequest(t, handler, "ada", http.MethodPost, "/v1/app-generations", body)
	if created.Code != http.StatusOK || !strings.Contains(created.Body.String(), attachment.SHA256) {
		t.Fatalf("create %d %s", created.Code, created.Body)
	}
	var result struct {
		ID    string `json:"id"`
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || result.ID == "" || result.Owner != "ada" {
		t.Fatalf("result %s %v", created.Body, err)
	}

	adaList := planningRequest(t, handler, "ada", http.MethodGet, "/v1/app-generations", "")
	if adaList.Code != http.StatusOK || !strings.Contains(adaList.Body.String(), result.ID) {
		t.Fatalf("ada list %d %s", adaList.Code, adaList.Body)
	}
	adaGet := planningRequest(t, handler, "ada", http.MethodGet, "/v1/app-generations/"+result.ID, "")
	if adaGet.Code != http.StatusOK || !strings.Contains(adaGet.Body.String(), "Dockerfile") || !strings.Contains(adaGet.Body.String(), "a private page") {
		t.Fatalf("ada get %d %s", adaGet.Code, adaGet.Body)
	}
	adaFile := planningRequest(t, handler, "ada", http.MethodGet, "/v1/intake-attachments/"+attachment.SHA256, "")
	if adaFile.Code != http.StatusOK || !strings.Contains(adaFile.Body.String(), "private note") {
		t.Fatalf("ada file %d %s", adaFile.Code, adaFile.Body)
	}

	beaList := planningRequest(t, handler, "bea", http.MethodGet, "/v1/app-generations", "")
	if beaList.Code != http.StatusOK || strings.Contains(beaList.Body.String(), result.ID) || strings.Contains(beaList.Body.String(), "a private page") || strings.Contains(beaList.Body.String(), attachment.SHA256) {
		t.Fatalf("bea list %d %s", beaList.Code, beaList.Body)
	}
	foreign := planningRequest(t, handler, "bea", http.MethodGet, "/v1/app-generations/"+result.ID, "")
	missing := planningRequest(t, handler, "bea", http.MethodGet, "/v1/app-generations/gen_missing", "")
	if foreign.Code != http.StatusNotFound || missing.Code != http.StatusNotFound || foreign.Body.String() != missing.Body.String() {
		t.Fatalf("foreign %d %s missing %d %s", foreign.Code, foreign.Body, missing.Code, missing.Body)
	}
	foreignFile := planningRequest(t, handler, "bea", http.MethodGet, "/v1/intake-attachments/"+attachment.SHA256, "")
	missingFile := planningRequest(t, handler, "bea", http.MethodGet, "/v1/intake-attachments/"+strings.Repeat("0", 64), "")
	if foreignFile.Code != http.StatusNotFound || missingFile.Code != http.StatusNotFound || foreignFile.Body.String() != missingFile.Body.String() || strings.Contains(foreignFile.Body.String(), "private note") {
		t.Fatalf("foreign file %d %s missing file %d %s", foreignFile.Code, foreignFile.Body, missingFile.Code, missingFile.Body)
	}
	beaFiles := planningRequest(t, handler, "bea", http.MethodGet, "/v1/intake-attachments", "")
	if beaFiles.Code != http.StatusOK || strings.Contains(beaFiles.Body.String(), attachment.SHA256) {
		t.Fatalf("bea files %d %s", beaFiles.Code, beaFiles.Body)
	}

	events, err := repository.ListAuthorizationAudit(t.Context(), "app-generation/"+result.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDenial(events, "bea", "app-generation.read") {
		t.Fatalf("generation audit %+v", events)
	}
	fileEvents, err := repository.ListAuthorizationAudit(t.Context(), "intake-attachment/"+attachment.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDenial(fileEvents, "bea", "intake.read") {
		t.Fatalf("attachment audit %+v", fileEvents)
	}
}

func hasDenial(events []controlplane.AuthorizationAuditEvent, subject, action string) bool {
	for _, event := range events {
		if event.Subject == subject && event.Action == action && event.Decision == governance.DecisionDeny {
			return true
		}
	}
	return false
}
