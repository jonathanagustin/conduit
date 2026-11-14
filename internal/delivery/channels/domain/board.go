// Package domain is the channel subscription view.
//
// An environment subscribes to dev, candidate, or stable. The version on each
// of those channels is computed by the application from the catalog the hub
// already stores. This package does not read a repository.
package domain

import "strings"

// Channels is the set an environment subscribes to, in rollout order.
func Channels() []string {
	return []string{"dev", "candidate", "stable"}
}

// Row is one product in one environment.
type Row struct {
	Environment string            `json:"environment"`
	Product     string            `json:"product"`
	Subscribed  string            `json:"subscribed"`
	Versions    map[string]string `json:"versions"`
	Running     string            `json:"running,omitempty"`
}

// View is the read model.
type View struct {
	Channels []string `json:"channels"`
	Rows     []Row    `json:"rows"`
}

// SubscriptionOf maps a stored channel name onto dev, candidate, or stable.
// Names outside that set are not a subscription.
func SubscriptionOf(channel string) string {
	switch strings.ToUpper(strings.TrimSpace(channel)) {
	case "DEV":
		return "dev"
	case "CANDIDATE", "RELEASE_CANDIDATE":
		return "candidate"
	case "STABLE", "RELEASE":
		return "stable"
	default:
		switch strings.ToLower(strings.TrimSpace(channel)) {
		case "dev", "candidate", "stable":
			return strings.ToLower(strings.TrimSpace(channel))
		default:
			return ""
		}
	}
}

// CatalogChannel is the catalog name for one subscription channel.
func CatalogChannel(subscription string) string {
	switch SubscriptionOf(subscription) {
	case "dev":
		return "DEV"
	case "candidate":
		return "RELEASE_CANDIDATE"
	case "stable":
		return "RELEASE"
	default:
		return ""
	}
}

// Empty is a view with the three channels and no rows.
func Empty() View {
	return View{Channels: Channels(), Rows: []Row{}}
}
