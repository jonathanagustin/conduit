package repository

import (
	"sort"
	"strconv"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/ontology/domain"
)

type builder struct {
	allow        func(string) bool
	environments map[string]domain.Object
	objects      []domain.Object
	links        []domain.Link
	seen         map[string]bool
}

func newBuilder(allow func(string) bool) *builder {
	return &builder{
		allow: allow, environments: map[string]domain.Object{}, seen: map[string]bool{},
	}
}

func (builder *builder) visible(environmentID string) bool {
	environmentID = strings.TrimSpace(environmentID)
	return environmentID != "" && builder.allow(environmentID)
}

func (builder *builder) addPlan(record controlplane.PlanRecord) {
	environmentID := record.Plan.Environment
	generation := record.Plan.Generation
	var channelName string
	if record.DesiredState != nil {
		if record.DesiredState.Environment.ID != "" {
			environmentID = record.DesiredState.Environment.ID
		}
		if record.DesiredState.Environment.Generation != "" {
			generation = record.DesiredState.Environment.Generation
		}
		channelName = record.DesiredState.Environment.DefaultReleaseChannel
		builder.addEnvironment(record.DesiredState.Environment)
	}
	if !builder.visible(environmentID) {
		return
	}
	builder.addEnvironment(controlplane.Environment{ID: environmentID, Generation: generation, DefaultReleaseChannel: channelName})
	builder.put(domain.Object{
		Type: "Plan", PrimaryKey: record.ID,
		Properties: map[string]string{
			"id": record.ID, "environment": environmentID, "status": string(record.Status),
			"generation": generation, "changeRequestId": record.ChangeRequestID,
		},
	})
	builder.link("plans", "Environment", environmentID, "Plan", record.ID)
	builder.link("environment", "Plan", record.ID, "Environment", environmentID)
	if record.ChangeRequestID != "" {
		builder.link("changeRequest", "Plan", record.ID, "ChangeRequest", record.ChangeRequestID)
		builder.link("plan", "ChangeRequest", record.ChangeRequestID, "Plan", record.ID)
	}
	if record.DesiredState == nil {
		return
	}
	for _, channel := range record.DesiredState.ReleaseChannels {
		builder.addChannel(environmentID, channel.Name, channel.Description, channel.Archived)
	}
	if channelName != "" {
		builder.addChannel(environmentID, channelName, "", false)
	}
	for _, entity := range record.DesiredState.Entities {
		if entity.ReleaseChannel != "" {
			builder.addChannel(environmentID, entity.ReleaseChannel, "", false)
		}
	}
	for _, release := range record.DesiredState.Releases {
		builder.addArtifact(record.ID, release.Product, release.Version, release.Chart)
		for _, image := range release.Images {
			builder.addArtifact(record.ID, release.Product, release.Version, image)
		}
	}
}

func (builder *builder) addChange(request controlplane.ChangeRequest) {
	if !builder.visible(request.Environment) {
		return
	}
	builder.addEnvironment(controlplane.Environment{ID: request.Environment})
	planID := ""
	if builder.seen["Plan\x00"+request.PlanID] {
		planID = request.PlanID
	}
	builder.put(domain.Object{
		Type: "ChangeRequest", PrimaryKey: request.ID,
		Properties: map[string]string{
			"id": request.ID, "environment": request.Environment, "planId": planID,
			"title": request.Title, "status": request.Status, "author": request.Author.Username,
		},
	})
	builder.link("changeRequests", "Environment", request.Environment, "ChangeRequest", request.ID)
	builder.link("environment", "ChangeRequest", request.ID, "Environment", request.Environment)
	if planID != "" {
		builder.link("changeRequest", "Plan", planID, "ChangeRequest", request.ID)
		builder.link("plan", "ChangeRequest", request.ID, "Plan", planID)
	}
}

func (builder *builder) addReport(report controlplane.EnvironmentReport) {
	if !builder.visible(report.Environment) || report.AgentID == "" {
		return
	}
	builder.addEnvironment(controlplane.Environment{ID: report.Environment})
	builder.put(domain.Object{
		Type: "Spoke", PrimaryKey: report.AgentID,
		Properties: map[string]string{"id": report.AgentID, "environment": report.Environment},
	})
	builder.link("reportingSpoke", "Environment", report.Environment, "Spoke", report.AgentID)
	builder.link("environments", "Spoke", report.AgentID, "Environment", report.Environment)
}

func (builder *builder) addProfile(profile controlplane.EnvironmentProfile) {
	if !builder.visible(profile.EnvironmentID) {
		return
	}
	builder.put(domain.Object{
		Type: "WorkProfile", PrimaryKey: profile.EnvironmentID,
		Properties: map[string]string{
			"environmentId": profile.EnvironmentID, "displayName": profile.DisplayName,
			"description": profile.Description, "accreditation": profile.Accreditation,
			"ownerTeam": profile.OwnerTeam,
		},
	})
	builder.link("workProfile", "Environment", profile.EnvironmentID, "WorkProfile", profile.EnvironmentID)
	builder.link("environment", "WorkProfile", profile.EnvironmentID, "Environment", profile.EnvironmentID)
}

func (builder *builder) addEnvironment(environment controlplane.Environment) {
	if !builder.visible(environment.ID) {
		return
	}
	current := builder.environments[environment.ID]
	if current.PrimaryKey == "" {
		current = domain.Object{Type: "Environment", PrimaryKey: environment.ID, Properties: map[string]string{"id": environment.ID}}
	}
	if environment.Generation != "" {
		current.Properties["generation"] = environment.Generation
	}
	if environment.ConnectionSetting != "" {
		current.Properties["connection"] = environment.ConnectionSetting
	}
	if environment.DefaultReleaseChannel != "" {
		current.Properties["releaseChannel"] = environment.DefaultReleaseChannel
	}
	builder.environments[environment.ID] = current
}

func (builder *builder) addChannel(environmentID, name, description string, archived bool) {
	name = strings.TrimSpace(name)
	if name == "" || !builder.visible(environmentID) {
		return
	}
	id := environmentID + "/" + name
	builder.put(domain.Object{
		Type: "ReleaseChannel", PrimaryKey: id,
		Properties: map[string]string{
			"id": id, "environment": environmentID, "name": name,
			"description": description, "archived": strconv.FormatBool(archived),
		},
	})
	builder.link("releaseChannels", "Environment", environmentID, "ReleaseChannel", id)
	builder.link("environment", "ReleaseChannel", id, "Environment", environmentID)
}

func (builder *builder) addArtifact(planID, product, version string, artifact controlplane.Artifact) {
	if artifact.Digest == "" && artifact.Repository == "" {
		return
	}
	id := artifact.Digest
	if id == "" {
		id = product + "@" + version + "/" + artifact.Repository
	}
	builder.put(domain.Object{
		Type: "Artifact", PrimaryKey: id,
		Properties: map[string]string{
			"id": id, "product": product, "version": version,
			"repository": artifact.Repository, "tag": artifact.Tag, "digest": artifact.Digest,
		},
	})
	builder.link("artifacts", "Plan", planID, "Artifact", id)
	builder.link("plans", "Artifact", id, "Plan", planID)
}

func (builder *builder) put(object domain.Object) {
	key := object.Type + "\x00" + object.PrimaryKey
	if builder.seen[key] {
		return
	}
	builder.seen[key] = true
	builder.objects = append(builder.objects, object)
}

func (builder *builder) link(apiName, fromType, fromKey, toType, toKey string) {
	key := apiName + "\x00" + fromType + "\x00" + fromKey + "\x00" + toType + "\x00" + toKey
	if builder.seen["link\x00"+key] {
		return
	}
	builder.seen["link\x00"+key] = true
	builder.links = append(builder.links, domain.Link{
		APIName: apiName, FromType: fromType, FromKey: fromKey, ToType: toType, ToKey: toKey,
	})
}

func (builder *builder) environmentIDs() []string {
	ids := make([]string, 0, len(builder.environments))
	for id := range builder.environments {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (builder *builder) catalog() domain.Catalog {
	objects := make([]domain.Object, 0, len(builder.objects)+len(builder.environments))
	for _, id := range builder.environmentIDs() {
		objects = append(objects, builder.environments[id])
	}
	objects = append(objects, builder.objects...)
	return domain.Catalog{Objects: objects, Links: builder.links}
}
