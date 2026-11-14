package controlplane

import (
	"context"
	"testing"
)

func TestProposeHumanChangeStaysPending(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	request, err := service.ProposeHumanChange(context.Background(), "PLAN", "sample-app-1:memory", "dev", []EntityChange{{
		ID: "memory-limit", Type: ChangeTypeUpdateEntity, InstallationID: "sample-app",
		ReferencedResources: map[string]ReferencedResource{
			"environment":  {RID: "environment/dev", Name: "dev"},
			"installation": {RID: "installation/dev/sample-app", Name: "sample-app"},
			"product":      {RID: "product/app.example:sample-app", Name: "sample-app"},
		},
	}}, Principal{Username: "operator"}, "Proposed memory limit 512Mi", "headroom")
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != ChangePendingApprovals || len(request.Reviews) != 0 {
		t.Fatalf("request = %#v", request)
	}
}
