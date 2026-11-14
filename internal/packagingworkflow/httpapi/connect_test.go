package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	packagingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1/packagingv1connect"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

func TestConnectRouteMatchesTheGeneratedProcedure(t *testing.T) {
	if ConnectRoute != "POST "+packagingv1connect.PackagingRunServiceGetPackagingRunProcedure {
		t.Fatalf("ConnectRoute = %q, procedure = %q", ConnectRoute, packagingv1connect.PackagingRunServiceGetPackagingRunProcedure)
	}
}

func TestConnectHappyPathNotFoundAndUnauthorizedMatchREST(t *testing.T) {
	stored := domain.RunStatus{
		RunID:             "sample-app-1",
		Phase:             domain.PhaseReview,
		Outcome:           string(domain.OutcomeRejected),
		Reason:            "needs a person",
		SourceDigest:      "sha256:source",
		ArtifactDigest:    "sha256:artifact",
		ProposalRevision:  "rev-2",
		TargetPlanRef:     "plan-ref",
		PlanID:            "plan_abc",
		HandoffReconciled: true,
		LastReview:        string(domain.ReviewAccepted),
		DuplicateReviews:  2,
		TemplateVersion:   "fixture-1",
		ToolVersions:      "lint=fixture-1",
		// A reader must not be able to relabel the run. Both transports stamp
		// the same authority fields over whatever the reader returned.
		Authority:         "temporal-approval",
		PlanAuthority:     "temporal-plan",
		ApprovalAuthority: "temporal-governance",
		Tier:              "other",
	}
	mux := mountBoth(Config{
		Authorize: func(*http.Request) error { return nil },
		Reader:    fixedReader{status: stored},
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	rest := getREST(t, server.URL, "sample-app-1")
	got := getConnect(t, server, "sample-app-1")
	assertSameRun(t, rest, got)
	if rest.Authority != "packaging-run" || rest.PlanAuthority != "conduit-plan" || rest.ApprovalAuthority != "conduit-governance" || rest.Tier != "CONDUIT_NATIVE" || !rest.Configured {
		t.Fatalf("stamped status = %#v", rest)
	}
	if rest.PlanID != "plan_abc" || rest.Phase != domain.PhaseReview || rest.DuplicateReviews != 2 || !rest.HandoffReconciled {
		t.Fatalf("payload = %#v", rest)
	}
	if rest.TemplateVersion != "fixture-1" || rest.ToolVersions != "lint=fixture-1" {
		t.Fatalf("versions = %#v", rest)
	}

	missing := httptest.NewServer(mountBoth(Config{
		Authorize: func(*http.Request) error { return nil },
		Reader:    missingReader{},
	}))
	t.Cleanup(missing.Close)
	if status, _ := raw(t, http.MethodGet, missing.URL+"/v1/packaging-runs/missing", ""); status != http.StatusNotFound {
		t.Fatalf("REST not-found status = %d", status)
	}
	_, err := connectClient(missing).GetPackagingRun(t.Context(), connect.NewRequest(&packagingv1.GetPackagingRunRequest{RunId: "missing"}))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("Connect not-found code = %v (%v)", connect.CodeOf(err), err)
	}
	procedure := packagingv1connect.PackagingRunServiceGetPackagingRunProcedure
	if status, body := postConnect(t, missing.URL+procedure, `{"runId":"missing"}`); status != http.StatusNotFound {
		t.Fatalf("Connect HTTP not-found status = %d body = %s", status, body)
	}

	refused := httptest.NewServer(mountBoth(Config{Authorize: func(*http.Request) error {
		return StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
	}}))
	t.Cleanup(refused.Close)
	if status, body := raw(t, http.MethodGet, refused.URL+"/v1/packaging-runs/sample-app-1", ""); status != http.StatusUnauthorized || !strings.Contains(body, "unauthorized") {
		t.Fatalf("REST unauthorized = %d %s", status, body)
	}
	if status, body := postConnect(t, refused.URL+procedure, `{"runId":"sample-app-1"}`); status != http.StatusUnauthorized || !strings.Contains(body, "unauthorized") {
		t.Fatalf("Connect unauthorized = %d %s", status, body)
	}
	_, err = connectClient(refused).GetPackagingRun(t.Context(), connect.NewRequest(&packagingv1.GetPackagingRunRequest{RunId: "sample-app-1"}))
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("Connect unauthorized code = %v (%v)", connect.CodeOf(err), err)
	}
}

func TestConnectUnconfiguredReaderMatchesREST(t *testing.T) {
	server := httptest.NewServer(mountBoth(Config{Authorize: func(*http.Request) error { return nil }}))
	t.Cleanup(server.Close)
	rest := getREST(t, server.URL, "sample-app-1")
	got := getConnect(t, server, "sample-app-1")
	assertSameRun(t, rest, got)
	if rest.Configured || rest.RunID != "" || rest.Authority != "packaging-run" {
		t.Fatalf("unconfigured = %#v", rest)
	}
}

func mountBoth(cfg Config) http.Handler {
	mux := http.NewServeMux()
	Register(mux, cfg)
	RegisterConnect(mux, cfg)
	return mux
}

type missingReader struct{}

func (missingReader) Read(context.Context, string) (domain.RunStatus, error) {
	return domain.RunStatus{}, ErrNotFound
}

func (missingReader) List(context.Context) ([]domain.RunStatus, error) {
	return nil, nil
}

func (missingReader) Events(context.Context, string) ([]domain.StepEvent, error) {
	return nil, ErrNotFound
}

func getREST(t *testing.T, baseURL, runID string) domain.RunStatus {
	t.Helper()
	status, body := raw(t, http.MethodGet, baseURL+"/v1/packaging-runs/"+runID, "")
	if status != http.StatusOK {
		t.Fatalf("REST status = %d body = %s", status, body)
	}
	var decoded domain.RunStatus
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func getConnect(t *testing.T, server *httptest.Server, runID string) domain.RunStatus {
	t.Helper()
	response, err := connectClient(server).GetPackagingRun(t.Context(), connect.NewRequest(&packagingv1.GetPackagingRunRequest{RunId: runID}))
	if err != nil {
		t.Fatal(err)
	}
	message := response.Msg
	return domain.RunStatus{
		Tier:              message.GetTier(),
		Configured:        message.GetConfigured(),
		Authority:         message.GetAuthority(),
		PlanAuthority:     message.GetPlanAuthority(),
		ApprovalAuthority: message.GetApprovalAuthority(),
		RunID:             message.GetRunId(),
		Phase:             message.GetPhase(),
		Outcome:           message.GetOutcome(),
		Reason:            message.GetReason(),
		SourceDigest:      message.GetSourceDigest(),
		ArtifactDigest:    message.GetArtifactDigest(),
		ProposalRevision:  message.GetProposalRevision(),
		TargetPlanRef:     message.GetTargetPlanRef(),
		PlanID:            message.GetPlanId(),
		HandoffReconciled: message.GetHandoffReconciled(),
		LastReview:        message.GetLastReview(),
		DuplicateReviews:  int(message.GetDuplicateReviews()),
		TemplateVersion:   message.GetTemplateVersion(),
		ToolVersions:      message.GetToolVersions(),
	}
}

func assertSameRun(t *testing.T, rest, got domain.RunStatus) {
	t.Helper()
	if rest != got {
		t.Fatalf("REST = %#v\nConnect = %#v", rest, got)
	}
}

func connectClient(server *httptest.Server) packagingv1connect.PackagingRunServiceClient {
	return packagingv1connect.NewPackagingRunServiceClient(server.Client(), server.URL)
}

func postConnect(t *testing.T, url, body string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	return do(t, request)
}

func raw(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return do(t, request)
}

func do(t *testing.T, request *http.Request) (int, string) {
	t.Helper()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(payload)
}
