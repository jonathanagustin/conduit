package controlplane

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The Apollo Product Specification lets configuration.yml refer to values that
// only exist once an installation is placed: a generated secret, a discovered
// peer, the path a volume was mounted at, the port an endpoint was bound to. It
// also lets configuration refer to itself, so a block written once can be
// spliced into several consumers. Those references are written in a double-curly
// substitution language.
//
// The language matters to Platform for one reason: it is the only place a
// Product Definition can name something it does not itself declare. A
// `{{secret("db-password")}}` whose secret is not in generated_secrets, or a
// `{{discovered.catalog}}` with no matching discovery.consumes entry, is a
// definition that cannot be installed. Catching that at publication time is the
// whole point of validating a Product Definition rather than only decoding it.
//
// This is a reconstructed model of a documented authoring surface. It is not a
// claim of compatibility with any private Apollo template engine.

// ApolloSubstitutionKind names the head of a substitution expression.
type ApolloSubstitutionKind string

const (
	// ApolloSubstitutionSecret is secret("name") or secret("name", part: p),
	// resolving to the value of a generated or operator-supplied secret.
	ApolloSubstitutionSecret ApolloSubstitutionKind = "secret"
	// ApolloSubstitutionDiscovered is discovered.<name>[.<field>], resolving
	// against a discovery.consumes entry.
	ApolloSubstitutionDiscovered ApolloSubstitutionKind = "discovered"
	// ApolloSubstitutionSelfDiscovered is self_discovered.<name>[.<field>],
	// resolving against this Product's own discovery.produces entry.
	ApolloSubstitutionSelfDiscovered ApolloSubstitutionKind = "self_discovered"
	// ApolloSubstitutionConf is conf.<path>, splicing one part of the Product's
	// own configuration into another.
	ApolloSubstitutionConf ApolloSubstitutionKind = "conf"
	// ApolloSubstitutionSSL is ssl.<field>, resolving into the certificate mount.
	ApolloSubstitutionSSL ApolloSubstitutionKind = "ssl"
	// ApolloSubstitutionVolume is volumes.<name>.PathOnDisk.
	ApolloSubstitutionVolume ApolloSubstitutionKind = "volumes"
	// ApolloSubstitutionEndpoint is endpoints.definitions.<name>.<field>.
	ApolloSubstitutionEndpoint ApolloSubstitutionKind = "endpoints"
)

// Files the platform mounts into the certificate directory. They are constants
// because ssl.* substitutions resolve to these exact names; renaming one would
// silently change what a Product reads at startup.
const (
	ApolloCertFileCA          = "ca.cer"
	ApolloCertFileKey         = "key.pem"
	ApolloCertFileCertificate = "cert.cer"
	ApolloCertFileTrustStore  = "truststore.jks"
	ApolloCertFileKeyStore    = "keystore.jks"
)

// Secret parts. A secret can be referenced by its value or by a derived form;
// bcrypt_hash exists so a Product can publish a verifier for its own shared
// secret through discovery without publishing the secret.
const (
	ApolloSecretPartValue      = "value"
	ApolloSecretPartBcryptHash = "bcrypt_hash"
)

// apolloSSLFields maps each accepted ssl.<field> to the mounted file it names.
//
// The specification's worked examples use pem_path, cert_path, and ca_path.
// keystore_path and truststore_path are included because the same section
// documents keystore.jks and truststore.jks as part of the mount, and a Java
// Product that cannot name them would have to hardcode the mount layout. They
// are marked as derived rather than quoted in the contract note.
var apolloSSLFields = map[string]string{
	"pem_path":        ApolloCertFileKey,
	"cert_path":       ApolloCertFileCertificate,
	"ca_path":         ApolloCertFileCA,
	"keystore_path":   ApolloCertFileKeyStore,
	"truststore_path": ApolloCertFileTrustStore,
}

// ApolloCertFiles lists the mounted certificate material in a stable order.
func ApolloCertFiles() []string {
	return []string{
		ApolloCertFileCA, ApolloCertFileKey, ApolloCertFileCertificate,
		ApolloCertFileTrustStore, ApolloCertFileKeyStore,
	}
}

// ApolloSSLFields lists the accepted ssl.<field> names in a stable order.
func ApolloSSLFields() []string {
	fields := make([]string, 0, len(apolloSSLFields))
	for field := range apolloSSLFields {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

// apolloEndpointFields are the endpoint properties a substitution may read.
//
// The port is spelled both ways in the specification: the Product Specification
// writes endpoints.definitions.<name>.desired-port and the Endpoints extension
// writes .port for the same value. Both resolve to the container port, so both
// are accepted rather than picking one and failing documents written against
// the other page.
var apolloEndpointFields = map[string]string{
	"desired-port":  "desired-port",
	"port":          "desired-port",
	"external-port": "external-port",
	"path":          "path",
}

// ApolloEndpointFields lists the endpoint properties a substitution may read.
func ApolloEndpointFields() []string {
	fields := make([]string, 0, len(apolloEndpointFields))
	for field := range apolloEndpointFields {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

// ApolloCanonicalEndpointField maps an endpoint substitution field to the one
// definition key it reads, so a resolver keys on one spelling.
func ApolloCanonicalEndpointField(field string) (string, bool) {
	canonical, known := apolloEndpointFields[field]
	return canonical, known
}

// ApolloSubstitution is one parsed {{ ... }} reference.
type ApolloSubstitution struct {
	// Raw is the complete token including braces, so a resolver can replace
	// exactly what it matched rather than re-deriving the spelling.
	Raw string `json:"raw"`
	// Kind is the head of the expression.
	Kind ApolloSubstitutionKind `json:"kind"`
	// Argument is the secret, discovery, volume, endpoint, conf root, or ssl
	// field name.
	Argument string `json:"argument"`
	// Field is the property path after the argument, empty when absent.
	Field string `json:"field,omitempty"`
	// Part is the secret() part selector, empty when the reference is the
	// secret's own value.
	Part string `json:"part,omitempty"`
}

// Reference renders the substitution's lookup key: the argument, joined to its
// field when one is present. Resolvers key on this so a caller supplies one flat
// map instead of a nested one.
func (substitution ApolloSubstitution) Reference() string {
	if substitution.Field == "" {
		return substitution.Argument
	}
	return substitution.Argument + "." + substitution.Field
}

var (
	apolloSubstitutionTokenPattern = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	// The call form accepts either quote style and tolerates surrounding
	// whitespace, matching how the documented examples are written. The optional
	// second argument is the `part:` selector, whose value may be quoted or bare
	// because both spellings appear in the specification.
	apolloSubstitutionCallPattern = regexp.MustCompile(
		`^([a-z_][a-z0-9_]*)\s*\(\s*(?:"([^"]*)"|'([^']*)')\s*` +
			`(?:,\s*part\s*:\s*(?:"([^"]*)"|'([^']*)'|([A-Za-z0-9_]+))\s*)?\)$`)
	apolloSubstitutionPathPattern = regexp.MustCompile(
		`^([A-Za-z_][A-Za-z0-9_-]*)((?:\.[A-Za-z0-9_-]+)+)$`)
)

// ParseApolloSubstitutions extracts every substitution in a string. An
// unparseable or unknown token is an error rather than a value passed through
// unresolved: a Product that starts with a literal "{{secret(...)}}" in its
// configuration has silently lost its credential.
func ParseApolloSubstitutions(value string) ([]ApolloSubstitution, error) {
	tokens := apolloSubstitutionTokenPattern.FindAllString(value, -1)
	if len(tokens) == 0 {
		return nil, nil
	}
	substitutions := make([]ApolloSubstitution, 0, len(tokens))
	for _, token := range tokens {
		substitution, err := parseApolloSubstitution(token)
		if err != nil {
			return nil, err
		}
		substitutions = append(substitutions, substitution)
	}
	return substitutions, nil
}

func parseApolloSubstitution(token string) (ApolloSubstitution, error) {
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(token, "{{"), "}}"))
	if match := apolloSubstitutionCallPattern.FindStringSubmatch(body); match != nil {
		if match[1] != string(ApolloSubstitutionSecret) {
			return ApolloSubstitution{}, fmt.Errorf("substitution %s: unknown helper %q", token, match[1])
		}
		part := match[4] + match[5] + match[6]
		switch part {
		case "", ApolloSecretPartValue, ApolloSecretPartBcryptHash:
		default:
			return ApolloSubstitution{}, fmt.Errorf(
				"substitution %s: secret part must be %s or %s", token, ApolloSecretPartValue, ApolloSecretPartBcryptHash)
		}
		return ApolloSubstitution{
			Raw: token, Kind: ApolloSubstitutionSecret, Argument: match[2] + match[3], Part: part,
		}, nil
	}
	if match := apolloSubstitutionPathPattern.FindStringSubmatch(body); match != nil {
		segments := strings.Split(strings.TrimPrefix(match[2], "."), ".")
		switch ApolloSubstitutionKind(match[1]) {
		case ApolloSubstitutionSSL:
			if len(segments) != 1 {
				return ApolloSubstitution{}, fmt.Errorf("substitution %s: ssl takes exactly one field", token)
			}
			return ApolloSubstitution{Raw: token, Kind: ApolloSubstitutionSSL, Argument: segments[0]}, nil
		case ApolloSubstitutionVolume:
			if len(segments) != 2 || segments[1] != "PathOnDisk" {
				return ApolloSubstitution{}, fmt.Errorf("substitution %s: volumes takes <name>.PathOnDisk", token)
			}
			return ApolloSubstitution{
				Raw: token, Kind: ApolloSubstitutionVolume, Argument: segments[0], Field: segments[1],
			}, nil
		case ApolloSubstitutionEndpoint:
			// The name is required. The specification has one example that omits
			// it (`endpoints.definitions.port`); read literally that names an
			// endpoint called "port", and accepting it would let a typo resolve to
			// nothing at install time.
			if len(segments) != 3 || segments[0] != "definitions" {
				return ApolloSubstitution{}, fmt.Errorf(
					"substitution %s: endpoints takes definitions.<name>.<field>", token)
			}
			return ApolloSubstitution{
				Raw: token, Kind: ApolloSubstitutionEndpoint, Argument: segments[1], Field: segments[2],
			}, nil
		case ApolloSubstitutionDiscovered:
			return ApolloSubstitution{
				Raw: token, Kind: ApolloSubstitutionDiscovered,
				Argument: segments[0], Field: strings.Join(segments[1:], "."),
			}, nil
		case ApolloSubstitutionSelfDiscovered:
			return ApolloSubstitution{
				Raw: token, Kind: ApolloSubstitutionSelfDiscovered,
				Argument: segments[0], Field: strings.Join(segments[1:], "."),
			}, nil
		case ApolloSubstitutionConf:
			return ApolloSubstitution{
				Raw: token, Kind: ApolloSubstitutionConf,
				Argument: segments[0], Field: strings.Join(segments[1:], "."),
			}, nil
		}
	}
	return ApolloSubstitution{}, fmt.Errorf("substitution %s is not a recognized Apollo substitution", token)
}

// ApolloSubstitutionContext supplies resolved values, keyed by
// ApolloSubstitution.Reference. It is deliberately flat: resolution happens once
// an installation is placed, and by then every reference has a single value.
type ApolloSubstitutionContext struct {
	// Secrets is keyed by name for a plain reference and by "name#part" for a
	// part reference, so a bcrypt hash cannot be served where the raw secret was
	// asked for.
	Secrets        map[string]string
	Discovered     map[string]string
	SelfDiscovered map[string]string
	VolumePaths    map[string]string
	EndpointValues map[string]string
	// Conf is the Product's own configuration tree, used to resolve conf.<path>
	// self-references.
	Conf map[string]any
	// CertsMountPath is the directory the certificate material is mounted at.
	// Path-valued ssl fields are derived from it rather than supplied, so a
	// caller cannot point one Product at another's key material by mistake.
	CertsMountPath string
}

// ApolloSecretReference renders the key a secret substitution resolves under.
func ApolloSecretReference(name, part string) string {
	if part == "" || part == ApolloSecretPartValue {
		return name
	}
	return name + "#" + part
}

// ResolveApolloSubstitutions renders every substitution in a string. It fails
// closed: an unresolved reference returns an error instead of an empty string,
// because an empty credential or peer URI is the failure mode that reaches
// production looking like a working configuration.
func ResolveApolloSubstitutions(value string, context ApolloSubstitutionContext) (string, error) {
	substitutions, err := ParseApolloSubstitutions(value)
	if err != nil {
		return "", err
	}
	resolved := value
	for _, substitution := range substitutions {
		replacement, err := context.resolve(substitution)
		if err != nil {
			return "", err
		}
		resolved = strings.Replace(resolved, substitution.Raw, replacement, 1)
	}
	return resolved, nil
}

func (context ApolloSubstitutionContext) resolve(substitution ApolloSubstitution) (string, error) {
	lookup := func(values map[string]string) (string, error) {
		value, found := values[substitution.Reference()]
		if !found {
			return "", fmt.Errorf("substitution %s is unresolved", substitution.Raw)
		}
		return value, nil
	}
	switch substitution.Kind {
	case ApolloSubstitutionSecret:
		value, found := context.Secrets[ApolloSecretReference(substitution.Argument, substitution.Part)]
		if !found {
			return "", fmt.Errorf("substitution %s is unresolved", substitution.Raw)
		}
		return value, nil
	case ApolloSubstitutionDiscovered:
		return lookup(context.Discovered)
	case ApolloSubstitutionSelfDiscovered:
		return lookup(context.SelfDiscovered)
	case ApolloSubstitutionConf:
		value, found := lookupApolloConfPath(context.Conf, apolloConfSegments(substitution))
		if !found {
			return "", fmt.Errorf("substitution %s is unresolved", substitution.Raw)
		}
		return fmt.Sprint(value), nil
	case ApolloSubstitutionVolume:
		value, found := context.VolumePaths[substitution.Argument]
		if !found {
			return "", fmt.Errorf("substitution %s is unresolved", substitution.Raw)
		}
		return value, nil
	case ApolloSubstitutionEndpoint:
		return lookup(context.EndpointValues)
	case ApolloSubstitutionSSL:
		if context.CertsMountPath == "" {
			return "", fmt.Errorf("substitution %s requires a certificate mount path", substitution.Raw)
		}
		file, known := apolloSSLFields[substitution.Argument]
		if !known {
			return "", fmt.Errorf("substitution %s names an unknown ssl field", substitution.Raw)
		}
		return strings.TrimSuffix(context.CertsMountPath, "/") + "/" + file, nil
	}
	return "", fmt.Errorf("substitution %s has no resolver", substitution.Raw)
}

// apolloConfSegments splits a conf substitution back into its path segments.
func apolloConfSegments(substitution ApolloSubstitution) []string {
	segments := []string{substitution.Argument}
	if substitution.Field != "" {
		segments = append(segments, strings.Split(substitution.Field, ".")...)
	}
	return segments
}

// lookupApolloConfPath walks a conf tree. It reports whether the path exists
// rather than returning a zero value, because a conf reference that silently
// resolves to nothing is exactly the class of error this package exists to
// catch.
func lookupApolloConfPath(conf map[string]any, segments []string) (any, bool) {
	var current any = conf
	for _, segment := range segments {
		switch typed := current.(type) {
		case map[string]any:
			child, exists := typed[segment]
			if !exists {
				return nil, false
			}
			current = child
		case map[any]any:
			child, exists := typed[segment]
			if !exists {
				return nil, false
			}
			current = child
		default:
			return nil, false
		}
	}
	return current, true
}
