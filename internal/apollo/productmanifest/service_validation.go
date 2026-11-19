package productmanifest

import (
	"fmt"
	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
	"regexp"
	"sort"
	"strings"
)

// Existing manifest service-extension values remain unchanged.
const (
	QuorumAll               = "all"
	QuorumAllButOne         = "all-but-one"
	QuorumAny               = "any"
	QuorumSimpleMajority    = "simple-majority"
	UpgradeStrategyRolling  = "rolling"
	UpgradeStrategyShutdown = "shutdown"
)

var apolloVolumeNamePattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// ValidateServiceExtensions checks the manifest-only service declarations.
func ValidateServiceExtensions(extensions Extensions, errs *[]validationdomain.Problem) {
	add := func(path, message string) {
		*errs = append(*errs, validationdomain.Problem{Path: path, Message: message})
	}
	for _, name := range sortedKeys(extensions.VolumesV2) {
		volume := extensions.VolumesV2[name]
		path := fmt.Sprintf("extensions.volumes-v2.%s", name)
		if !apolloVolumeNamePattern.MatchString(name) {
			add(path, "volume name must be alphanumerics and hyphens")
		}
		if volume.VolumeType.DurableVolume == nil {
			add(path+".volume-type", "must declare a volume type; durable-volume is the only known type")
		}
	}
	proxy := validatePublicEndpointNames("extensions.public-proxy-endpoints", extensions.PublicProxyEndpoints, errs)
	validatePublicEndpointNames("extensions.public-tcp-endpoints", extensions.PublicTCPEndpoints, errs)
	validatePublicEndpointNames("extensions.public-udp-endpoints", extensions.PublicUDPEndpoints, errs)
	// An endpoint cannot be both proxied and raw TCP or UDP: the proxy terminates
	// TLS and routes on a path, while the raw paths forward an allocated external
	// port. TCP and UDP together are allowed, and are how a Product asks for both
	// protocols on one external port.
	for index, name := range extensions.PublicTCPEndpoints {
		if _, exists := proxy[name]; exists {
			add(fmt.Sprintf("extensions.public-tcp-endpoints[%d]", index),
				"must not also appear in public-proxy-endpoints")
		}
	}
	for index, name := range extensions.PublicUDPEndpoints {
		if _, exists := proxy[name]; exists {
			add(fmt.Sprintf("extensions.public-udp-endpoints[%d]", index),
				"must not also appear in public-proxy-endpoints")
		}
	}
	switch extensions.QuorumRequirement {
	case "", QuorumAll, QuorumAllButOne, QuorumAny, QuorumSimpleMajority:
	default:
		add("extensions.quorum-requirement", "must be all, all-but-one, any, or simple-majority")
	}
	switch extensions.UpgradeStrategy {
	case "", UpgradeStrategyRolling, UpgradeStrategyShutdown:
	default:
		add("extensions.upgrade-strategy", "must be rolling or shutdown")
	}
}

// validatePublicEndpointNames checks one of the two ingress extensions and
// returns the set of names it exposed, which the cross-document checks reuse.
func validatePublicEndpointNames(path string, names []string, errs *[]validationdomain.Problem) map[string]struct{} {
	add := func(path, message string) {
		*errs = append(*errs, validationdomain.Problem{Path: path, Message: message})
	}
	seen := make(map[string]struct{}, len(names))
	for index, name := range names {
		entry := fmt.Sprintf("%s[%d]", path, index)
		if strings.TrimSpace(name) == "" {
			add(entry, "must name an endpoints.definitions entry")
			continue
		}
		if _, exists := seen[name]; exists {
			add(entry, "must not repeat an endpoint name")
		}
		seen[name] = struct{}{}
	}
	return seen
}

func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
