package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestArtifactRegistryCatalogProjectsLatestDesiredAssignments(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	now := time.Now().UTC()
	for index, state := range []DesiredState{
		{Environment: Environment{ID: "dev", ArtifactRegistries: []ArtifactRegistryAssignment{{ID: "old", URL: "registry.old.test", Revision: 1}}}},
		{Environment: Environment{ID: "dev", ArtifactRegistries: []ArtifactRegistryAssignment{{ID: "primary", URL: "registry.example.test", Revision: 2, CredentialRef: "provider/ref"}}}},
	} {
		state.Environment.Generation = strconv.Itoa(index + 1)
		_, err := repository.CreatePlan(t.Context(), PlanRecord{
			ID: "plan_" + state.Environment.Generation, DesiredState: &state,
			Plan:   Plan{Environment: "dev", Generation: state.Environment.Generation},
			Status: PlanAwaitingApproval, CreatedAt: now.Add(time.Duration(index) * time.Minute), UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	request := httptest.NewRequest(http.MethodGet, "/v1/artifact-registries", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		ArtifactRegistries []ArtifactRegistryRecord `json:"artifactRegistries"`
	}
	if err := json.NewDecoder(bytes.NewReader(response.Body.Bytes())).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.ArtifactRegistries) != 1 || body.ArtifactRegistries[0].Assignment.ID != "primary" ||
		body.ArtifactRegistries[0].Assignment.CredentialRef != "provider/ref" {
		t.Fatalf("records=%#v", body.ArtifactRegistries)
	}
}
