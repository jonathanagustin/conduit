package composition

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/adapters/nodebridge"
	acpdomain "github.com/jonathanagustin/conduit/internal/workspaceacp/domain"
)

func TestWorkspaceACPBridgeIntegration(t *testing.T) {
	if os.Getenv("CONDUIT_TEST_AGENT_CLIENT") != "1" {
		t.Skip("executed by scripts/test-console.sh with the frozen console toolchain")
	}

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	webDir := filepath.Join(root, "web")
	fakeAgent := filepath.Join(webDir, "src", "server", "fake-agent.mjs")

	pool := nodebridge.NewPool(nodebridge.Config{
		WebDir:        webDir,
		NodeBinary:    "node",
		AgentCommand:  "node",
		AgentArgs:     []string{fakeAgent},
		Fixture:       true,
		OfflineBundle: true,
	})
	defer func() { _ = pool.Close() }()

	repo := controlplane.NewMemoryRepository()
	gov := controlplane.NewGovernanceService(repo)
	if _, err := gov.PutTeam(t.Context(), controlplane.Team{
		Name:        "devs",
		DisplayName: "Devs",
		AdminUsers:  []string{"alice"},
		MemberUsers: []string{"alice"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := gov.PutGrant(t.Context(), controlplane.ResourceGrant{
		ResourceRID: "environment/dev-int",
		Team:        "devs",
		Operations:  []string{controlplane.OperationReadResource, controlplane.OperationProposeChange, controlplane.OperationOperate},
	}); err != nil {
		t.Fatal(err)
	}

	hub, err := NewHub(repo, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"root"},
	}, Options{
		WorkspaceACPBridges:       pool,
		WorkspaceACPOfflineBundle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(hub)
	defer server.Close()

	// Create workspace for alice.
	createReq, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/v1/dev-workspaces", strings.NewReader(`{"profileId":"go-service","environmentId":"dev-int","title":"Integration WS"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-Platform-Subject", "alice")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /v1/dev-workspaces returned %d, want 201", createResp.StatusCode)
	}
	var created struct {
		ID string `json:"workspaceId"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	branch := acpdomain.DefaultBranch("alice", created.ID)

	// Drive real JS editor ACP client -> Go Hub -> Node ACP bridge -> fake-agent.mjs -> JS console readAgentStream.
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "src/test/fixtures/workspace-acp-client.mjs")
	cmd.Dir = webDir
	cmd.Env = append(
		os.Environ(),
		"CONDUIT_ACP_FIXTURE_ORIGIN="+server.URL,
		"CONDUIT_ACP_FIXTURE_WORKSPACE="+created.ID,
		"CONDUIT_ACP_FIXTURE_BRANCH="+branch,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("workspace-acp-client.mjs failed: %v\n%s", err, out)
	}

	// Verify notes.txt was committed into the Hub workspace store.
	fileReq, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/v1/dev-workspaces/"+created.ID+"/file?path=notes.txt", nil)
	fileReq.Header.Set("X-Platform-Subject", "alice")
	fileResp, err := http.DefaultClient.Do(fileReq)
	if err != nil {
		t.Fatal(err)
	}
	defer fileResp.Body.Close()
	var filePayload struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(fileResp.Body).Decode(&filePayload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filePayload.Content, "hello from real acp bridge") {
		t.Fatalf("workspace store missing notes.txt content: %+v", filePayload)
	}

	// Prove direct browser request to the underlying loopback Node ACP bridge is refused with 403,
	// and unauthenticated direct request without token is refused with 401.
	instances := pool.Instances()
	if len(instances) == 0 {
		t.Fatal("expected at least one running Node ACP bridge instance")
	}
	bridgeURL := fmt.Sprintf("http://127.0.0.1:%d/acp", instances[0].Port())

	browserReq, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, bridgeURL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`))
	browserReq.Header.Set("Origin", "http://localhost:5173")
	browserReq.Header.Set("Sec-Fetch-Site", "cross-site")
	browserReq.Header.Set("Content-Type", "application/json")
	browserResp, err := http.DefaultClient.Do(browserReq)
	if err != nil {
		t.Fatal(err)
	}
	browserResp.Body.Close()
	if browserResp.StatusCode != http.StatusForbidden {
		t.Fatalf("direct browser call to Node ACP bridge returned %d, want 403", browserResp.StatusCode)
	}

	unauthReq, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, bridgeURL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`))
	unauthReq.Header.Set("Content-Type", "application/json")
	unauthResp, err := http.DefaultClient.Do(unauthReq)
	if err != nil {
		t.Fatal(err)
	}
	unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("direct unauthenticated call to Node ACP bridge returned %d, want 401", unauthResp.StatusCode)
	}

	// Prove offline-bundle HTTP client refuses non-loopback egress.
	offlineClient := instances[0].HTTPClient()
	extReq, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://93.184.216.34/bundle-probe", nil)
	if extResp, extErr := offlineClient.Do(extReq); extErr == nil {
		extResp.Body.Close()
		t.Fatal("offline-bundle HTTP client allowed non-loopback dial")
	}
}
