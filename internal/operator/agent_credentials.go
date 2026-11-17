package operator

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

// AgentCredentialView is one Agent's last presented certificate as the Hub
// reports it. Declared here rather than imported from the Hub's HTTP adapter:
// a client depends on the wire shape, not on the server package that writes it.
type AgentCredentialView struct {
	Environment        string    `json:"environment"`
	AgentID            string    `json:"agentId"`
	Serial             string    `json:"serial"`
	NotBefore          time.Time `json:"notBefore"`
	NotAfter           time.Time `json:"notAfter"`
	LastSeenAt         time.Time `json:"lastSeenAt"`
	SecondsUntilExpiry int64     `json:"secondsUntilExpiry"`
	RevokedBy          string    `json:"revokedBy,omitempty"`
}

// AgentCredentialControlPlane is the optional native port over the Hub's Agent
// deny-list. It revokes and lifts; it issues nothing.
type AgentCredentialControlPlane interface {
	ListAgentRevocations(ctx context.Context, environment string) ([]domain.AgentRevocation, error)
	AddAgentRevocation(ctx context.Context, environment string, draft domain.AgentRevocation) (domain.AgentRevocation, error)
	RemoveAgentRevocation(ctx context.Context, environment, id string) (domain.AgentRevocation, error)
	ListAgentCredentials(ctx context.Context, environment string) ([]AgentCredentialView, error)
}

func (service Service) agentCredentialControlPlane() (AgentCredentialControlPlane, error) {
	port, ok := service.controlPlane.(AgentCredentialControlPlane)
	if !ok {
		return nil, errors.New("Agent credential management is unavailable from this transport")
	}
	return port, nil
}

func (service Service) ListAgentRevocations(ctx context.Context, environment string) ([]domain.AgentRevocation, error) {
	port, err := service.agentCredentialControlPlane()
	if err != nil {
		return nil, err
	}
	return port.ListAgentRevocations(ctx, environment)
}

func (service Service) AddAgentRevocation(ctx context.Context, environment string, draft domain.AgentRevocation) (domain.AgentRevocation, error) {
	port, err := service.agentCredentialControlPlane()
	if err != nil {
		return domain.AgentRevocation{}, err
	}
	return port.AddAgentRevocation(ctx, environment, draft)
}

func (service Service) RemoveAgentRevocation(ctx context.Context, environment, id string) (domain.AgentRevocation, error) {
	port, err := service.agentCredentialControlPlane()
	if err != nil {
		return domain.AgentRevocation{}, err
	}
	return port.RemoveAgentRevocation(ctx, environment, id)
}

func (service Service) ListAgentCredentials(ctx context.Context, environment string) ([]AgentCredentialView, error) {
	port, err := service.agentCredentialControlPlane()
	if err != nil {
		return nil, err
	}
	return port.ListAgentCredentials(ctx, environment)
}

func agentRevocationsPath(environment string) string {
	return "/v1/environments/" + url.PathEscape(environment) + "/agent-revocations"
}

func (adapter *HTTPControlPlane) ListAgentRevocations(ctx context.Context, environment string) ([]domain.AgentRevocation, error) {
	var result struct {
		Revocations []domain.AgentRevocation `json:"revocations"`
	}
	err := adapter.request(ctx, http.MethodGet, agentRevocationsPath(environment), nil, &result)
	return result.Revocations, err
}

func (adapter *HTTPControlPlane) AddAgentRevocation(ctx context.Context, environment string, draft domain.AgentRevocation) (domain.AgentRevocation, error) {
	// Only what an operator chooses is sent. The Hub derives the identifier,
	// the author and the creation time, and refuses a body that names more.
	body := map[string]any{"scope": draft.Scope, "reason": draft.Reason}
	if draft.AgentID != "" {
		body["agentId"] = draft.AgentID
	}
	if draft.Serial != "" {
		body["serial"] = draft.Serial
	}
	if !draft.NotBefore.IsZero() {
		body["notBefore"] = draft.NotBefore.UTC().Format(time.RFC3339Nano)
	}
	var result domain.AgentRevocation
	err := adapter.request(ctx, http.MethodPost, agentRevocationsPath(environment), body, &result)
	return result, err
}

func (adapter *HTTPControlPlane) RemoveAgentRevocation(ctx context.Context, environment, id string) (domain.AgentRevocation, error) {
	var result domain.AgentRevocation
	err := adapter.request(ctx, http.MethodDelete, agentRevocationsPath(environment)+"/"+url.PathEscape(id), nil, &result)
	return result, err
}

func (adapter *HTTPControlPlane) ListAgentCredentials(ctx context.Context, environment string) ([]AgentCredentialView, error) {
	var result struct {
		AgentCredentials []AgentCredentialView `json:"agentCredentials"`
	}
	err := adapter.request(ctx, http.MethodGet, "/v1/environments/"+url.PathEscape(environment)+"/agent-credentials", nil, &result)
	return result.AgentCredentials, err
}
