// Package application turns Conduit's delivery records into Knative resources.
//
// It reads Conduit's own catalog domain -- Releases and their Products -- and
// returns serving.knative.dev/v1 objects, filling only the fields the mapping
// table below can cite a source for. It is read-only, like the Apollo interface
// beside it: Knative's write path (kubectl apply of a Service) would be a
// control surface, and Conduit's control surface is its own governed change
// pipeline, not a projection. Presenting a writable Knative API would invite a
// client to believe an apply here reconciles, which it does not.
package application

import (
	"regexp"
	"strings"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	"github.com/jonathanagustin/conduit/internal/knative/domain"
	"github.com/jonathanagustin/conduit/internal/knative/schema"
)

func at(kind, field string) domain.Coordinate {
	return domain.Coordinate{Kind: kind, Field: field}
}

// mapping is the single statement of which Knative coordinates Conduit fills and
// from where. Reconcile-style: a served kind's field that is NOT here refuses.
//
// The refusals are as much the point as the mappings. Revision.spec
// .containerConcurrency, Route.spec.traffic[].percent and the rest of the
// PodSpec are Knative fields Conduit has no delivery-side source for, so they
// are deliberately absent here and refuse rather than emit a zero that reads as
// a chosen setting. Route and Service are not served at all in this slice; the
// design record explains why (Route.percent has no Conduit backing at all).
var mapping = domain.NewMapping(map[domain.Coordinate]string{
	// Revision <- Release. The immutable, versioned unit of each system.
	at(schema.KindRevision, "metadata.name"):           "catalog: Release.Product + Release.Version, DNS-normalized",
	at(schema.KindRevision, "metadata.labels"):         "catalog: serving.knative.dev/configuration <- Release.Product",
	at(schema.KindRevision, "spec.containers[].image"): "catalog: Release.Images[0], digest-pinned",
	at(schema.KindRevision, "spec.timeoutSeconds"):     "catalog: Release.ResourceReadinessTimeoutSeconds, when set",
	// Configuration <- Product. The linear history of Releases and its head.
	at(schema.KindConfiguration, "metadata.name"):                    "catalog: Product id, DNS-normalized",
	at(schema.KindConfiguration, "status.latestCreatedRevisionName"): "catalog: newest Release by version order",
	// status.latestReadyRevisionName is REFUSED in this slice. "Ready" is the
	// revision currently serving, and the catalog cannot say which that is:
	// EffectiveChannels folds in the channels a version implies, so channel
	// membership means "eligible," not "promoted." An honest latestReady needs
	// either a chosen channel's head or Reporting's observed-healthy revision
	// (the same source the Apollo interface draws health from) -- a real
	// decision, filed rather than guessed. Emitting the newest as "ready" would
	// route a Knative client to a Release no consumer was given.
})

// Mapping exposes the table so a test and an operator-facing report read the
// same statement the projection fills against.
func Mapping() domain.Mapping { return mapping }

var notDNS1123 = regexp.MustCompile(`[^a-z0-9-]+`)

// dnsName renders a Conduit identifier as a DNS-1123 label, which is what a
// Knative object name must be. Product ids are Maven coordinates (group:name)
// and versions carry dots, neither of which is DNS-valid, so this is lossy by
// construction: the exact coordinate travels in a label, and this is only the
// addressable name. A collision is possible in principle and would be a real
// defect; the design record tracks hardening it (a digest suffix) rather than
// pretending the normalization is injective.
func dnsName(identifier string) string {
	lowered := strings.ToLower(identifier)
	replaced := notDNS1123.ReplaceAllString(lowered, "-")
	trimmed := strings.Trim(replaced, "-")
	if trimmed == "" {
		return "unnamed"
	}
	return trimmed
}

const labelConfiguration = "serving.knative.dev/configuration"

// RevisionName is the Knative Revision name Conduit derives for a Release. It is
// exported because the Configuration's revision pointers must name the same
// Revisions this produces; two derivations would drift.
func RevisionName(product, version string) string {
	return dnsName(product) + "-" + dnsName(version)
}

// imageReference renders a Release image the way Conduit pins one: digest-first,
// because a Release identifies its image by digest and a tag alone would be a
// weaker claim than Conduit actually holds.
func imageReference(image catalog.Artifact) string {
	switch {
	case image.Digest != "":
		return image.Repository + "@" + image.Digest
	case image.Tag != "":
		return image.Repository + ":" + image.Tag
	default:
		return image.Repository
	}
}

// RevisionFromRelease projects one Release as an immutable Knative Revision.
func RevisionFromRelease(release catalog.Release) schema.Revision {
	revision := schema.Revision{
		APIVersion: schema.APIVersion,
		Kind:       schema.KindRevision,
		Metadata: schema.ObjectMeta{
			Name:   RevisionName(release.Product, release.Version),
			Labels: map[string]string{labelConfiguration: dnsName(release.Product)},
		},
		Spec: schema.RevisionSpec{Containers: []schema.Container{}},
	}
	if len(release.Images) > 0 {
		revision.Spec.Containers = []schema.Container{{Image: imageReference(release.Images[0])}}
	}
	// Absent (zero) means "not set" in Conduit, and Knative's own default for an
	// unset timeout is not Conduit's to assert, so it is omitted rather than sent
	// as 0.
	if release.ResourceReadinessTimeoutSeconds > 0 {
		timeout := release.ResourceReadinessTimeoutSeconds
		revision.Spec.TimeoutSeconds = &timeout
	}
	return revision
}

// ConfigurationFromReleases projects a Product's Releases as a Knative
// Configuration -- the floating HEAD of their linear history.
//
// latestCreated is the newest Release by Conduit's version ordering, which is
// unambiguous. latestReady is deliberately left empty: see the mapping table
// for why "ready" cannot be established from the catalog without a decision,
// and why guessing it would be worse than refusing it.
func ConfigurationFromReleases(product string, releases []catalog.Release) schema.Configuration {
	configuration := schema.Configuration{
		APIVersion: schema.APIVersion,
		Kind:       schema.KindConfiguration,
		Metadata:   schema.ObjectMeta{Name: dnsName(product)},
	}
	var newest *catalog.Release
	for index := range releases {
		release := releases[index]
		if release.Product != product {
			continue
		}
		if newest == nil || catalog.CompareVersions(release.Version, newest.Version) > 0 {
			copyRelease := release
			newest = &copyRelease
		}
	}
	if newest != nil {
		configuration.Status.LatestCreatedRevisionName = RevisionName(newest.Product, newest.Version)
	}
	return configuration
}
