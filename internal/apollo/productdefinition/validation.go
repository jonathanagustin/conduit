// Package productdefinition owns cross-document Apollo compatibility validation.
package productdefinition

import (
	"fmt"
	"github.com/jonathanagustin/conduit/internal/apollo/productconfiguration"
	"github.com/jonathanagustin/conduit/internal/apollo/productmanifest"
	subst "github.com/jonathanagustin/conduit/internal/apollo/substitution"
	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Input is a read-only view of the documents and available template names.
// Decoding and native Release conversion remain with their existing entry points.
type Input struct {
	Manifest      productmanifest.Manifest
	Configuration productconfiguration.Configuration
	Templates     []string
}

var (
	apolloNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	// Endpoint names are kebab-cased over a deliberately narrow character set:
	// they become DNS and Service object names downstream, where case is not
	// preserved.
	apolloEndpointNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	// Storage sizes are a whole number of megabytes, gigabytes, or terabytes.
	apolloVolumeSizePattern = regexp.MustCompile(`^([0-9]+)(M|G|T)$`)
)

var (
	apolloVolumeSizeScales = map[string]float64{"M": 1e6, "G": 1e9, "T": 1e12}
)

func ValidateConfiguration(definition Input) []validationdomain.Problem {
	errs := make([]validationdomain.Problem, 0)
	add := func(path, message string) {
		errs = append(errs, validationdomain.Problem{Path: path, Message: message})
	}
	configuration := definition.Configuration

	if resources := configuration.Resources; resources != nil {
		requestCPU, requestMemory := productconfiguration.ValidateResourceQuantities("configuration.resources.requests", resources.Requests, &errs)
		limitCPU, limitMemory := productconfiguration.ValidateResourceQuantities("configuration.resources.limits", resources.Limits, &errs)
		if requestCPU > 0 && limitCPU > 0 && requestCPU > limitCPU {
			add("configuration.resources.requests.cpu", "must not exceed the CPU limit")
		}
		if requestMemory > 0 && limitMemory > 0 && requestMemory > limitMemory {
			add("configuration.resources.requests.memory", "must not exceed the memory limit")
		}
	}
	// Zero replicas is not the documented way to stop a workload: desired must be
	// greater than zero, and an installation is stopped through its Entity rather
	// than by publishing a Product that asks for nothing.
	if replication := configuration.Replication; replication != nil && replication.Desired != nil && *replication.Desired < 1 {
		add("configuration.replication.desired", "must be greater than zero")
	}

	validateApolloVolumeConfiguration(definition, &errs)
	definitions := ValidateEndpoints(definition, &errs)
	validateApolloManagedFiles(definition, &errs)

	for _, name := range sortedKeys(configuration.GeneratedSecrets) {
		ValidateGeneratedSecret("configuration.generated_secrets."+name, name, configuration.GeneratedSecrets[name], &errs)
	}

	validateApolloConfTags(configuration.Conf, &errs)
	produced, consumed := ValidateDiscovery(configuration, &errs)
	errs = append(errs, validateApolloDefinitionSubstitutions(definition, definitions, produced, consumed)...)
	return errs
}

// validateApolloVolumeConfiguration checks the volumes section against the
// manifest in both directions. A configured volume with no declaration is a size
// the platform will not provision; a declared volume with no configuration is a
// mount the Product expects and will not get.
func validateApolloVolumeConfiguration(definition Input, errs *[]validationdomain.Problem) {
	add := func(path, message string) {
		*errs = append(*errs, validationdomain.Problem{Path: path, Message: message})
	}
	declaredVolumes := definition.Manifest.Extensions.VolumesV2
	configured := definition.Configuration.Volumes
	for _, name := range sortedKeys(configured) {
		errorPath := "configuration.volumes." + name
		declared, exists := declaredVolumes[name]
		if !exists {
			add(errorPath, "must name a volume declared by the volumes-v2 manifest extension")
		}
		volume := configured[name]
		if strings.TrimSpace(volume.DesiredSize) != "" {
			add(errorPath+".desired-size", "must be nested under durable-storage-configuration")
		}
		if volume.DurableStorageConfiguration == nil {
			add(errorPath, "must declare durable-storage-configuration")
			continue
		}
		// The two unions have to agree: durable-storage-configuration is the
		// configuration for a durable-volume, and pairing it with any other
		// declared type would configure storage the volume does not have.
		if exists && declared.VolumeType.DurableVolume == nil {
			add(errorPath+".durable-storage-configuration",
				"volume is not declared as a durable-volume")
		}
		validateApolloVolumeSize(
			errorPath+".durable-storage-configuration.desired-size",
			volume.DurableStorageConfiguration.DesiredSize, errs)
	}
	for _, name := range sortedKeys(declaredVolumes) {
		if _, exists := configured[name]; !exists {
			add("manifest.extensions.volumes-v2."+name,
				"must have corresponding configuration under the volumes key")
		}
	}
}

// validateApolloVolumeSize checks a desired-size against the storage grammar,
// which is narrower than the compute one: a whole number of megabytes,
// gigabytes, or terabytes, with the unit required.
func validateApolloVolumeSize(path, value string, errs *[]validationdomain.Problem) {
	add := func(message string) { *errs = append(*errs, validationdomain.Problem{Path: path, Message: message}) }
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		add("must be a positive integer followed by M, G, or T")
		return
	}
	match := apolloVolumeSizePattern.FindStringSubmatch(trimmed)
	if match == nil {
		add("must be a positive integer followed by M, G, or T")
		return
	}
	amount, err := strconv.ParseFloat(match[1], 64)
	if err != nil || amount <= 0 {
		add("must be greater than zero")
		return
	}
	size := amount * apolloVolumeSizeScales[match[2]]
	maximum, _ := parseApolloVolumeSize(productconfiguration.MaximumVolumeSize)
	if size > maximum {
		add("must not exceed " + productconfiguration.MaximumVolumeSize)
	}
}

// parseApolloVolumeSize converts a storage quantity to a comparable magnitude.
func parseApolloVolumeSize(value string) (float64, bool) {
	match := apolloVolumeSizePattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, false
	}
	amount, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	return amount * apolloVolumeSizeScales[match[2]], true
}

// ValidateVolumeOverride checks an installation's replacement size for one
// volume. Only upsizing is permitted: a smaller volume cannot hold what the
// current one does, so the platform would have to discard data to satisfy it.
func ValidateVolumeOverride(name, current, override string) error {
	existing, ok := parseApolloVolumeSize(current)
	if !ok {
		return fmt.Errorf("volume %s has an unparseable current size %q", name, current)
	}
	requested, ok := parseApolloVolumeSize(override)
	if !ok {
		return fmt.Errorf("volume %s override size %q must be a positive integer followed by M, G, or T", name, override)
	}
	if requested < existing {
		return fmt.Errorf("volume %s may only be upsized: %s is smaller than %s", name, override, current)
	}
	return nil
}

// ValidateEndpoints checks the endpoint definitions and the manifest's
// ingress extensions against each other, and returns the definitions so the
// substitution checks can reuse them.
func ValidateEndpoints(definition Input, errs *[]validationdomain.Problem) map[string]productconfiguration.EndpointDefinition {
	add := func(path, message string) {
		*errs = append(*errs, validationdomain.Problem{Path: path, Message: message})
	}
	definitions := map[string]productconfiguration.EndpointDefinition{}
	configuration := definition.Configuration
	if configuration.Endpoints != nil {
		definitions = configuration.Endpoints.Definitions
	}

	// A raw endpoint has no request path to route on and needs an external port
	// allocated; a proxied endpoint is the reverse. Which list a name appears in
	// decides which of the two fields the definition must carry.
	raw := map[string]string{}
	for _, name := range definition.Manifest.Extensions.PublicTCPEndpoints {
		raw[name] = "public-tcp-endpoints"
	}
	for _, name := range definition.Manifest.Extensions.PublicUDPEndpoints {
		if existing, exists := raw[name]; exists {
			raw[name] = existing + " and public-udp-endpoints"
			continue
		}
		raw[name] = "public-udp-endpoints"
	}
	proxied := make(map[string]struct{}, len(definition.Manifest.Extensions.PublicProxyEndpoints))
	for _, name := range definition.Manifest.Extensions.PublicProxyEndpoints {
		proxied[name] = struct{}{}
	}

	// Two endpoints may share a port only when their paths distinguish them.
	type route struct {
		port int
		path string
	}
	routes := map[route]string{}

	for _, name := range sortedKeys(definitions) {
		errorPath := "configuration.endpoints.definitions." + name
		endpoint := definitions[name]
		if !apolloEndpointNamePattern.MatchString(name) {
			add(errorPath, "endpoint name must be kebab-cased from a-z and 0-9")
		}
		if endpoint.DesiredPort < productconfiguration.MinimumEndpointPort || endpoint.DesiredPort > productconfiguration.MaximumEndpointPort {
			add(errorPath+".desired-port", fmt.Sprintf("must be between %d and %d",
				productconfiguration.MinimumEndpointPort, productconfiguration.MaximumEndpointPort))
		}
		if endpoint.Path != "" && !strings.HasPrefix(endpoint.Path, "/") {
			add(errorPath+".path", "must be an absolute path")
		}
		if endpoint.ExternalPort != 0 && (endpoint.ExternalPort < 1 || endpoint.ExternalPort > 65535) {
			add(errorPath+".external-port", "must be between 1 and 65535")
		}
		if endpoint.DesiredPort != 0 {
			key := route{port: endpoint.DesiredPort, path: endpoint.EffectivePath()}
			if previous, exists := routes[key]; exists {
				add(errorPath, "must not repeat the port and path of "+previous)
			} else {
				routes[key] = name
			}
		}
		if list, exposed := raw[name]; exposed {
			if endpoint.ExternalPort == 0 {
				add(errorPath+".external-port", "must be set for an endpoint in "+list)
			}
			if endpoint.Path != "" {
				add(errorPath+".path", "must not be set for an endpoint in "+list)
			}
			continue
		}
		if endpoint.ExternalPort != 0 {
			add(errorPath+".external-port",
				"must only be set for an endpoint in public-tcp-endpoints or public-udp-endpoints")
		}
	}

	if configuration.Endpoints != nil {
		if len(definitions) == 0 {
			add("configuration.endpoints.definitions", "must define at least one endpoint")
		}
		status := configuration.Endpoints.SLSStatusEndpoint
		switch {
		case status != "":
			if _, exists := definitions[status]; !exists {
				add("configuration.endpoints.sls-status-endpoint", "must name an endpoints.definitions entry")
			}
		case len(definitions) > 1:
			// With one endpoint the choice is unambiguous. With more, an absent
			// selection means the platform would have to guess which port serves
			// the status resource it polls to decide the Product is alive.
			add("configuration.endpoints.sls-status-endpoint",
				"must be set when more than one endpoint is defined")
		}
	}

	for index, name := range definition.Manifest.Extensions.PublicProxyEndpoints {
		if _, exists := definitions[name]; name != "" && !exists {
			add(fmt.Sprintf("manifest.extensions.public-proxy-endpoints[%d]", index),
				"must name an endpoints.definitions entry")
		}
	}
	for index, name := range definition.Manifest.Extensions.PublicTCPEndpoints {
		if _, exists := definitions[name]; name != "" && !exists {
			add(fmt.Sprintf("manifest.extensions.public-tcp-endpoints[%d]", index),
				"must name an endpoints.definitions entry")
		}
	}
	return definitions
}

func validateApolloManagedFiles(definition Input, errs *[]validationdomain.Problem) {
	add := func(path, message string) {
		*errs = append(*errs, validationdomain.Problem{Path: path, Message: message})
	}
	configuration := definition.Configuration
	templates := make(map[string]struct{}, len(definition.Templates))
	for _, template := range definition.Templates {
		templates[path.Base(template)] = struct{}{}
	}
	for _, name := range sortedKeys(configuration.ManagedFiles) {
		file := configuration.ManagedFiles[name]
		errorPath := "configuration.managed_files." + name
		if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			add(errorPath, "must be a relative path inside the Product directory")
		}
		fileType := file.Type
		if file.Shorthand {
			// The shorthand's type is the key's extension, so an unknown
			// extension is not a defaulting question: there is no type to use.
			fileType = strings.TrimPrefix(path.Ext(name), ".")
			if fileType == "" {
				add(errorPath, "shorthand managed file name must have a type extension")
			}
		}
		switch fileType {
		case productconfiguration.ManagedFileTypeJSON, productconfiguration.ManagedFileTypeYAML, productconfiguration.ManagedFileTypeYML, productconfiguration.ManagedFileTypeTemplate:
		default:
			add(errorPath+".type", "must be json, yaml, yml, or tmpl")
		}
		switch file.ReloadType {
		case "", productconfiguration.ReloadTypeInstall, productconfiguration.ReloadTypeRuntime:
		default:
			add(errorPath+".reload-type", "must be install or runtime")
		}
		if fileType == productconfiguration.ManagedFileTypeTemplate {
			// A tmpl file renders from the whole conf tree through its template,
			// so it needs the template rather than a content selector.
			if definition.Templates != nil {
				if _, exists := templates[name+".tmpl"]; !exists {
					add(errorPath, fmt.Sprintf("requires a %s/%s.tmpl template", productconfiguration.TemplateDirectory, name))
				}
			}
			continue
		}
		content, isString := file.Content.(string)
		switch {
		case file.Content == nil:
			add(errorPath+".content", "must name a portion of the conf block")
		case isString:
			if _, exists := subst.LookupConfPath(configuration.Conf, []string{content}); !exists {
				add(errorPath+".content", "must name a key of the conf block")
			}
		}
	}
}

// validateApolloConfTags checks the reserved conf key that feeds discovery tags.
// It is the only part of the conf tree whose shape Platform constrains.
func validateApolloConfTags(conf map[string]any, errs *[]validationdomain.Problem) {
	add := func(path, message string) {
		*errs = append(*errs, validationdomain.Problem{Path: path, Message: message})
	}
	raw, exists := conf["tags"]
	if !exists {
		return
	}
	values, ok := raw.([]any)
	if !ok {
		add("configuration.conf.tags", "must be a list of tag names")
		return
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		tag, ok := value.(string)
		if !ok || !apolloNamePattern.MatchString(tag) {
			add(fmt.Sprintf("configuration.conf.tags[%d]", index), "must be a tag name")
			continue
		}
		if _, exists := seen[tag]; exists {
			add(fmt.Sprintf("configuration.conf.tags[%d]", index), "must not repeat a tag")
		}
		seen[tag] = struct{}{}
	}
}

func ValidateDiscovery(
	configuration productconfiguration.Configuration, errs *[]validationdomain.Problem,
) (map[string]struct{}, map[string]struct{}) {
	add := func(path, message string) {
		*errs = append(*errs, validationdomain.Problem{Path: path, Message: message})
	}
	produced := map[string]struct{}{}
	consumed := map[string]struct{}{}
	if configuration.Discovery == nil {
		return produced, consumed
	}
	for index, stack := range configuration.Discovery.ProduceToAdditionalStacks {
		if !apolloNamePattern.MatchString(stack) {
			add(fmt.Sprintf("configuration.discovery.produce-to-additional-stacks[%d]", index), "must be a stack name")
		}
	}
	for _, name := range sortedKeys(configuration.Discovery.Produces) {
		errorPath := "configuration.discovery.produces." + name
		produces := configuration.Discovery.Produces[name]
		if !apolloNamePattern.MatchString(name) {
			add(errorPath, "must be a discovery name")
		}
		produced[name] = struct{}{}
		if produces.Role != "" && !apolloNamePattern.MatchString(produces.Role) {
			add(errorPath+".role", "must be a discovery role name")
		}
		if produces.Port != 0 && (produces.Port < 1 || produces.Port > 65535) {
			add(errorPath+".port", "must be between 1 and 65535")
		}
		if produces.Path != "" && !strings.HasPrefix(produces.Path, "/") {
			add(errorPath+".path", "must be an absolute path")
		}
		switch produces.Scheme {
		case "", "http", "https":
		default:
			add(errorPath+".scheme", "must be http or https")
		}
		// scheme and path only mean anything once a port makes the entry a URI
		// candidate; stating them without one is a reference that will resolve to
		// no uri at install time.
		if produces.Port == 0 && (produces.Scheme != "" || produces.Path != "") {
			add(errorPath+".port", "must be set for an entry declaring a scheme or path")
		}
	}
	for _, name := range sortedKeys(configuration.Discovery.Consumes) {
		errorPath := "configuration.discovery.consumes." + name
		consumes := configuration.Discovery.Consumes[name]
		if !apolloNamePattern.MatchString(name) {
			add(errorPath, "must be a discovery name")
		}
		consumed[name] = struct{}{}
		if consumes.Role == "" {
			add(errorPath+".role", "must name the role the entry consumes")
		} else if !apolloNamePattern.MatchString(consumes.Role) {
			add(errorPath+".role", "must be a discovery role name")
		}
		if consumes.Stack != "" && !apolloNamePattern.MatchString(consumes.Stack) {
			add(errorPath+".stack", "must be a stack name")
		}
		if consumes.Select != "" && !apolloNamePattern.MatchString(consumes.Select) {
			add(errorPath+".select", "must name a field of the produced entry")
		}
		seen := make(map[string]struct{}, len(consumes.Tags))
		for index, tag := range consumes.Tags {
			if !apolloNamePattern.MatchString(tag) {
				add(fmt.Sprintf("%s.tags[%d]", errorPath, index), "must be a tag name")
			}
			if _, exists := seen[tag]; exists {
				add(fmt.Sprintf("%s.tags[%d]", errorPath, index), "must not repeat a tag")
			}
			seen[tag] = struct{}{}
		}
	}
	return produced, consumed
}

func ValidateGeneratedSecret(path, name string, secret productconfiguration.GeneratedSecret, errs *[]validationdomain.Problem) {
	add := func(field, message string) {
		*errs = append(*errs, validationdomain.Problem{Path: path + field, Message: message})
	}
	if !apolloNamePattern.MatchString(name) {
		add("", "secret name must be alphanumeric with hyphens or underscores")
	}
	if secret.Length < 0 {
		add(".length", "must not be negative")
	}
	charsetTyped := secret.SecretType == productconfiguration.SecretTypeRandomString
	if !charsetTyped && (secret.AllowChars != "" || secret.DisallowChars != "") {
		add("", "allow-chars and disallow-chars apply only to random_string")
	}
	if secret.AllowChars != "" && secret.DisallowChars != "" {
		add("", "must not set both allow-chars and disallow-chars")
	}
	switch secret.SecretType {
	case productconfiguration.SecretTypeRandomString, productconfiguration.SecretTypeRandomBytes, productconfiguration.SecretTypeSTIGPassword:
	case productconfiguration.SecretTypeRSAPKCS8:
		switch secret.Length {
		case 0, 2048, 3072, 4096:
		default:
			add(".length", "must be 2048, 3072, or 4096")
		}
	case productconfiguration.SecretTypeECPKCS8:
		// The standard curves the specification names. Its own example writes
		// 2048 here, which is an RSA key size and not one of the curves it lists
		// two paragraphs earlier; the enumerated values win.
		switch secret.Length {
		case 0, 224, 256, 384, 521:
		default:
			add(".length", "must be 224, 256, 384, or 521")
		}
	default:
		add(".secret-type",
			"must be random_string, random_bytes, rsa_pkcs8, ec_pkcs8, or stig_password")
	}
}

// validateApolloDefinitionSubstitutions walks every string the definition
// carries and checks that each substitution names something the definition
// declares. Template managed files are skipped: their content is a Go template
// rendered from the substituted conf tree, and its braces are not this
// language's.
func validateApolloDefinitionSubstitutions(
	definition Input, definitions map[string]productconfiguration.EndpointDefinition,
	produced, consumed map[string]struct{},
) []validationdomain.Problem {
	errs := make([]validationdomain.Problem, 0)
	configuration := definition.Configuration
	check := func(path, value string) {
		substitutions, err := subst.Parse(value)
		if err != nil {
			errs = append(errs, validationdomain.Problem{Path: path, Message: err.Error()})
			return
		}
		for _, substitution := range substitutions {
			if message := apolloSubstitutionTargetError(substitution, definition, definitions, produced, consumed); message != "" {
				errs = append(errs, validationdomain.Problem{Path: path, Message: message})
			}
		}
	}
	walkApolloStrings("configuration.conf", configuration.Conf, check)
	if configuration.Discovery != nil {
		for _, name := range sortedKeys(configuration.Discovery.Produces) {
			produces := configuration.Discovery.Produces[name]
			for _, key := range sortedKeys(produces.Payload) {
				node := produces.Payload[key]
				var value any
				if err := node.Decode(&value); err != nil {
					continue
				}
				walkApolloStrings(
					fmt.Sprintf("configuration.discovery.produces.%s.%s", name, key), value, check)
			}
		}
	}
	return errs
}

func apolloSubstitutionTargetError(
	substitution subst.Substitution, definition Input,
	definitions map[string]productconfiguration.EndpointDefinition, produced, consumed map[string]struct{},
) string {
	switch substitution.Kind {
	case subst.KindSecret:
		if _, exists := definition.Configuration.GeneratedSecrets[substitution.Argument]; !exists {
			return fmt.Sprintf("substitution %s names no generated_secrets entry", substitution.Raw)
		}
	case subst.KindDiscovered:
		if _, exists := consumed[substitution.Argument]; !exists {
			return fmt.Sprintf("substitution %s names no discovery.consumes entry", substitution.Raw)
		}
	case subst.KindSelfDiscovered:
		if _, exists := produced[substitution.Argument]; !exists {
			return fmt.Sprintf("substitution %s names no discovery.produces entry", substitution.Raw)
		}
	case subst.KindConf:
		if _, exists := subst.LookupConfPath(
			definition.Configuration.Conf, substitution.ConfSegments()); !exists {
			return fmt.Sprintf("substitution %s names no conf entry", substitution.Raw)
		}
	case subst.KindVolume:
		if _, exists := definition.Manifest.Extensions.VolumesV2[substitution.Argument]; !exists {
			return fmt.Sprintf("substitution %s names no volumes-v2 volume", substitution.Raw)
		}
	case subst.KindEndpoint:
		if _, exists := definitions[substitution.Argument]; !exists {
			return fmt.Sprintf("substitution %s names no endpoints.definitions entry", substitution.Raw)
		}
		if _, known := subst.CanonicalEndpointField(substitution.Field); !known {
			return fmt.Sprintf("substitution %s names an unknown endpoint field", substitution.Raw)
		}
	case subst.KindSSL:
		if _, known := subst.SSLFile(substitution.Argument); !known {
			return fmt.Sprintf("substitution %s names an unknown ssl field", substitution.Raw)
		}
	}
	return ""
}

func walkApolloStrings(path string, value any, visit func(path, value string)) {
	switch typed := value.(type) {
	case string:
		visit(path, typed)
	case map[string]any:
		for _, key := range sortedKeys(typed) {
			walkApolloStrings(path+"."+key, typed[key], visit)
		}
	case map[any]any:
		keys := make([]string, 0, len(typed))
		converted := make(map[string]any, len(typed))
		for key, child := range typed {
			name := fmt.Sprint(key)
			keys = append(keys, name)
			converted[name] = child
		}
		sort.Strings(keys)
		for _, key := range keys {
			walkApolloStrings(path+"."+key, converted[key], visit)
		}
	case []any:
		for index, child := range typed {
			walkApolloStrings(fmt.Sprintf("%s[%d]", path, index), child, visit)
		}
	}
}

func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
