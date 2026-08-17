package domain

import "time"

// Publication is the immutable catalog receipt created at the public
// compatibility boundary. The wire shape that produced it is deliberately not
// retained: Catalog owns the normalized Product, Release, and exact-artifact
// result, while the Apollo context owns foreign DTOs and Maven coordinates.
type Publication struct {
	MavenCoordinate   string   `json:"mavenCoordinate"`
	Product           string   `json:"product"`
	Version           string   `json:"version"`
	Kind              string   `json:"kind"`
	PublicationDigest string   `json:"publicationDigest"`
	ReleaseChannels   []string `json:"releaseChannels,omitempty"`
	// UpgradeStrategy is what the publisher declared in the Product manifest,
	// and empty means they declared nothing. It persists under a different key
	// than it is served under, deliberately. Until this field had a writer,
	// every Publication was stamped with the constant "shutdown", so every
	// stored value is synthesized -- and once a real declaration can reach it,
	// a genuine "shutdown" is indistinguishable from a synthesized one. Nothing
	// in the record discriminates them: there is no schema version, the wire
	// payload is not retained, and the digest never covered the strategy. So
	// the key moved instead. A legacy record's "upgradeStrategy" is simply not
	// decoded, which restores it as undeclared and serves it as an omission;
	// "declaredUpgradeStrategy" is only ever written from a publisher's
	// manifest. Presence of the key is the discriminator, by construction
	// rather than by date.
	UpgradeStrategy string            `json:"declaredUpgradeStrategy,omitempty"`
	Dependencies    []Dependency      `json:"dependencies,omitempty"`
	Release         *Release          `json:"release,omitempty"`
	DefaultConfig   *PublicationAsset `json:"defaultConfig,omitempty"`
	ConfigSchema    *PublicationAsset `json:"configSchema,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
}

// PublicationAsset binds exact bytes to a Publication without making a
// storage timestamp part of the immutable publication digest.
type PublicationAsset struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}
