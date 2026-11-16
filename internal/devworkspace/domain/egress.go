package domain

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// IsolationNotice is the canonical isolation posture statement displayed in
	// workspace metadata, UI surfaces, and operational documentation.
	IsolationNotice = "container isolation now, stronger isolation planned"

	// DefaultWorkspaceRegistryHost is the default in-cluster OCI registry service
	// allowed by workspace.egress when no override is configured.
	DefaultWorkspaceRegistryHost = "registry.uds.svc.cluster.local"

	// DefaultWorkspaceRegistryPort is the default TCP port for the in-cluster registry.
	DefaultWorkspaceRegistryPort = 5000

	// DefaultGitPort is the default TCP port for the configured git host.
	DefaultGitPort = 443
)

var (
	// ErrInvalidEgress indicates a workspace.egress configuration failed schema or security validation.
	ErrInvalidEgress = fmt.Errorf("%w: invalid workspace.egress configuration", ErrInvalid)
	// ErrEgressDenied indicates an outbound workspace connection was blocked by the default-deny policy.
	ErrEgressDenied = errors.New("workspace egress denied by policy")

	egressEntryNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	dnsLabelPattern        = regexp.MustCompile(`^[a-zA-Z0-9]([-a-zA-Z0-9]{0,61}[a-zA-Z0-9])?$`)
)

// EgressAllowEntry is one platform-operator-controlled entry in workspace.egress.allow.
type EgressAllowEntry struct {
	Name     string `json:"name" yaml:"name"`
	Host     string `json:"host" yaml:"host"`
	Port     int    `json:"port" yaml:"port"`
	Protocol string `json:"protocol" yaml:"protocol"`
}

// EgressConfig is the platform-operator-supplied workspace.egress configuration.
// It is rendered into the workspace's UDS Package CR network.allow section and
// Kubernetes NetworkPolicy, and is never read from inside the workspace container.
type EgressConfig struct {
	GitHost      string             `json:"gitHost,omitempty" yaml:"gitHost,omitempty"`
	GitPort      int                `json:"gitPort,omitempty" yaml:"gitPort,omitempty"`
	Registry     string             `json:"registry,omitempty" yaml:"registry,omitempty"`
	RegistryPort int                `json:"registryPort,omitempty" yaml:"registryPort,omitempty"`
	Allow        []EgressAllowEntry `json:"allow,omitempty" yaml:"allow,omitempty"`
}

type workspaceValuesEnvelope struct {
	Workspace *struct {
		Egress *EgressConfig `yaml:"egress"`
	} `yaml:"workspace"`
	Egress *EgressConfig `yaml:"egress"`
}

// ParseEgressValuesYAML parses and validates a workspace.egress YAML configuration
// supplied through package or bundle values.
func ParseEgressValuesYAML(raw []byte) (EgressConfig, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return EgressConfig{}, fmt.Errorf("%w: workspace.egress YAML is empty", ErrInvalidEgress)
	}
	var env workspaceValuesEnvelope
	dec := yaml.NewDecoder(bytes.NewReader(trimmed))
	dec.KnownFields(true)
	if err := dec.Decode(&env); err == nil {
		if env.Workspace != nil && env.Workspace.Egress != nil {
			if err := checkExplicitAllowEntries(trimmed); err != nil {
				return EgressConfig{}, err
			}
			return NormalizeAndValidateEgress(*env.Workspace.Egress)
		}
		if env.Egress != nil {
			if err := checkExplicitAllowEntries(trimmed); err != nil {
				return EgressConfig{}, err
			}
			return NormalizeAndValidateEgress(*env.Egress)
		}
	}
	var direct EgressConfig
	decDirect := yaml.NewDecoder(bytes.NewReader(trimmed))
	decDirect.KnownFields(true)
	if err := decDirect.Decode(&direct); err != nil {
		return EgressConfig{}, fmt.Errorf("%w: %v", ErrInvalidEgress, err)
	}
	if err := checkExplicitAllowEntries(trimmed); err != nil {
		return EgressConfig{}, err
	}
	return NormalizeAndValidateEgress(direct)
}

func checkExplicitAllowEntries(raw []byte) error {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEgress, err)
	}
	egressMap := extractEgressMap(doc)
	if egressMap == nil {
		return nil
	}
	rawAllow, hasAllow := egressMap["allow"]
	if !hasAllow {
		return nil
	}
	list, ok := rawAllow.([]any)
	if !ok || len(list) == 0 {
		return fmt.Errorf("%w: workspace.egress.allow cannot be empty when specified", ErrInvalidEgress)
	}
	for i, item := range list {
		entryMap, ok := item.(map[string]any)
		if !ok || len(entryMap) == 0 {
			return fmt.Errorf("%w: workspace.egress.allow[%d] is empty", ErrInvalidEgress, i)
		}
		for _, reqKey := range []string{"name", "host", "port", "protocol"} {
			val, exists := entryMap[reqKey]
			if !exists || val == nil || strings.TrimSpace(fmt.Sprint(val)) == "" {
				return fmt.Errorf("%w: workspace.egress.allow[%d].%s is required", ErrInvalidEgress, i, reqKey)
			}
		}
	}
	return nil
}

func extractEgressMap(doc map[string]any) map[string]any {
	if ws, ok := doc["workspace"].(map[string]any); ok {
		if eg, ok := ws["egress"].(map[string]any); ok {
			return eg
		}
	}
	if eg, ok := doc["egress"].(map[string]any); ok {
		return eg
	}
	if _, hasGit := doc["gitHost"]; hasGit {
		return doc
	}
	if _, hasAllow := doc["allow"]; hasAllow {
		return doc
	}
	return nil
}

// NormalizeAndValidateEgress applies defaults for the local registry and allowlist
// and validates that gitHost is present and that no entry uses wildcards, 0.0.0.0/0,
// or empty fields.
func NormalizeAndValidateEgress(cfg EgressConfig) (EgressConfig, error) {
	gitHost, gitPort, err := splitOptionalHostPort(strings.TrimSpace(cfg.GitHost), cfg.GitPort, DefaultGitPort)
	if err != nil {
		return EgressConfig{}, err
	}
	if gitHost == "" {
		// Check whether an explicit git-host entry in Allow supplies it.
		for _, entry := range cfg.Allow {
			if strings.TrimSpace(entry.Name) == "git-host" && strings.TrimSpace(entry.Host) != "" {
				gitHost = strings.TrimSpace(entry.Host)
				if entry.Port > 0 {
					gitPort = entry.Port
				}
				break
			}
		}
	}
	if gitHost == "" {
		return EgressConfig{}, fmt.Errorf("%w: workspace.egress.gitHost is required and must not be empty", ErrInvalidEgress)
	}
	if err := validateEgressHost(gitHost); err != nil {
		return EgressConfig{}, fmt.Errorf("gitHost: %w", err)
	}
	if gitPort < 1 || gitPort > 65535 {
		return EgressConfig{}, fmt.Errorf("%w: gitPort must be between 1 and 65535", ErrInvalidEgress)
	}

	regInput := strings.TrimSpace(cfg.Registry)
	if regInput == "" {
		regInput = DefaultWorkspaceRegistryHost
	}
	regHost, regPort, err := splitOptionalHostPort(regInput, cfg.RegistryPort, DefaultWorkspaceRegistryPort)
	if err != nil {
		return EgressConfig{}, err
	}
	if err := validateEgressHost(regHost); err != nil {
		return EgressConfig{}, fmt.Errorf("registry: %w", err)
	}
	if regPort < 1 || regPort > 65535 {
		return EgressConfig{}, fmt.Errorf("%w: registryPort must be between 1 and 65535", ErrInvalidEgress)
	}

	out := EgressConfig{
		GitHost:      gitHost,
		GitPort:      gitPort,
		Registry:     regHost,
		RegistryPort: regPort,
	}

	if len(cfg.Allow) == 0 {
		out.Allow = []EgressAllowEntry{
			{
				Name:     "git-host",
				Host:     gitHost,
				Port:     gitPort,
				Protocol: "TCP",
			},
			{
				Name:     "local-registry",
				Host:     regHost,
				Port:     regPort,
				Protocol: "TCP",
			},
		}
		return out, nil
	}

	seenNames := make(map[string]struct{}, len(cfg.Allow))
	hasGitEntry := false
	normalized := make([]EgressAllowEntry, 0, len(cfg.Allow))
	for i, entry := range cfg.Allow {
		name := strings.TrimSpace(entry.Name)
		host := strings.TrimSpace(entry.Host)
		proto := strings.ToUpper(strings.TrimSpace(entry.Protocol))
		if name == "" || !egressEntryNamePattern.MatchString(name) {
			return EgressConfig{}, fmt.Errorf("%w: allow[%d].name %q is invalid", ErrInvalidEgress, i, entry.Name)
		}
		if _, dup := seenNames[name]; dup {
			return EgressConfig{}, fmt.Errorf("%w: duplicate allow entry name %q", ErrInvalidEgress, name)
		}
		seenNames[name] = struct{}{}
		if err := validateEgressHost(host); err != nil {
			return EgressConfig{}, fmt.Errorf("allow[%d] (%s): %w", i, name, err)
		}
		if entry.Port < 1 || entry.Port > 65535 {
			return EgressConfig{}, fmt.Errorf("%w: allow[%d] (%s) port %d must be between 1 and 65535", ErrInvalidEgress, i, name, entry.Port)
		}
		if proto != "TCP" && proto != "UDP" {
			return EgressConfig{}, fmt.Errorf("%w: allow[%d] (%s) protocol %q must be TCP or UDP", ErrInvalidEgress, i, name, entry.Protocol)
		}
		if name == "git-host" {
			hasGitEntry = true
		}
		normalized = append(normalized, EgressAllowEntry{
			Name:     name,
			Host:     host,
			Port:     entry.Port,
			Protocol: proto,
		})
	}
	if !hasGitEntry {
		return EgressConfig{}, fmt.Errorf("%w: workspace.egress.allow must include the git-host entry", ErrInvalidEgress)
	}
	out.Allow = normalized
	return out, nil
}

func splitOptionalHostPort(raw string, explicitPort, defaultPort int) (string, int, error) {
	if raw == "" {
		if explicitPort > 0 {
			return "", explicitPort, nil
		}
		return "", defaultPort, nil
	}
	if strings.Contains(raw, "/") {
		// CIDR notation
		if explicitPort > 0 {
			return raw, explicitPort, nil
		}
		return raw, defaultPort, nil
	}
	if h, pStr, err := net.SplitHostPort(raw); err == nil {
		p, convErr := strconv.Atoi(pStr)
		if convErr != nil || p < 1 || p > 65535 {
			return "", 0, fmt.Errorf("%w: invalid port in %q", ErrInvalidEgress, raw)
		}
		if explicitPort > 0 && explicitPort != p {
			return "", 0, fmt.Errorf("%w: conflicting ports in %q and %d", ErrInvalidEgress, raw, explicitPort)
		}
		return h, p, nil
	}
	if explicitPort > 0 {
		return raw, explicitPort, nil
	}
	return raw, defaultPort, nil
}

func validateEgressHost(host string) error {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return fmt.Errorf("%w: host must not be empty", ErrInvalidEgress)
	}
	if strings.Contains(trimmed, "*") {
		return fmt.Errorf("%w: wildcard host %q is rejected", ErrInvalidEgress, host)
	}
	lower := strings.ToLower(trimmed)
	if lower == "0.0.0.0/0" || lower == "::/0" || lower == "0.0.0.0" || lower == "::" || lower == "255.255.255.255" {
		return fmt.Errorf("%w: allow-all address %q is rejected", ErrInvalidEgress, host)
	}
	if strings.Contains(trimmed, "/") {
		ip, ipNet, err := net.ParseCIDR(trimmed)
		if err != nil || ip == nil || ipNet == nil {
			return fmt.Errorf("%w: invalid CIDR %q", ErrInvalidEgress, host)
		}
		ones, _ := ipNet.Mask.Size()
		if ones == 0 || ip.IsUnspecified() {
			return fmt.Errorf("%w: allow-all CIDR %q is rejected", ErrInvalidEgress, host)
		}
		return nil
	}
	if ip := net.ParseIP(trimmed); ip != nil {
		if ip.IsUnspecified() {
			return fmt.Errorf("%w: unspecified IP %q is rejected", ErrInvalidEgress, host)
		}
		return nil
	}
	if !isValidDNSName(trimmed) {
		return fmt.Errorf("%w: invalid DNS hostname %q", ErrInvalidEgress, host)
	}
	return nil
}

func isValidDNSName(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if !dnsLabelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

// AllowsEndpoint checks whether an outbound connection from a workspace container
// to host:port over protocol is permitted by the validated workspace.egress policy.
// Cluster DNS resolution (kube-dns on port 53 UDP/TCP) and entries in cfg.Allow are allowed;
// everything else is refused.
func AllowsEndpoint(cfg EgressConfig, host string, port int, protocol string) error {
	validated, err := NormalizeAndValidateEgress(cfg)
	if err != nil {
		return err
	}
	cleanHost := strings.TrimSpace(strings.ToLower(host))
	cleanProto := strings.ToUpper(strings.TrimSpace(protocol))
	if cleanProto == "" {
		cleanProto = "TCP"
	}
	if cleanHost == "" || port < 1 || port > 65535 {
		return fmt.Errorf("%w: invalid target %s:%d/%s", ErrEgressDenied, host, port, cleanProto)
	}
	if port == 53 && (cleanProto == "UDP" || cleanProto == "TCP") &&
		(cleanHost == "kube-dns" || cleanHost == "kube-dns.kube-system.svc.cluster.local" || cleanHost == "coredns.kube-system.svc.cluster.local") {
		return nil
	}
	targetIP := net.ParseIP(cleanHost)
	for _, entry := range validated.Allow {
		if !strings.EqualFold(entry.Protocol, cleanProto) || entry.Port != port {
			continue
		}
		entryHost := strings.ToLower(strings.TrimSpace(entry.Host))
		if strings.Contains(entryHost, "/") {
			if targetIP != nil {
				if _, cidrNet, err := net.ParseCIDR(entryHost); err == nil && cidrNet.Contains(targetIP) {
					return nil
				}
			}
			continue
		}
		if entryIP := net.ParseIP(entryHost); entryIP != nil && targetIP != nil {
			if entryIP.Equal(targetIP) {
				return nil
			}
			continue
		}
		if entryHost == cleanHost {
			return nil
		}
	}
	return fmt.Errorf("%w: %s:%d/%s is not in workspace.egress.allow", ErrEgressDenied, host, port, cleanProto)
}

// RenderWorkspacePackageCR renders the UDS Package custom resource for the
// workspace with its spec.network.allow section populated from the validated
// workspace.egress configuration.
func RenderWorkspacePackageCR(namespace, name string, cfg EgressConfig) ([]byte, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return nil, ErrInvalid
	}
	validated, err := NormalizeAndValidateEgress(cfg)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("apiVersion: uds.dev/v1alpha1\n")
	b.WriteString("kind: Package\n")
	b.WriteString("metadata:\n")
	b.WriteString("  name: " + quoteYAML(name) + "\n")
	b.WriteString("  namespace: " + quoteYAML(namespace) + "\n")
	b.WriteString("  annotations:\n")
	b.WriteString("    conduit.dev/isolation: " + quoteYAML(IsolationNotice) + "\n")
	b.WriteString("    packaging.conduit.dev/value-tier: \"CONDUIT_NATIVE\"\n")
	b.WriteString("spec:\n")
	b.WriteString("  network:\n")
	b.WriteString("    serviceMesh:\n")
	b.WriteString("      mode: \"ambient\"\n")
	b.WriteString("    allow:\n")
	for _, proto := range []string{"UDP", "TCP"} {
		b.WriteString("      - direction: \"Egress\"\n")
		b.WriteString("        description: " + quoteYAML("cluster-dns-"+strings.ToLower(proto)) + "\n")
		b.WriteString("        selector:\n")
		b.WriteString("          app: \"dev-workspace\"\n")
		b.WriteString("        remoteNamespace: \"kube-system\"\n")
		b.WriteString("        remoteSelector:\n")
		b.WriteString("          k8s-app: \"kube-dns\"\n")
		b.WriteString("        port: 53\n")
		b.WriteString("        remoteProtocol: " + quoteYAML(proto) + "\n")
	}
	for _, entry := range validated.Allow {
		b.WriteString("      - direction: \"Egress\"\n")
		b.WriteString("        description: " + quoteYAML(entry.Name) + "\n")
		b.WriteString("        selector:\n")
		b.WriteString("          app: \"dev-workspace\"\n")
		if strings.Contains(entry.Host, "/") {
			b.WriteString("        remoteCidr: " + quoteYAML(entry.Host) + "\n")
		} else if ip := net.ParseIP(entry.Host); ip != nil {
			mask := "/32"
			if ip.To4() == nil {
				mask = "/128"
			}
			b.WriteString("        remoteCidr: " + quoteYAML(entry.Host+mask) + "\n")
		} else {
			b.WriteString("        remoteHost: " + quoteYAML(entry.Host) + "\n")
		}
		b.WriteString("        port: " + strconv.Itoa(entry.Port) + "\n")
		b.WriteString("        remoteProtocol: " + quoteYAML(entry.Protocol) + "\n")
	}
	return []byte(b.String()), nil
}

func quoteYAML(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// IsProtectedEgressPath reports whether a workspace-relative path attempts to
// write or override the workspace's network policy or egress allowlist from
// inside the workspace tree.
func IsProtectedEgressPath(relPath string) bool {
	lower := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(relPath, `\`, "/")))
	base := lower
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		base = lower[idx+1:]
	}
	switch base {
	case "workspace-egress.yaml", "workspace-egress.yml", "workspace-networkpolicy.yaml", "workspace-networkpolicy.yml", "networkpolicy.yaml", "networkpolicy.yml":
		return true
	}
	return strings.HasPrefix(lower, ".conduit/egress") || strings.HasPrefix(lower, ".conduit/network")
}

// IsNetworkPolicyEditCommand reports whether a command invoked inside a workspace
// attempts to modify a NetworkPolicy, UDS Package network policy, or host firewall.
func IsNetworkPolicyEditCommand(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	joined := strings.ToLower(strings.Join(argv, " "))
	if strings.Contains(joined, "networkpolicy") || strings.Contains(joined, "netpol") ||
		strings.Contains(joined, "workspace.egress") || strings.Contains(joined, "iptables") ||
		strings.Contains(joined, "nftables") || strings.Contains(joined, "ip route") {
		return true
	}
	base := strings.ToLower(argv[0])
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	switch base {
	case "kubectl", "oc", "k3s", "iptables", "ip6tables", "nft":
		return true
	}
	return false
}
