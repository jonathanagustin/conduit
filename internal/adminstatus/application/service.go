package application

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/adminstatus/domain"
	"github.com/jonathanagustin/conduit/internal/adminstatus/ports"
)

type Service struct{ Source ports.Source }

func (service Service) Read(ctx context.Context) (domain.Summary, error) {
	return service.Source.Read(ctx)
}
