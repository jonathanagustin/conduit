package operator

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/jonathanagustin/conduit/internal/spaces/domain"
)

// SpaceControlPlane is the optional native grouping management port. It does
// not extend the captured Apollo client or assign resource permissions.
type SpaceControlPlane interface {
	CreateSpace(context.Context, string, string) (domain.Space, error)
	GetSpace(context.Context, string) (domain.Space, error)
	ListSpaces(context.Context) ([]domain.Space, error)
	BindSpaceResource(context.Context, string, domain.Resource) (domain.Binding, error)
	ListSpaceBindings(context.Context, string) ([]domain.Binding, error)
}

func (service Service) spaceControlPlane() (SpaceControlPlane, error) {
	port, ok := service.controlPlane.(SpaceControlPlane)
	if !ok {
		return nil, errors.New("native Space management is unavailable from this transport")
	}
	return port, nil
}
func (service Service) CreateSpace(ctx context.Context, name, description string) (domain.Space, error) {
	port, err := service.spaceControlPlane()
	if err != nil {
		return domain.Space{}, err
	}
	return port.CreateSpace(ctx, name, description)
}
func (service Service) GetSpace(ctx context.Context, id string) (domain.Space, error) {
	port, err := service.spaceControlPlane()
	if err != nil {
		return domain.Space{}, err
	}
	return port.GetSpace(ctx, id)
}
func (service Service) ListSpaces(ctx context.Context) ([]domain.Space, error) {
	port, err := service.spaceControlPlane()
	if err != nil {
		return nil, err
	}
	return port.ListSpaces(ctx)
}
func (service Service) BindSpaceResource(ctx context.Context, id string, resource domain.Resource) (domain.Binding, error) {
	port, err := service.spaceControlPlane()
	if err != nil {
		return domain.Binding{}, err
	}
	return port.BindSpaceResource(ctx, id, resource)
}
func (service Service) ListSpaceBindings(ctx context.Context, id string) ([]domain.Binding, error) {
	port, err := service.spaceControlPlane()
	if err != nil {
		return nil, err
	}
	return port.ListSpaceBindings(ctx, id)
}
func (adapter *HTTPControlPlane) CreateSpace(ctx context.Context, name, description string) (domain.Space, error) {
	var result domain.Space
	err := adapter.request(ctx, http.MethodPost, "/v1/spaces", map[string]string{"name": name, "description": description}, &result)
	return result, err
}
func (adapter *HTTPControlPlane) GetSpace(ctx context.Context, id string) (domain.Space, error) {
	var result domain.Space
	err := adapter.request(ctx, http.MethodGet, "/v1/spaces/"+url.PathEscape(id), nil, &result)
	return result, err
}
func (adapter *HTTPControlPlane) ListSpaces(ctx context.Context) ([]domain.Space, error) {
	var result struct {
		Spaces []domain.Space `json:"spaces"`
	}
	err := adapter.request(ctx, http.MethodGet, "/v1/spaces", nil, &result)
	return result.Spaces, err
}
func (adapter *HTTPControlPlane) BindSpaceResource(ctx context.Context, id string, resource domain.Resource) (domain.Binding, error) {
	var result domain.Binding
	err := adapter.request(ctx, http.MethodPut, "/v1/spaces/"+url.PathEscape(id)+"/bindings", resource, &result)
	return result, err
}
func (adapter *HTTPControlPlane) ListSpaceBindings(ctx context.Context, id string) ([]domain.Binding, error) {
	var result struct {
		Bindings []domain.Binding `json:"bindings"`
	}
	err := adapter.request(ctx, http.MethodGet, "/v1/spaces/"+url.PathEscape(id)+"/bindings", nil, &result)
	return result.Bindings, err
}

// SetSpaceSelection configures relevant native listing/publication calls. It
// neither changes credentials nor applies a resource grant.
func (adapter *HTTPControlPlane) SetSpaceSelection(id string) error {
	if id != "" && !domain.ValidID(id) {
		return errors.New("expected a native Space identity")
	}
	adapter.spaceID = id
	return nil
}
func (adapter *HTTPControlPlane) withSpaceSelection(path string) string {
	if adapter.spaceID == "" {
		return path
	}
	return path + "?spaceId=" + url.QueryEscape(adapter.spaceID)
}
