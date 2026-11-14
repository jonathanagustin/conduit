package operator

import (
	"context"
	"errors"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/delivery/maintenance"
)

type TemporaryMaintenanceControlPlane interface {
	CreateMaintenanceOverrides(context.Context, maintenance.OverrideDraft) (maintenance.OverrideBatch, error)
}

func (service Service) CreateMaintenanceOverrides(ctx context.Context, draft maintenance.OverrideDraft) (maintenance.OverrideBatch, error) {
	port, ok := service.controlPlane.(TemporaryMaintenanceControlPlane)
	if !ok {
		return maintenance.OverrideBatch{}, errors.New("temporary Entity maintenance overrides unavailable")
	}
	return port.CreateMaintenanceOverrides(ctx, draft)
}
func (adapter *HTTPControlPlane) CreateMaintenanceOverrides(ctx context.Context, draft maintenance.OverrideDraft) (maintenance.OverrideBatch, error) {
	var result maintenance.OverrideBatch
	err := adapter.request(ctx, http.MethodPost, "/v1/temporary-maintenance-overrides", draft, &result)
	return result, err
}
