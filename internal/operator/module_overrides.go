package operator

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/jonathanagustin/conduit/internal/installoverride/ports"
)

type ModuleOverrideControlPlane interface {
	ProposeModuleOverride(context.Context, ports.ModuleDraft, bool) (ports.ModuleReview, error)
	ApplyModuleOverride(context.Context, string) (ports.ModuleAdmission, error)
}

func (service Service) ProposeModuleOverride(ctx context.Context, draft ports.ModuleDraft, dryRun bool) (ports.ModuleReview, error) {
	port, ok := service.controlPlane.(ModuleOverrideControlPlane)
	if !ok {
		return ports.ModuleReview{}, errors.New("reviewed Module overrides are unavailable from this service")
	}
	return port.ProposeModuleOverride(ctx, draft, dryRun)
}
func (service Service) ApplyModuleOverride(ctx context.Context, id string) (ports.ModuleAdmission, error) {
	port, ok := service.controlPlane.(ModuleOverrideControlPlane)
	if !ok {
		return ports.ModuleAdmission{}, errors.New("reviewed Module overrides are unavailable from this service")
	}
	return port.ApplyModuleOverride(ctx, id)
}

func (adapter *HTTPControlPlane) ProposeModuleOverride(ctx context.Context, draft ports.ModuleDraft, dryRun bool) (ports.ModuleReview, error) {
	var result ports.ModuleReview
	err := adapter.request(ctx, http.MethodPost, "/v1/module-override-proposals", struct {
		Draft  ports.ModuleDraft `json:"draft"`
		DryRun bool              `json:"dryRun"`
	}{draft, dryRun}, &result)
	return result, err
}
func (adapter *HTTPControlPlane) ApplyModuleOverride(ctx context.Context, id string) (ports.ModuleAdmission, error) {
	var result ports.ModuleAdmission
	err := adapter.request(ctx, http.MethodPost, "/v1/module-override-proposals/"+url.PathEscape(id)+"/apply", nil, &result)
	return result, err
}
