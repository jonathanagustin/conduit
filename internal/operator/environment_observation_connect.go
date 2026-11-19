package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1/environmentsv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func invalidEnvironmentObservation() error {
	return fmt.Errorf("operator control plane returned invalid Environment reported state")
}

func (adapter *HTTPControlPlane) getReportedStateConnect(ctx context.Context, environment string) (controlplane.EnvironmentReport, error) {
	client := environmentsv1connect.NewEnvironmentObservationServiceClient(connect.NewClient(connecthttp.NewTransport(adapter.client, adapter.baseURL.String(), connecthttp.WithReadMaxBytes(maxOperatorResponseBytes))))
	response, err := client.GetEnvironmentReportedState(ctx, &environmentsv1.GetEnvironmentReportedStateRequest{Environment: environment})
	if err != nil {
		return controlplane.EnvironmentReport{}, err
	}
	if response == nil {
		return controlplane.EnvironmentReport{}, invalidEnvironmentObservation()
	}
	return environmentObservationFromWire(response.Reported, environment)
}

func observationWireTime(value *timestamppb.Timestamp) (time.Time, error) {
	if value == nil {
		return time.Time{}, nil
	}
	if value.CheckValid() != nil {
		return time.Time{}, invalidEnvironmentObservation()
	}
	return value.AsTime(), nil
}

func environmentObservationFromWire(report *environmentsv1.ReportedEnvironmentState, environment string) (controlplane.EnvironmentReport, error) {
	if report == nil || environment == "" || report.Environment != environment {
		return controlplane.EnvironmentReport{}, invalidEnvironmentObservation()
	}
	observed, err := observationWireTime(report.ObservedAt)
	if err != nil {
		return controlplane.EnvironmentReport{}, err
	}
	result := controlplane.EnvironmentReport{Environment: report.Environment, AgentID: report.AgentId, PlanID: report.PlanId, ObservedAt: observed, Revision: report.Revision}
	for _, entity := range report.Entities {
		if entity == nil {
			return controlplane.EnvironmentReport{}, invalidEnvironmentObservation()
		}
		observed, err := observationWireTime(entity.ObservedAt)
		if err != nil {
			return controlplane.EnvironmentReport{}, err
		}
		row := controlplane.ReportedEntity{InstallationID: entity.InstallationId, Product: entity.Product, Version: entity.Version, ReleaseDigest: entity.ReleaseDigest, Namespace: entity.Namespace, ConfigDigest: entity.ConfigDigest, Health: entity.Health, RolloutStatus: entity.RolloutStatus, ConfigStatus: entity.ConfigStatus, Lifecycle: entity.Lifecycle, ObservedAt: observed}
		if entity.HealthDetails != nil {
			row.HealthDetails = &controlplane.EntityHealthDetails{Quorum: entity.HealthDetails.Quorum}
			for _, node := range entity.HealthDetails.Nodes {
				if node == nil {
					return controlplane.EnvironmentReport{}, invalidEnvironmentObservation()
				}
				n := controlplane.EntityNodeHealth{NodeID: node.NodeId, ProductVersion: node.ProductVersion, Result: node.Result}
				for _, container := range node.Containers {
					if container == nil {
						return controlplane.EnvironmentReport{}, invalidEnvironmentObservation()
					}
					c := controlplane.EntityContainerHealth{ContainerID: container.ContainerId, Liveness: container.Liveness, Readiness: container.Readiness, Result: container.Result}
					for _, check := range container.Checks {
						if check == nil {
							return controlplane.EnvironmentReport{}, invalidEnvironmentObservation()
						}
						var params map[string]any
						decoder := json.NewDecoder(bytes.NewReader(check.ParamsJson))
						decoder.UseNumber()
						if decoder.Decode(&params) != nil {
							return controlplane.EnvironmentReport{}, invalidEnvironmentObservation()
						}
						var extra any
						if decoder.Decode(&extra) != io.EOF {
							return controlplane.EnvironmentReport{}, invalidEnvironmentObservation()
						}
						c.Checks = append(c.Checks, controlplane.HealthCheck{ID: check.Id, Message: check.Message, Result: check.Result, Params: params})
					}
					n.Containers = append(n.Containers, c)
				}
				row.HealthDetails.Nodes = append(row.HealthDetails.Nodes, n)
			}
		}
		result.Entities = append(result.Entities, row)
	}
	for _, secret := range report.Secrets {
		if secret == nil || int64(int(secret.Revision)) != secret.Revision {
			return controlplane.EnvironmentReport{}, invalidEnvironmentObservation()
		}
		result.Secrets = append(result.Secrets, controlplane.ReportedSecret{Assignment: secret.Assignment, Namespace: secret.Namespace, Revision: int(secret.Revision), ProviderRef: secret.ProviderRef, KubernetesSecretName: secret.KubernetesSecretName, Keys: append([]string(nil), secret.Keys...)})
	}
	return result, nil
}
