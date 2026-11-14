package composition

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestOntologyCatalogHidesTheOtherTenant(t *testing.T) {
	t.Parallel()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	ctx := t.Context()
	for _, tenant := range []struct{ environment, team, principal string }{
		{"tenant-a-use2", "tenant-a", "alice"},
		{"tenant-b-use2", "tenant-b", "bob"},
	} {
		if _, err := service.PutTeam(ctx, controlplane.Team{
			Name: tenant.team, DisplayName: tenant.team,
			AdminUsers: []string{tenant.principal}, MemberUsers: []string{tenant.principal},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutGrant(ctx, controlplane.ResourceGrant{
			ResourceRID: "environment/" + tenant.environment, Team: tenant.team,
			Operations: []string{controlplane.OperationReadResource, controlplane.OperationProposeChange},
		}); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"root"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, environment := range []string{"tenant-a-use2", "tenant-b-use2"} {
		body, marshalErr := json.Marshal(desiredState(environment, environment+":ledger"))
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		response := driveAs(t, handler, http.MethodPost, "/v1/plans", "root", string(body))
		if response.Code != http.StatusCreated {
			t.Fatalf("seed %s: %d %s", environment, response.Code, response.Body.String())
		}
	}
	loaded := driveAs(t, handler, http.MethodPost, "/api/v2/ontologies/conduit/objectSets/loadObjects", "alice",
		`{"objectSet":{"type":"base","objectType":"Environment"}}`)
	if loaded.Code != http.StatusOK {
		t.Fatalf("load = %d %s", loaded.Code, loaded.Body.String())
	}
	if strings.Contains(loaded.Body.String(), "tenant-b-use2") || !strings.Contains(loaded.Body.String(), "tenant-a-use2") {
		t.Fatalf("alice's catalog = %s", loaded.Body.String())
	}
	denied := driveAs(t, handler, http.MethodPost, "/api/v2/ontologies/conduit/actions/createChangeRequest/apply", "alice",
		`{"parameters":{"environment":"tenant-b-use2","targetKind":"ENVIRONMENT","targetId":"tenant-b-use2","changeType":"UPDATE_ENVIRONMENT_SETTINGS"},"options":{"mode":"VALIDATE_ONLY"}}`)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("alice apply on tenant-b = %d %s", denied.Code, denied.Body.String())
	}
}
