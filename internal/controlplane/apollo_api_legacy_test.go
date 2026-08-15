package controlplane

// These wire-shaped helpers keep the historical mutation tests readable while
// production translation lives in internal/apollo/application. They are test
// fixtures only; no control-plane binary contains foreign DTO duplicates.

import (
	"time"

	"github.com/jonathanagustin/conduit/internal/apollo"
	apolloapp "github.com/jonathanagustin/conduit/internal/apollo/application"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

type apolloProduct struct {
	ID    string `json:"id"`
	Group string `json:"group"`
}

type apolloRelease struct {
	MavenCoordinate     string                    `json:"mavenCoordinate"`
	ReleaseChannels     []string                  `json:"releaseChannels,omitempty"`
	UpgradeStrategy     string                    `json:"upgradeStrategy,omitempty"`
	RecallStatus        apolloRecallStatus        `json:"recallStatus"`
	ProductDependencies []apolloProductDependency `json:"productDependencies,omitempty"`
}

type apolloRecallStatus struct {
	Recalled bool `json:"recalled"`
}

type apolloProductDependency struct {
	ProductID  string `json:"productId"`
	MinVersion string `json:"minVersion,omitempty"`
	MaxVersion string `json:"maxVersion,omitempty"`
	Optional   bool   `json:"optional,omitempty"`
}

type apolloEnvironment struct {
	RID             string `json:"rid"`
	Name            string `json:"name"`
	EnvironmentType string `json:"environmentType,omitempty"`
	Status          string `json:"status"`
}

type apolloInstallation struct {
	RID                 string   `json:"rid"`
	EnvironmentRID      string   `json:"environmentRid"`
	ApolloEntityID      string   `json:"apolloEntityId"`
	PackageRID          string   `json:"packageRid"`
	ProductID           string   `json:"productId"`
	ReleaseChannel      string   `json:"releaseChannel,omitempty"`
	IgnoredDependencies []string `json:"ignoredDependencies,omitempty"`
	Version             string   `json:"version,omitempty"`
}

type apolloNamespace struct {
	ID      string `json:"id"`
	Owners  []any  `json:"owners"`
	Editors []any  `json:"editors"`
	Viewers []any  `json:"viewers"`
}

type apolloNodeInfo struct {
	CreationTimestamp time.Time `json:"creationTimestamp"`
	HostID            string    `json:"hostId"`
	NodeID            string    `json:"nodeId"`
	ProductVersion    string    `json:"productVersion"`
}

type apolloEntityInfo struct {
	EntityType   string           `json:"entityType"`
	ID           string           `json:"id"`
	ProductGroup string           `json:"productGroup"`
	ProductName  string           `json:"productName"`
	K8sNamespace string           `json:"k8sNamespace,omitempty"`
	NodesInfo    []apolloNodeInfo `json:"nodesInfo,omitempty"`
	Quorum       string           `json:"quorum,omitempty"`
}

type apolloContainerStatus struct {
	ContainerID string        `json:"containerId"`
	Checks      []HealthCheck `json:"checks,omitempty"`
	Liveness    bool          `json:"liveness"`
	Readiness   bool          `json:"readiness"`
}

type apolloEntityHealth struct {
	ProductGroup      string                  `json:"productGroup"`
	ProductName       string                  `json:"productName"`
	ProductVersion    string                  `json:"productVersion"`
	NodeID            string                  `json:"nodeId"`
	ContainerStatuses []apolloContainerStatus `json:"containerStatuses,omitempty"`
}

func validateApolloEntityInfo(values []apolloEntityInfo) error {
	converted := make([]apollo.EntityInfo, 0, len(values))
	for _, value := range values {
		converted = append(converted, generatedEntityInfo(value))
	}
	return apolloapp.ValidateEntityInfo(converted)
}

func validateApolloEntityHealth(values []apolloEntityHealth) error {
	return apolloapp.ValidateEntityHealth(generatedEntityHealth(values))
}

func aggregateApolloHealth(quorum string, values []apolloEntityHealth) (EntityHealthDetails, string) {
	var generatedQuorum *apollo.Quorum
	if quorum != "" {
		value := apollo.Quorum(quorum)
		generatedQuorum = &value
	}
	return apolloapp.AggregateHealth(generatedQuorum, generatedEntityHealth(values))
}

func matchApolloInfo(values []apolloEntityInfo, step PlanStep) apolloEntityInfo {
	converted := make([]apollo.EntityInfo, 0, len(values))
	for _, value := range values {
		converted = append(converted, generatedEntityInfo(value))
	}
	matched := apolloapp.MatchInfo(converted, environments.PlanStepProjection{
		InstallationID: step.InstallationID, Entity: step.Entity, Product: step.Product,
		Version: step.Version, Namespace: step.Namespace,
	})
	return legacyEntityInfo(matched)
}

func matchApolloHealth(values []apolloEntityHealth, info apolloEntityInfo, product string) []apolloEntityHealth {
	matched := apolloapp.MatchHealth(generatedEntityHealth(values), generatedEntityInfo(info), product)
	result := make([]apolloEntityHealth, 0, len(matched))
	for _, value := range matched {
		result = append(result, legacyEntityHealth(value))
	}
	return result
}

func generatedEntityInfo(value apolloEntityInfo) apollo.EntityInfo {
	nodes := make([]apollo.NodeInfo, 0, len(value.NodesInfo))
	for _, node := range value.NodesInfo {
		nodes = append(nodes, apollo.NodeInfo{
			CreationTimestamp: node.CreationTimestamp, HostId: node.HostID, NodeId: node.NodeID, ProductVersion: node.ProductVersion,
		})
	}
	result := apollo.EntityInfo{
		EntityType: apollo.EntityType(value.EntityType), Id: value.ID, ProductGroup: value.ProductGroup,
		ProductName: value.ProductName, NodesInfo: &nodes,
	}
	if value.K8sNamespace != "" {
		namespace := apollo.K8sNamespace(value.K8sNamespace)
		result.K8sNamespace = &namespace
	}
	if value.Quorum != "" {
		quorum := apollo.Quorum(value.Quorum)
		result.Quorum = &quorum
	}
	return result
}

func legacyEntityInfo(value apollo.EntityInfo) apolloEntityInfo {
	result := apolloEntityInfo{
		EntityType: string(value.EntityType), ID: value.Id, ProductGroup: value.ProductGroup, ProductName: value.ProductName,
	}
	if value.K8sNamespace != nil {
		result.K8sNamespace = *value.K8sNamespace
	}
	if value.Quorum != nil {
		result.Quorum = string(*value.Quorum)
	}
	if value.NodesInfo != nil {
		for _, node := range *value.NodesInfo {
			result.NodesInfo = append(result.NodesInfo, apolloNodeInfo{
				CreationTimestamp: node.CreationTimestamp, HostID: node.HostId, NodeID: node.NodeId, ProductVersion: node.ProductVersion,
			})
		}
	}
	return result
}

func generatedEntityHealth(values []apolloEntityHealth) []apollo.EntityHealth {
	result := make([]apollo.EntityHealth, 0, len(values))
	for _, value := range values {
		containers := make([]apollo.ContainerStatus, 0, len(value.ContainerStatuses))
		for _, container := range value.ContainerStatuses {
			checks := make([]apollo.HealthCheck, 0, len(container.Checks))
			for _, check := range container.Checks {
				checks = append(checks, apollo.HealthCheck{
					Id: check.ID, Message: check.Message, Params: check.Params, Result: apollo.HealthCheckResult(check.Result),
				})
			}
			containers = append(containers, apollo.ContainerStatus{
				ContainerId: container.ContainerID, Checks: &checks, Liveness: container.Liveness, Readiness: container.Readiness,
			})
		}
		result = append(result, apollo.EntityHealth{
			ProductGroup: value.ProductGroup, ProductName: value.ProductName, ProductVersion: value.ProductVersion,
			NodeId: value.NodeID, ContainerStatuses: &containers,
		})
	}
	return result
}

func legacyEntityHealth(value apollo.EntityHealth) apolloEntityHealth {
	result := apolloEntityHealth{
		ProductGroup: value.ProductGroup, ProductName: value.ProductName, ProductVersion: value.ProductVersion, NodeID: value.NodeId,
	}
	if value.ContainerStatuses != nil {
		for _, container := range *value.ContainerStatuses {
			legacy := apolloContainerStatus{ContainerID: container.ContainerId, Liveness: container.Liveness, Readiness: container.Readiness}
			if container.Checks != nil {
				for _, check := range *container.Checks {
					legacy.Checks = append(legacy.Checks, HealthCheck{
						ID: check.Id, Message: check.Message, Params: check.Params, Result: string(check.Result),
					})
				}
			}
			result.ContainerStatuses = append(result.ContainerStatuses, legacy)
		}
	}
	return result
}
