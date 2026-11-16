// Package domain defines the Conduit-native, observation-only release rollout view.
package domain

import (
	"errors"
	"time"
)

// ValueTier identifies this native observation contract.
//
// CONDUIT-INVENTED: delivery.ReleaseRolloutView
// Displaced-by: a published or owner-approved sanitized release rollout contract
// establishing join grain, resource visibility, graph scope and wave persistence.
const ValueTier = "CONDUIT_NATIVE"
const MaxRows = 16384

var (
	ErrNotFound    = errors.New("release not found")
	ErrUnavailable = errors.New("release rollout read unavailable")
)

// Installation copies the existing fleet's desired/reported facts; empty
// coordinates and UNKNOWN classifications remain unknown, never healthy.
type Installation struct {
	Environment           string    `json:"environment"`
	InstallationID        string    `json:"installationId"`
	Product               string    `json:"product"`
	Namespace             string    `json:"namespace"`
	DesiredVersion        string    `json:"desiredVersion,omitempty"`
	ObservedVersion       string    `json:"observedVersion,omitempty"`
	DesiredReleaseDigest  string    `json:"desiredReleaseDigest,omitempty"`
	ObservedReleaseDigest string    `json:"observedReleaseDigest,omitempty"`
	DesiredConfigDigest   string    `json:"desiredConfigDigest,omitempty"`
	ObservedConfigDigest  string    `json:"observedConfigDigest,omitempty"`
	Health                string    `json:"health"`
	RolloutStatus         string    `json:"rolloutStatus"`
	ConfigStatus          string    `json:"configStatus"`
	Lifecycle             string    `json:"lifecycle"`
	ObservedAt            time.Time `json:"observedAt,omitempty"`
	ObservationAgeSeconds int64     `json:"observationAgeSeconds,omitempty"`
	Stale                 bool      `json:"stale"`
}

// Edge is declared topology, not a claim that promotion is currently permitted.
type Edge struct {
	FromChannel string `json:"fromChannel"`
	ToChannel   string `json:"toChannel"`
}
type Graph struct {
	Environment string `json:"environment"`
	Product     string `json:"product"`
	PlanID      string `json:"planId"`
	Enabled     bool   `json:"enabled"`
	Edges       []Edge `json:"edges"`
}
type Wave struct {
	Name         string   `json:"name"`
	Environments []string `json:"environments"`
	Gate         string   `json:"gate"`
	Status       string   `json:"status"`
}
type Rollout struct {
	ID      string `json:"id"`
	Product string `json:"product"`
	Version string `json:"version"`
	Status  string `json:"status"`
	Waves   []Wave `json:"waves"`
}
type Environment struct {
	ID              string         `json:"id"`
	Installations   []Installation `json:"installations"`
	PromotionGraphs []Graph        `json:"promotionGraphs"`
}
type View struct {
	RolloutPersistence string        `json:"rolloutPersistence"`
	Product            string        `json:"product"`
	Version            string        `json:"version"`
	ReadAt             time.Time     `json:"readAt"`
	Environments       []Environment `json:"environments"`
	Rollouts           []Rollout     `json:"rollouts"`
	ValueTier          string        `json:"valueTier"`
}

// Snapshot is a detached input. The source must not return shared mutable slices.
type Snapshot struct {
	Found         bool
	Installations []Installation
	Graphs        []Graph
	Rollouts      []Rollout
}
