package catalog

import (
	"context"
	"time"
)

// Publication is the immutable catalog receipt created at the public
// compatibility boundary. The wire shape that produced it is deliberately not
// retained: Catalog owns the normalized Product, Release, and exact-artifact
// result, while the Apollo context owns foreign DTOs and Maven coordinates.
type Publication struct {
	MavenCoordinate   string            `json:"mavenCoordinate"`
	Product           string            `json:"product"`
	Version           string            `json:"version"`
	Kind              string            `json:"kind"`
	PublicationDigest string            `json:"publicationDigest"`
	ReleaseChannels   []string          `json:"releaseChannels,omitempty"`
	UpgradeStrategy   string            `json:"upgradeStrategy,omitempty"`
	Dependencies      []Dependency      `json:"dependencies,omitempty"`
	Release           *Release          `json:"release,omitempty"`
	DefaultConfig     *PublicationAsset `json:"defaultConfig,omitempty"`
	ConfigSchema      *PublicationAsset `json:"configSchema,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
}

// PublicationAsset binds exact bytes to a Publication without making a
// storage timestamp part of the immutable publication digest.
type PublicationAsset struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// PublicationRepository is the persistence port Catalog exposes to
// compatibility contexts. Implementations enforce coordinate immutability.
type PublicationRepository interface {
	CreateProductPublication(context.Context, Publication) (Publication, error)
	ListProductPublications(context.Context, string) ([]Publication, error)
}
