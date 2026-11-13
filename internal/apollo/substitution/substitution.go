// Package substitution is the Apollo double-curly substitution language: the
// vocabulary a configuration.yml uses to name a value it does not itself hold.
//
// It lives in the Apollo integration context because that context owns pinned
// Apollo vocabulary, and this is vocabulary in its purest form -- the grammar
// and the accepted field names come from the published Product Specification,
// not from anything Conduit decided. Nothing here knows what a Product, a
// Release or an Environment is, which is why it can sit below the control
// plane rather than inside it.
package substitution

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

// Kind names the head of a substitution expression.
type Kind string

const (
	// KindSecret is secret("name") or secret("name", part: p),
	// resolving to the value of a generated or operator-supplied secret.
	KindSecret Kind = "secret"
	// KindDiscovered is discovered.<name>[.<field>], resolving
	// against a discovery.consumes entry.
	KindDiscovered Kind = "discovered"
	// KindSelfDiscovered is self_discovered.<name>[.<field>],
	// resolving against this Product's own discovery.produces entry.
	KindSelfDiscovered Kind = "self_discovered"
	// KindConf is conf.<path>, splicing one part of the Product's
	// own configuration into another.
	KindConf Kind = "conf"
	// KindSSL is ssl.<field>, resolving into the certificate mount.
	KindSSL Kind = "ssl"
	// KindVolume is volumes.<name>.PathOnDisk.
	KindVolume Kind = "volumes"
	// KindEndpoint is endpoints.definitions.<name>.<field>.
	KindEndpoint Kind = "endpoints"
)

// Files the platform mounts into the certificate directory. They are constants
// because ssl.* substitutions resolve to these exact names; renaming one would
// silently change what a Product reads at startup.
const (
	CertFileCA          = "ca.cer"
	CertFileKey         = "key.pem"
	CertFileCertificate = "cert.cer"
	CertFileTrustStore  = "truststore.jks"
	CertFileKeyStore    = "keystore.jks"
)

// Secret parts. A secret can be referenced by its value or by a derived form;
// bcrypt_hash exists so a Product can publish a verifier for its own shared
// secret through discovery without publishing the secret.
const (
	SecretPartValue      = "value"
	SecretPartBcryptHash = "bcrypt_hash"
)

// sslFiles maps each accepted ssl.<field> to the mounted file it names.
//
// The specification's worked examples use pem_path, cert_path, and ca_path.
// keystore_path and truststore_path are included because the same section
// documents keystore.jks and truststore.jks as part of the mount, and a Java
// Product that cannot name them would have to hardcode the mount layout. They
// are marked as derived rather than quoted in the contract note.
var sslFiles = map[string]string{
	"pem_path":        CertFileKey,
	"cert_path":       CertFileCertificate,
	"ca_path":         CertFileCA,
	"keystore_path":   CertFileKeyStore,
	"truststore_path": CertFileTrustStore,
}

// CertFiles lists the mounted certificate material in a stable order.
func CertFiles() []string {
	return []string{
		CertFileCA, CertFileKey, CertFileCertificate,
		CertFileTrustStore, CertFileKeyStore,
	}
}

// SSLFields lists the accepted ssl.<field> names in a stable order.
func SSLFields() []string {
	fields := make([]string, 0, len(sslFiles))
	for field := range sslFiles {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

// SSLFile reports which mounted file an ssl.<field> names, and whether the
// field is one this language accepts at all. Callers outside this package need
// the second answer to reject an unknown field at publication time, which is
// why the map itself stays unexported: there is exactly one way to ask.
func SSLFile(field string) (string, bool) {
	file, known := sslFiles[field]
	return file, known
}

// endpointFields are the endpoint properties a substitution may read.
//
// The port is spelled both ways in the specification: the Product Specification
// writes endpoints.definitions.<name>.desired-port and the Endpoints extension
// writes .port for the same value. Both resolve to the container port, so both
// are accepted rather than picking one and failing documents written against
// the other page.
var endpointFields = map[string]string{
	"desired-port":  "desired-port",
	"port":          "desired-port",
	"external-port": "external-port",
	"path":          "path",
}

// EndpointFields lists the endpoint properties a substitution may read.
func EndpointFields() []string {
	fields := make([]string, 0, len(endpointFields))
	for field := range endpointFields {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

// CanonicalEndpointField maps an endpoint substitution field to the one
// definition key it reads, so a resolver keys on one spelling.
func CanonicalEndpointField(field string) (string, bool) {
	canonical, known := endpointFields[field]
	return canonical, known
}

// Substitution is one parsed {{ ... }} reference.
type Substitution struct {
	// Raw is the complete token including braces, so a resolver can replace
	// exactly what it matched rather than re-deriving the spelling.
	Raw string `json:"raw"`
	// Kind is the head of the expression.
	Kind Kind `json:"kind"`
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
func (substitution Substitution) Reference() string {
	if substitution.Field == "" {
		return substitution.Argument
	}
	return substitution.Argument + "." + substitution.Field
}

var (
	tokenPattern = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	// The call form accepts either quote style and tolerates surrounding
	// whitespace, matching how the documented examples are written. The optional
	// second argument is the `part:` selector, whose value may be quoted or bare
	// because both spellings appear in the specification.
	callPattern = regexp.MustCompile(
		`^([a-z_][a-z0-9_]*)\s*\(\s*(?:"([^"]*)"|'([^']*)')\s*` +
			`(?:,\s*part\s*:\s*(?:"([^"]*)"|'([^']*)'|([A-Za-z0-9_]+))\s*)?\)$`)
	pathPattern = regexp.MustCompile(
		`^([A-Za-z_][A-Za-z0-9_-]*)((?:\.[A-Za-z0-9_-]+)+)$`)
)

// Parse extracts every substitution in a string. An
// unparseable or unknown token is an error rather than a value passed through
// unresolved: a Product that starts with a literal "{{secret(...)}}" in its
// configuration has silently lost its credential.
func Parse(value string) ([]Substitution, error) {
	tokens := tokenPattern.FindAllString(value, -1)
	if len(tokens) == 0 {
		return nil, nil
	}
	substitutions := make([]Substitution, 0, len(tokens))
	for _, token := range tokens {
		substitution, err := parseOne(token)
		if err != nil {
			return nil, err
		}
		substitutions = append(substitutions, substitution)
	}
	return substitutions, nil
}

func parseOne(token string) (Substitution, error) {
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(token, "{{"), "}}"))
	if match := callPattern.FindStringSubmatch(body); match != nil {
		if match[1] != string(KindSecret) {
			return Substitution{}, fmt.Errorf("substitution %s: unknown helper %q", token, match[1])
		}
		part := match[4] + match[5] + match[6]
		switch part {
		case "", SecretPartValue, SecretPartBcryptHash:
		default:
			return Substitution{}, fmt.Errorf(
				"substitution %s: secret part must be %s or %s", token, SecretPartValue, SecretPartBcryptHash)
		}
		return Substitution{
			Raw: token, Kind: KindSecret, Argument: match[2] + match[3], Part: part,
		}, nil
	}
	if match := pathPattern.FindStringSubmatch(body); match != nil {
		segments := strings.Split(strings.TrimPrefix(match[2], "."), ".")
		switch Kind(match[1]) {
		case KindSSL:
			if len(segments) != 1 {
				return Substitution{}, fmt.Errorf("substitution %s: ssl takes exactly one field", token)
			}
			return Substitution{Raw: token, Kind: KindSSL, Argument: segments[0]}, nil
		case KindVolume:
			if len(segments) != 2 || segments[1] != "PathOnDisk" {
				return Substitution{}, fmt.Errorf("substitution %s: volumes takes <name>.PathOnDisk", token)
			}
			return Substitution{
				Raw: token, Kind: KindVolume, Argument: segments[0], Field: segments[1],
			}, nil
		case KindEndpoint:
			// The name is required. The specification has one example that omits
			// it (`endpoints.definitions.port`); read literally that names an
			// endpoint called "port", and accepting it would let a typo resolve to
			// nothing at install time.
			if len(segments) != 3 || segments[0] != "definitions" {
				return Substitution{}, fmt.Errorf(
					"substitution %s: endpoints takes definitions.<name>.<field>", token)
			}
			return Substitution{
				Raw: token, Kind: KindEndpoint, Argument: segments[1], Field: segments[2],
			}, nil
		case KindDiscovered:
			return Substitution{
				Raw: token, Kind: KindDiscovered,
				Argument: segments[0], Field: strings.Join(segments[1:], "."),
			}, nil
		case KindSelfDiscovered:
			return Substitution{
				Raw: token, Kind: KindSelfDiscovered,
				Argument: segments[0], Field: strings.Join(segments[1:], "."),
			}, nil
		case KindConf:
			return Substitution{
				Raw: token, Kind: KindConf,
				Argument: segments[0], Field: strings.Join(segments[1:], "."),
			}, nil
		}
	}
	return Substitution{}, fmt.Errorf("substitution %s is not a recognized Apollo substitution", token)
}

// Context supplies resolved values, keyed by
// Substitution.Reference. It is deliberately flat: resolution happens once
// an installation is placed, and by then every reference has a single value.
type Context struct {
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

// SecretReference renders the key a secret substitution resolves under.
func SecretReference(name, part string) string {
	if part == "" || part == SecretPartValue {
		return name
	}
	return name + "#" + part
}

// Resolve renders every substitution in a string. It fails
// closed: an unresolved reference returns an error instead of an empty string,
// because an empty credential or peer URI is the failure mode that reaches
// production looking like a working configuration.
func Resolve(value string, context Context) (string, error) {
	substitutions, err := Parse(value)
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

func (context Context) resolve(substitution Substitution) (string, error) {
	lookup := func(values map[string]string) (string, error) {
		value, found := values[substitution.Reference()]
		if !found {
			return "", fmt.Errorf("substitution %s is unresolved", substitution.Raw)
		}
		return value, nil
	}
	switch substitution.Kind {
	case KindSecret:
		value, found := context.Secrets[SecretReference(substitution.Argument, substitution.Part)]
		if !found {
			return "", fmt.Errorf("substitution %s is unresolved", substitution.Raw)
		}
		return value, nil
	case KindDiscovered:
		return lookup(context.Discovered)
	case KindSelfDiscovered:
		return lookup(context.SelfDiscovered)
	case KindConf:
		value, found := LookupConfPath(context.Conf, substitution.ConfSegments())
		if !found {
			return "", fmt.Errorf("substitution %s is unresolved", substitution.Raw)
		}
		return fmt.Sprint(value), nil
	case KindVolume:
		value, found := context.VolumePaths[substitution.Argument]
		if !found {
			return "", fmt.Errorf("substitution %s is unresolved", substitution.Raw)
		}
		return value, nil
	case KindEndpoint:
		return lookup(context.EndpointValues)
	case KindSSL:
		if context.CertsMountPath == "" {
			return "", fmt.Errorf("substitution %s requires a certificate mount path", substitution.Raw)
		}
		file, known := SSLFile(substitution.Argument)
		if !known {
			return "", fmt.Errorf("substitution %s names an unknown ssl field", substitution.Raw)
		}
		return strings.TrimSuffix(context.CertsMountPath, "/") + "/" + file, nil
	}
	return "", fmt.Errorf("substitution %s has no resolver", substitution.Raw)
}

// ConfSegments splits a conf substitution back into its path segments.
func (substitution Substitution) ConfSegments() []string {
	segments := []string{substitution.Argument}
	if substitution.Field != "" {
		segments = append(segments, strings.Split(substitution.Field, ".")...)
	}
	return segments
}

// LookupConfPath walks a conf tree. It reports whether the path exists
// rather than returning a zero value, because a conf reference that silently
// resolves to nothing is exactly the class of error this package exists to
// catch.
//
// It is exported because resolution and publication-time validation ask the
// same question of the same tree -- does this reference name anything? -- and a
// validator that walked the tree its own way could disagree with the resolver
// that runs at install time.
func LookupConfPath(conf map[string]any, segments []string) (any, bool) {
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
