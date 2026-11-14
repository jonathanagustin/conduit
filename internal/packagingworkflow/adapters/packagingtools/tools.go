// Package packagingtools implements the packaging toolbox port.
package packagingtools

import (
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// Service is the driven adapter behind domain.Toolbox.
type Service struct {
	client   *http.Client
	baseURL  string // optional override used by tests, for example an httptest server
	repoRoot string
}

// Option configures Service.
type Option func(*Service)

// WithHTTPClient replaces the default HTTP client used to inspect images.
func WithHTTPClient(client *http.Client) Option {
	return func(service *Service) {
		if client != nil {
			service.client = client
		}
	}
}

// WithRegistryBaseURL forces inspect requests to this registry origin.
// Tests use it to point at a local server. Production leaves it empty so
// the image ref's host selects the registry.
func WithRegistryBaseURL(baseURL string) Option {
	return func(service *Service) {
		service.baseURL = baseURL
	}
}

// New returns a toolbox. The default client has a 15-second timeout.
func New(options ...Option) *Service {
	service := &Service{client: &http.Client{Timeout: 15 * time.Second}}
	for _, option := range options {
		option(service)
	}
	return service
}

var _ domain.Toolbox = (*Service)(nil)
