package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jonathanagustin/conduit/internal/spaces/domain"
	"github.com/jonathanagustin/conduit/internal/spaces/ports"
)

// Service binds storage to the caller's existing authorization. Selecting a
// Space is never a substitute for authorization of a resource in that Space.
type Service struct {
	store     ports.Store
	authority ports.Authority
	now       func() time.Time
}

func New(store ports.Store, authority ports.Authority) (*Service, error) {
	if store == nil || authority == nil {
		return nil, errors.New("Space store and caller authority are required")
	}
	return &Service{store: store, authority: authority, now: time.Now}, nil
}

func (service *Service) Create(ctx context.Context, name, description string) (domain.Space, error) {
	subject, err := service.authority.RequireCreation(ctx)
	if err != nil {
		return domain.Space{}, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return domain.Space{}, fmt.Errorf("create Space identity: %w", err)
	}
	value, err := domain.New("space_"+hex.EncodeToString(random[:]), name, description, subject, service.now())
	if err != nil {
		return domain.Space{}, err
	}
	return service.store.CreateSpace(ctx, value)
}

func (service *Service) Get(ctx context.Context, id string) (domain.Space, error) {
	if err := service.authority.RequireRead(ctx, id); err != nil {
		return domain.Space{}, err
	}
	return service.store.GetSpace(ctx, id)
}

func (service *Service) List(ctx context.Context) ([]domain.Space, error) {
	values, err := service.store.ListSpaces(ctx)
	if err != nil {
		return nil, err
	}
	visible := make([]domain.Space, 0, len(values))
	for _, value := range values {
		if err := service.authority.RequireRead(ctx, value.ID); err != nil {
			if errors.Is(err, domain.ErrForbidden) {
				continue
			}
			return nil, err
		}
		visible = append(visible, value)
	}
	return visible, nil
}

func (service *Service) Bind(ctx context.Context, spaceID string, resource domain.Resource) (domain.Binding, error) {
	subject, err := service.authority.RequireBinding(ctx, spaceID, resource)
	if err != nil {
		return domain.Binding{}, err
	}
	value, err := domain.NewBinding(spaceID, resource, subject, service.now())
	if err != nil {
		return domain.Binding{}, err
	}
	return service.store.BindResource(ctx, value)
}

func (service *Service) Lookup(ctx context.Context, resource domain.Resource) (domain.Binding, error) {
	if err := service.authority.RequireResourceRead(ctx, resource); err != nil {
		return domain.Binding{}, err
	}
	return service.store.GetBinding(ctx, resource)
}

func (service *Service) ListBindings(ctx context.Context, spaceID string) ([]domain.Binding, error) {
	if err := service.authority.RequireRead(ctx, spaceID); err != nil {
		return nil, err
	}
	values, err := service.store.ListBindings(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	visible := make([]domain.Binding, 0, len(values))
	for _, value := range values {
		if err := service.authority.RequireResourceRead(ctx, value.Resource); err != nil {
			if errors.Is(err, domain.ErrForbidden) {
				continue
			}
			return nil, err
		}
		visible = append(visible, value)
	}
	return visible, nil
}

func (service *Service) RequireScope(ctx context.Context, spaceID string, resource domain.Resource) error {
	value, err := service.Lookup(ctx, resource)
	if err != nil {
		return err
	}
	if value.SpaceID != spaceID {
		return domain.ErrForbidden
	}
	return nil
}
