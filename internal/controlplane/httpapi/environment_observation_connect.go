package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1/environmentsv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

const EnvironmentObservationConnectRoute = "POST /conduit.environments.v1.EnvironmentObservationService/GetEnvironmentReportedState"

// EnvironmentObservationConfig binds the original authenticated carrier to a
// shared authorized read. The adapter receives no report-write capability.
type EnvironmentObservationConfig struct {
	Authenticate func(*http.Request) (context.Context, error)
	Read         func(context.Context, string) (controlplane.EnvironmentReport, error)
}

// RegisterEnvironmentObservationConnect serves only the contracted POST.
// Authentication precedes decoding, including malformed or unsupported bodies.
func RegisterEnvironmentObservationConnect(mux *http.ServeMux, cfg EnvironmentObservationConfig) {
	server := connect.NewServer()
	environmentsv1connect.RegisterEnvironmentObservationServiceHandler(server, &environmentObservationService{cfg: cfg})
	rpc := http.NewServeMux()
	connecthttp.Mount(rpc, server, connecthttp.WithReadMaxBytes(1<<20))
	mux.Handle(EnvironmentObservationConnectRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ctx context.Context
		err := controlplane.ErrUnauthorized
		if cfg.Authenticate != nil {
			ctx, err = cfg.Authenticate(r)
		}
		if err != nil || ctx == nil {
			code := connect.CodeInternal
			if err == nil || errors.Is(err, controlplane.ErrUnauthorized) {
				code = connect.CodeUnauthenticated
			} else if errors.Is(err, controlplane.ErrForbidden) {
				code = connect.CodePermissionDenied
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_ = connecthttp.NewErrorWriter().Write(w, r, connect.NewError(code, code.String()))
			return
		}
		rpc.ServeHTTP(w, r.WithContext(ctx))
	}))
}

type environmentObservationService struct{ cfg EnvironmentObservationConfig }

func (service *environmentObservationService) GetEnvironmentReportedState(ctx context.Context, request *environmentsv1.GetEnvironmentReportedStateRequest) (*environmentsv1.GetEnvironmentReportedStateResponse, error) {
	if request.Environment == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "Environment is required")
	}
	if service.cfg.Read == nil {
		return nil, connect.NewError(connect.CodeUnavailable, "Environment reported state unavailable")
	}
	report, err := service.cfg.Read(ctx, request.Environment)
	if err != nil {
		code := connect.CodeInternal
		switch {
		case errors.Is(err, controlplane.ErrUnauthorized):
			code = connect.CodeUnauthenticated
		case errors.Is(err, controlplane.ErrForbidden):
			code = connect.CodePermissionDenied
		case errors.Is(err, controlplane.ErrNotFound):
			code = connect.CodeNotFound
		}
		return nil, connect.NewError(code, "Environment reported state unavailable")
	}
	result, err := environmentObservationMessage(report)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "Environment reported state unavailable")
	}
	return &environmentsv1.GetEnvironmentReportedStateResponse{Reported: result}, nil
}

func environmentObservationMessage(report controlplane.EnvironmentReport) (*environmentsv1.ReportedEnvironmentState, error) {
	observed, err := environmentSummaryTimestamp(report.ObservedAt)
	if err != nil {
		return nil, err
	}
	result := &environmentsv1.ReportedEnvironmentState{Environment: nativeIndexString(report.Environment), AgentId: nativeIndexString(report.AgentID), PlanId: nativeIndexString(report.PlanID), ObservedAt: observed, Revision: report.Revision}
	for _, entity := range report.Entities {
		observed, err := environmentSummaryTimestamp(entity.ObservedAt)
		if err != nil {
			return nil, err
		}
		row := &environmentsv1.ReportedEntity{InstallationId: nativeIndexString(entity.InstallationID), Product: nativeIndexString(entity.Product), Version: nativeIndexString(entity.Version), ReleaseDigest: nativeIndexString(entity.ReleaseDigest), Namespace: nativeIndexString(entity.Namespace), ConfigDigest: nativeIndexString(entity.ConfigDigest), Health: nativeIndexString(entity.Health), RolloutStatus: nativeIndexString(entity.RolloutStatus), ConfigStatus: nativeIndexString(entity.ConfigStatus), Lifecycle: nativeIndexString(entity.Lifecycle), ObservedAt: observed}
		if entity.HealthDetails != nil {
			row.HealthDetails = &environmentsv1.EntityHealthDetails{Quorum: nativeIndexString(entity.HealthDetails.Quorum)}
			for _, node := range entity.HealthDetails.Nodes {
				n := &environmentsv1.EntityNodeHealth{NodeId: nativeIndexString(node.NodeID), ProductVersion: nativeIndexString(node.ProductVersion), Result: nativeIndexString(node.Result)}
				for _, container := range node.Containers {
					c := &environmentsv1.EntityContainerHealth{ContainerId: nativeIndexString(container.ContainerID), Liveness: container.Liveness, Readiness: container.Readiness, Result: nativeIndexString(container.Result)}
					for _, check := range container.Checks {
						params, err := json.Marshal(check.Params)
						if err != nil {
							return nil, err
						}
						c.Checks = append(c.Checks, &environmentsv1.HealthCheck{Id: nativeIndexString(check.ID), Message: nativeIndexString(check.Message), Result: nativeIndexString(check.Result), ParamsJson: params})
					}
					n.Containers = append(n.Containers, c)
				}
				row.HealthDetails.Nodes = append(row.HealthDetails.Nodes, n)
			}
		}
		result.Entities = append(result.Entities, row)
	}
	for _, secret := range report.Secrets {
		keys := make([]string, 0, len(secret.Keys))
		for _, key := range secret.Keys {
			keys = append(keys, nativeIndexString(key))
		}
		result.Secrets = append(result.Secrets, &environmentsv1.ReportedSecretReference{Assignment: nativeIndexString(secret.Assignment), Namespace: nativeIndexString(secret.Namespace), Revision: int64(secret.Revision), ProviderRef: nativeIndexString(secret.ProviderRef), KubernetesSecretName: nativeIndexString(secret.KubernetesSecretName), Keys: keys})
	}
	return result, nil
}
