package fixtures

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// PackageFiles is the packaging-run output: a zarf package definition and the
// Package custom resource it installs. The bytes are the files on disk. The
// digest covers both, in that order, so one file cannot be swapped for the other.
type PackageFiles struct {
	Zarf []byte
	CR   []byte
}

func (p PackageFiles) canonical() []byte {
	out := make([]byte, 0, len(p.Zarf)+len(p.CR)+1)
	out = append(out, p.Zarf...)
	out = append(out, 0)
	out = append(out, p.CR...)
	return out
}

func (p PackageFiles) Digest() string {
	return domain.DigestBytes(p.canonical())
}

func (p PackageFiles) Size() int {
	return len(p.Zarf) + len(p.CR)
}

// DefaultPackagePort is the monitor target and the application port used when
// the product does not declare one.
const DefaultPackagePort = 8080

// Render builds the package for one product source. sourceDigest, templateVersion
// and toolVersions are written into annotations; they are not a second copy of
// the source. Declared sso, network and monitor lines replace that section's
// default. A section that is absent gets the registered default.
//
// CONDUIT-INVENTED: packaging.UndeclaredNeeds
// Displaced-by: a reviewed packaging profile that states the SSO client, network allow and monitor values to emit when a product declares none.
func Render(source []byte, sourceDigest, templateVersion, toolVersions string) (PackageFiles, error) {
	if err := validateBindings(sourceDigest, templateVersion, toolVersions); err != nil {
		return PackageFiles{}, err
	}
	parsed, err := parseNeeds(source)
	if err != nil {
		return PackageFiles{}, err
	}
	return PackageFiles{
		Zarf: renderZarf(parsed, sourceDigest, templateVersion, toolVersions),
		CR:   renderPackage(parsed, sourceDigest),
	}, nil
}

var (
	errNeeds = fmt.Errorf("%w: declared needs are invalid", domain.ErrIdentity)

	appNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,38}[a-z0-9]$`)
	versionPattern  = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$`)
	imagePattern    = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)+(?::[A-Za-z0-9_][A-Za-z0-9._-]{0,127})?@sha256:[0-9a-f]{64}$`)
	digestPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	bindingPattern  = regexp.MustCompile(`^[A-Za-z0-9._,=+/-]{1,256}$`)
	labelPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,62}$`)
	redirectPattern = regexp.MustCompile(`^https://[a-z0-9][a-z0-9.-]{0,200}(/[A-Za-z0-9._~/-]{0,200})?$`)
	portNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,14}$`)
	pathPattern     = regexp.MustCompile(`^/[A-Za-z0-9._~/-]{0,128}$`)
	secretPattern   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	textPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

var reservedNames = map[string]bool{
	"kube-system": true, "kube-public": true, "kube-node-lease": true,
	"istio-system": true, "zarf": true, "uds-system": true, "pepr-system": true,
}

type productNeeds struct {
	name        string
	port        int
	portSet     bool
	version     string
	image       string
	mesh        string
	sso         []ssoClient
	ssoNone     bool
	network     []allowRule
	networkNone bool
	monitor     []monitorRule
	monitorNone bool
}

type ssoClient struct {
	name     string
	clientID string
	protocol string
	redirect string
	secret   string
}

type allowRule struct {
	direction       string
	description     string
	selectorKey     string
	selectorValue   string
	remoteNamespace string
	remoteKey       string
	remoteValue     string
	port            int
	protocol        string
}

type monitorRule struct {
	description   string
	portName      string
	targetPort    int
	path          string
	selectorKey   string
	selectorValue string
}

func parseNeeds(source []byte) (productNeeds, error) {
	if len(source) > 64*1024 {
		return productNeeds{}, errNeeds
	}
	var parsed productNeeds
	lines := strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	if len(lines) > 64 {
		return productNeeds{}, errNeeds
	}
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return productNeeds{}, errNeeds
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == "" {
			return productNeeds{}, errNeeds
		}
		switch key {
		case "kind", "requirement", "security-evidence", "lint", "proposal", "rationale", "memory", "memory-request", "chart-values", "resources", "image-ref", "image-digest":
			continue
		case "name":
			if parsed.name != "" || !appNamePattern.MatchString(value) || reservedNames[value] {
				return productNeeds{}, errNeeds
			}
			parsed.name = value
		case "port":
			if parsed.portSet {
				return productNeeds{}, errNeeds
			}
			port, err := strconv.Atoi(value)
			if err != nil || port < 1 || port > 65535 {
				return productNeeds{}, errNeeds
			}
			parsed.port = port
			parsed.portSet = true
		case "version":
			if parsed.version != "" || !versionPattern.MatchString(value) {
				return productNeeds{}, errNeeds
			}
			parsed.version = value
		case "image":
			if parsed.image != "" || len(value) > 256 || !imagePattern.MatchString(value) {
				return productNeeds{}, errNeeds
			}
			parsed.image = value
		case "mesh":
			if parsed.mesh != "" || (value != "ambient" && value != "sidecar") {
				return productNeeds{}, errNeeds
			}
			parsed.mesh = value
		case "sso":
			if err := addSSO(&parsed, value); err != nil {
				return productNeeds{}, err
			}
		case "network":
			if err := addNetwork(&parsed, value); err != nil {
				return productNeeds{}, err
			}
		case "monitor":
			if err := addMonitor(&parsed, value); err != nil {
				return productNeeds{}, err
			}
		default:
			return productNeeds{}, errNeeds
		}
	}
	if parsed.name == "" {
		return productNeeds{}, errNeeds
	}
	if !parsed.portSet {
		parsed.port = DefaultPackagePort
	}
	if parsed.version == "" {
		parsed.version = "0.0.1"
	}
	if parsed.mesh == "" {
		parsed.mesh = "ambient"
	}
	applyDefaults(&parsed)
	return parsed, nil
}

func addSSO(parsed *productNeeds, value string) error {
	if value == "none" {
		if parsed.ssoNone || len(parsed.sso) > 0 {
			return errNeeds
		}
		parsed.ssoNone = true
		return nil
	}
	if parsed.ssoNone || len(parsed.sso) >= 8 {
		return errNeeds
	}
	pairs, err := pairs(value)
	if err != nil {
		return err
	}
	clientID := pairs["clientId"]
	redirect := pairs["redirect"]
	if !appNamePattern.MatchString(clientID) || !redirectPattern.MatchString(redirect) {
		return errNeeds
	}
	name := pairs["name"]
	if name == "" {
		name = clientID
	}
	if !appNamePattern.MatchString(name) {
		return errNeeds
	}
	protocol := pairs["protocol"]
	if protocol == "" {
		protocol = "openid-connect"
	}
	if protocol != "openid-connect" && protocol != "saml" {
		return errNeeds
	}
	secret := pairs["secret"]
	if secret == "" {
		secret = clientID + "-sso"
	}
	if !secretPattern.MatchString(secret) {
		return errNeeds
	}
	for key := range pairs {
		switch key {
		case "clientId", "redirect", "name", "protocol", "secret":
		default:
			return errNeeds
		}
	}
	parsed.sso = append(parsed.sso, ssoClient{
		name: name, clientID: clientID, protocol: protocol, redirect: redirect, secret: secret,
	})
	return nil
}

func addNetwork(parsed *productNeeds, value string) error {
	if value == "none" {
		if parsed.networkNone || len(parsed.network) > 0 {
			return errNeeds
		}
		parsed.networkNone = true
		return nil
	}
	if parsed.networkNone || len(parsed.network) >= 16 {
		return errNeeds
	}
	pairs, err := pairs(value)
	if err != nil {
		return err
	}
	direction := pairs["direction"]
	if direction != "Ingress" && direction != "Egress" {
		return errNeeds
	}
	remoteNamespace := pairs["remoteNamespace"]
	if !appNamePattern.MatchString(remoteNamespace) && remoteNamespace != "kube-system" && remoteNamespace != "kube-public" {
		return errNeeds
	}
	remoteKey, remoteValue, ok := strings.Cut(pairs["remoteSelector"], "=")
	if !ok || !labelPattern.MatchString(remoteKey) || !labelPattern.MatchString(remoteValue) {
		return errNeeds
	}
	selectorKey := ""
	selectorValue := ""
	if raw, exists := pairs["selector"]; exists {
		var ok bool
		selectorKey, selectorValue, ok = strings.Cut(raw, "=")
		if !ok || !labelPattern.MatchString(selectorKey) || !labelPattern.MatchString(selectorValue) {
			return errNeeds
		}
	}
	port, err := strconv.Atoi(pairs["port"])
	if err != nil || port < 1 || port > 65535 {
		return errNeeds
	}
	protocol := pairs["protocol"]
	switch protocol {
	case "TCP", "UDP", "HTTP", "TLS":
	default:
		return errNeeds
	}
	description := pairs["description"]
	if description != "" && !textPattern.MatchString(description) {
		return errNeeds
	}
	for key := range pairs {
		switch key {
		case "direction", "remoteNamespace", "remoteSelector", "selector", "port", "protocol", "description":
		default:
			return errNeeds
		}
	}
	parsed.network = append(parsed.network, allowRule{
		direction: direction, description: description,
		selectorKey: selectorKey, selectorValue: selectorValue,
		remoteNamespace: remoteNamespace, remoteKey: remoteKey, remoteValue: remoteValue,
		port: port, protocol: protocol,
	})
	return nil
}

func addMonitor(parsed *productNeeds, value string) error {
	if value == "none" {
		if parsed.monitorNone || len(parsed.monitor) > 0 {
			return errNeeds
		}
		parsed.monitorNone = true
		return nil
	}
	if parsed.monitorNone || len(parsed.monitor) >= 8 {
		return errNeeds
	}
	pairs, err := pairs(value)
	if err != nil {
		return err
	}
	portName := pairs["portName"]
	if portName == "" {
		portName = "http"
	}
	if !portNamePattern.MatchString(portName) {
		return errNeeds
	}
	target := 0
	if raw, exists := pairs["targetPort"]; exists {
		target, err = strconv.Atoi(raw)
		if err != nil || target < 1 || target > 65535 {
			return errNeeds
		}
	}
	path := pairs["path"]
	if path == "" {
		path = "/metrics"
	}
	if !pathPattern.MatchString(path) {
		return errNeeds
	}
	selectorKey := ""
	selectorValue := ""
	if raw, exists := pairs["selector"]; exists {
		var ok bool
		selectorKey, selectorValue, ok = strings.Cut(raw, "=")
		if !ok || !labelPattern.MatchString(selectorKey) || !labelPattern.MatchString(selectorValue) {
			return errNeeds
		}
	}
	description := pairs["description"]
	if description != "" && !textPattern.MatchString(description) {
		return errNeeds
	}
	for key := range pairs {
		switch key {
		case "portName", "targetPort", "path", "selector", "description":
		default:
			return errNeeds
		}
	}
	parsed.monitor = append(parsed.monitor, monitorRule{
		description: description, portName: portName, targetPort: target, path: path,
		selectorKey: selectorKey, selectorValue: selectorValue,
	})
	return nil
}

func pairs(value string) (map[string]string, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return nil, errNeeds
	}
	out := make(map[string]string, len(fields))
	for _, field := range fields {
		key, raw, ok := strings.Cut(field, "=")
		if !ok || key == "" || raw == "" {
			return nil, errNeeds
		}
		if _, exists := out[key]; exists {
			return nil, errNeeds
		}
		out[key] = raw
	}
	return out, nil
}

// applyDefaults fills a section only when the product did not mention it.
// Ambient mesh mode is the Package custom resource default and is always written.
func applyDefaults(parsed *productNeeds) {
	selectorKey := "app.kubernetes.io/name"
	if !parsed.ssoNone && len(parsed.sso) == 0 {
		parsed.sso = []ssoClient{{
			name:     parsed.name,
			clientID: parsed.name,
			protocol: "openid-connect",
			redirect: "https://" + parsed.name + ".example/callback",
			secret:   parsed.name + "-sso",
		}}
	}
	if !parsed.networkNone && len(parsed.network) == 0 {
		base := allowRule{
			direction:       "Egress",
			description:     "default-name-resolution",
			selectorKey:     selectorKey,
			selectorValue:   parsed.name,
			remoteNamespace: "kube-system",
			remoteKey:       "k8s-app",
			remoteValue:     "kube-dns",
			port:            53,
		}
		udp := base
		udp.protocol = "UDP"
		tcp := base
		tcp.protocol = "TCP"
		parsed.network = []allowRule{udp, tcp}
	}
	if !parsed.monitorNone && len(parsed.monitor) == 0 {
		parsed.monitor = []monitorRule{{
			description:   "application-metrics",
			portName:      "http",
			targetPort:    parsed.port,
			path:          "/metrics",
			selectorKey:   selectorKey,
			selectorValue: parsed.name,
		}}
	}
	for i := range parsed.monitor {
		if parsed.monitor[i].selectorValue == "" {
			parsed.monitor[i].selectorKey = selectorKey
			parsed.monitor[i].selectorValue = parsed.name
		}
		if parsed.monitor[i].targetPort == 0 {
			parsed.monitor[i].targetPort = parsed.port
		}
	}
	for i := range parsed.network {
		if parsed.network[i].selectorValue == "" {
			parsed.network[i].selectorKey = selectorKey
			parsed.network[i].selectorValue = parsed.name
		}
	}
}

func validateBindings(sourceDigest, templateVersion, toolVersions string) error {
	if !digestPattern.MatchString(sourceDigest) || !bindingPattern.MatchString(templateVersion) || !bindingPattern.MatchString(toolVersions) {
		return errNeeds
	}
	return nil
}

func yq(value string) string {
	return `"` + value + `"`
}

func renderZarf(parsed productNeeds, sourceDigest, templateVersion, toolVersions string) []byte {
	var b strings.Builder
	b.WriteString("apiVersion: zarf.dev/v1beta1\n")
	b.WriteString("kind: ZarfPackageConfig\n")
	b.WriteString("metadata:\n")
	b.WriteString("  name: " + yq(parsed.name) + "\n")
	b.WriteString("  description: " + yq("Generated package for "+parsed.name) + "\n")
	b.WriteString("  version: " + yq(parsed.version) + "\n")
	b.WriteString("  annotations:\n")
	b.WriteString("    packaging.conduit.dev/source-digest: " + yq(sourceDigest) + "\n")
	b.WriteString("    packaging.conduit.dev/template-version: " + yq(templateVersion) + "\n")
	b.WriteString("    packaging.conduit.dev/tool-versions: " + yq(toolVersions) + "\n")
	b.WriteString("    packaging.conduit.dev/value-tier: " + yq("CONDUIT_NATIVE") + "\n")
	b.WriteString("components:\n")
	b.WriteString("  - name: " + yq(parsed.name) + "\n")
	b.WriteString("    optional: false\n")
	b.WriteString("    manifests:\n")
	b.WriteString("      - name: " + yq("uds-package") + "\n")
	b.WriteString("        namespace: " + yq(parsed.name) + "\n")
	b.WriteString("        files:\n")
	b.WriteString("          - " + yq("uds-package.yaml") + "\n")
	if parsed.image != "" {
		b.WriteString("    images:\n")
		b.WriteString("      - name: " + yq(parsed.image) + "\n")
	}
	return []byte(b.String())
}

func renderPackage(parsed productNeeds, sourceDigest string) []byte {
	var b strings.Builder
	b.WriteString("apiVersion: uds.dev/v1alpha1\n")
	b.WriteString("kind: Package\n")
	b.WriteString("metadata:\n")
	b.WriteString("  name: " + yq(parsed.name) + "\n")
	b.WriteString("  namespace: " + yq(parsed.name) + "\n")
	b.WriteString("  annotations:\n")
	b.WriteString("    packaging.conduit.dev/source-digest: " + yq(sourceDigest) + "\n")
	b.WriteString("    packaging.conduit.dev/value-tier: " + yq("CONDUIT_NATIVE") + "\n")
	b.WriteString("spec:\n")
	b.WriteString("  network:\n")
	b.WriteString("    serviceMesh:\n")
	b.WriteString("      mode: " + yq(parsed.mesh) + "\n")
	writeAllows(&b, parsed.network)
	writeSSO(&b, parsed.sso)
	writeMonitors(&b, parsed.monitor)
	return []byte(b.String())
}

func writeAllows(b *strings.Builder, rules []allowRule) {
	if len(rules) == 0 {
		b.WriteString("    allow: []\n")
		return
	}
	b.WriteString("    allow:\n")
	for _, rule := range rules {
		b.WriteString("      - direction: " + yq(rule.direction) + "\n")
		if rule.description != "" {
			b.WriteString("        description: " + yq(rule.description) + "\n")
		}
		b.WriteString("        selector:\n")
		b.WriteString("          " + yq(rule.selectorKey) + ": " + yq(rule.selectorValue) + "\n")
		b.WriteString("        remoteNamespace: " + yq(rule.remoteNamespace) + "\n")
		b.WriteString("        remoteSelector:\n")
		b.WriteString("          " + yq(rule.remoteKey) + ": " + yq(rule.remoteValue) + "\n")
		b.WriteString("        port: " + strconv.Itoa(rule.port) + "\n")
		b.WriteString("        remoteProtocol: " + yq(rule.protocol) + "\n")
	}
}

func writeSSO(b *strings.Builder, clients []ssoClient) {
	if len(clients) == 0 {
		b.WriteString("  sso: []\n")
		return
	}
	b.WriteString("  sso:\n")
	for _, client := range clients {
		b.WriteString("    - name: " + yq(client.name) + "\n")
		b.WriteString("      clientId: " + yq(client.clientID) + "\n")
		b.WriteString("      protocol: " + yq(client.protocol) + "\n")
		b.WriteString("      redirectUris:\n")
		b.WriteString("        - " + yq(client.redirect) + "\n")
		b.WriteString("      secretConfig:\n")
		b.WriteString("        name: " + yq(client.secret) + "\n")
	}
}

func writeMonitors(b *strings.Builder, rules []monitorRule) {
	if len(rules) == 0 {
		b.WriteString("  monitor: []\n")
		return
	}
	b.WriteString("  monitor:\n")
	for _, rule := range rules {
		b.WriteString("    - portName: " + yq(rule.portName) + "\n")
		b.WriteString("      targetPort: " + strconv.Itoa(rule.targetPort) + "\n")
		b.WriteString("      path: " + yq(rule.path) + "\n")
		b.WriteString("      selector:\n")
		b.WriteString("        " + yq(rule.selectorKey) + ": " + yq(rule.selectorValue) + "\n")
		if rule.description != "" {
			b.WriteString("      description: " + yq(rule.description) + "\n")
		}
	}
}
