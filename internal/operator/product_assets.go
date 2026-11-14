package operator

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	assetapp "github.com/jonathanagustin/conduit/internal/productassets/application"
	assets "github.com/jonathanagustin/conduit/internal/productassets/domain"
)

// DownloadControlPlane is the optional native download catalogue port. It
// grants no signature verification, local installation or Agent execution.
type DownloadControlPlane interface {
	PublishDownloadRelease(context.Context, assets.Manifest) (assetapp.PublicationResult, error)
	GetDownloadRelease(context.Context, string, string) (assetapp.Published, error)
	ListDownloadReleases(context.Context, string) ([]assetapp.Published, error)
}

func (service Service) downloadControlPlane() (DownloadControlPlane, error) {
	p, ok := service.controlPlane.(DownloadControlPlane)
	if !ok {
		return nil, errors.New("native download-asset catalogue is unavailable from this transport")
	}
	return p, nil
}
func (service Service) PublishDownloadRelease(ctx context.Context, m assets.Manifest) (assetapp.PublicationResult, error) {
	p, e := service.downloadControlPlane()
	if e != nil {
		return assetapp.PublicationResult{}, e
	}
	return p.PublishDownloadRelease(ctx, m)
}
func (service Service) GetDownloadRelease(ctx context.Context, product, version string) (assetapp.Published, error) {
	p, e := service.downloadControlPlane()
	if e != nil {
		return assetapp.Published{}, e
	}
	return p.GetDownloadRelease(ctx, product, version)
}
func (service Service) ListDownloadReleases(ctx context.Context, product string) ([]assetapp.Published, error) {
	p, e := service.downloadControlPlane()
	if e != nil {
		return nil, e
	}
	return p.ListDownloadReleases(ctx, product)
}
func (adapter *HTTPControlPlane) PublishDownloadRelease(ctx context.Context, m assets.Manifest) (assetapp.PublicationResult, error) {
	var r assetapp.PublicationResult
	e := adapter.request(ctx, http.MethodPost, adapter.withSpaceSelection("/v1/product-publications/download-assets"), m, &r)
	return r, e
}
func (adapter *HTTPControlPlane) GetDownloadRelease(ctx context.Context, product, version string) (assetapp.Published, error) {
	var r assetapp.Published
	e := adapter.request(ctx, http.MethodGet, "/v1/products/"+url.PathEscape(product)+"/releases/"+url.PathEscape(version)+"/download-assets", nil, &r)
	return r, e
}
func (adapter *HTTPControlPlane) ListDownloadReleases(ctx context.Context, product string) ([]assetapp.Published, error) {
	var r struct {
		Releases []assetapp.Published `json:"releases"`
	}
	e := adapter.request(ctx, http.MethodGet, "/v1/products/"+url.PathEscape(product)+"/download-releases", nil, &r)
	return r.Releases, e
}
