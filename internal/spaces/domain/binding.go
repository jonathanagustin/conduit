package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	Environment = "ENVIRONMENT"
	Product     = "PRODUCT"
	Bundle      = "BUNDLE"
)

// Resource names a stable grouping anchor. Entity and Release membership is
// inherited from their owning Environment and Product, not assigned twice.
type Resource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (resource Resource) Validate() error {
	switch resource.Kind {
	case Environment, Product, Bundle:
	default:
		return fmt.Errorf("%w: unsupported Space resource kind", ErrInvalid)
	}
	if resource.ID == "" || resource.ID != strings.TrimSpace(resource.ID) || len(resource.ID) > 1024 || strings.ContainsFunc(resource.ID, unicode.IsControl) {
		return fmt.Errorf("%w: resource identity is required", ErrInvalid)
	}
	return nil
}

// Binding is immutable native assignment intent. It grants no resource access
// and does not assert that a referenced resource is live or deployed.
type Binding struct {
	SpaceID   string    `json:"spaceId"`
	Resource  Resource  `json:"resource"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	ValueTier string    `json:"valueTier"`
}

func NewBinding(spaceID string, resource Resource, subject string, now time.Time) (Binding, error) {
	value := Binding{SpaceID: spaceID, Resource: resource, CreatedBy: subject, CreatedAt: now.UTC(), ValueTier: "CONDUIT_NATIVE"}
	return value, value.Validate()
}
func (binding Binding) Validate() error {
	if !ValidID(binding.SpaceID) {
		return fmt.Errorf("%w: expected a Conduit Space identity", ErrInvalid)
	}
	if err := binding.Resource.Validate(); err != nil {
		return err
	}
	if binding.CreatedBy == "" || binding.CreatedBy != strings.TrimSpace(binding.CreatedBy) || strings.ContainsFunc(binding.CreatedBy, unicode.IsControl) || binding.CreatedAt.IsZero() {
		return fmt.Errorf("%w: assignment provenance is required", ErrInvalid)
	}
	if binding.ValueTier != "CONDUIT_NATIVE" {
		return fmt.Errorf("%w: native provenance is required", ErrInvalid)
	}
	return nil
}
