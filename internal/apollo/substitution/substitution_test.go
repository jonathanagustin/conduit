package substitution

import "testing"

// TestSubstitutionFormsFromTheSpecification parses every spelling the published
// examples use. Each one is a document a Product author could reasonably ship.
func TestSubstitutionFormsFromTheSpecification(t *testing.T) {
	for _, testCase := range []struct {
		value    string
		kind     Kind
		argument string
		field    string
		part     string
	}{
		{value: `{{secret("my-oauth-secret")}}`, kind: KindSecret, argument: "my-oauth-secret"},
		{
			value: `{{secret("my-oauth-secret", part: bcrypt_hash)}}`, kind: KindSecret,
			argument: "my-oauth-secret", part: SecretPartBcryptHash,
		},
		{
			value: `{{secret('my-oauth-secret', part: "bcrypt_hash")}}`, kind: KindSecret,
			argument: "my-oauth-secret", part: SecretPartBcryptHash,
		},
		{
			value: `{{discovered.my-neighbor-api-uris}}`, kind: KindDiscovered,
			argument: "my-neighbor-api-uris",
		},
		{
			value: `{{self_discovered.my-app-uris.uris}}`, kind: KindSelfDiscovered,
			argument: "my-app-uris", field: "uris",
		},
		{
			value: `{{ conf.some-other-config.c }}`, kind: KindConf,
			argument: "some-other-config", field: "c",
		},
		{value: `{{ssl.pem_path}}`, kind: KindSSL, argument: "pem_path"},
		{value: `{{ssl.cert_path}}`, kind: KindSSL, argument: "cert_path"},
		{value: `{{ssl.ca_path}}`, kind: KindSSL, argument: "ca_path"},
		{
			value: `{{ volumes.my-volume.PathOnDisk }}`, kind: KindVolume,
			argument: "my-volume", field: "PathOnDisk",
		},
		{
			value: `{{endpoints.definitions.foo-service-endpoint.desired-port}}`, kind: KindEndpoint,
			argument: "foo-service-endpoint", field: "desired-port",
		},
		{
			value: `{{ endpoints.definitions.service-endpoint.port}}`, kind: KindEndpoint,
			argument: "service-endpoint", field: "port",
		},
	} {
		t.Run(testCase.value, func(t *testing.T) {
			parsed, err := Parse(testCase.value)
			if err != nil {
				t.Fatalf("parse %s: %v", testCase.value, err)
			}
			if len(parsed) != 1 {
				t.Fatalf("expected one substitution, got %d", len(parsed))
			}
			got := parsed[0]
			if got.Kind != testCase.kind || got.Argument != testCase.argument ||
				got.Field != testCase.field || got.Part != testCase.part {
				t.Fatalf("parsed = %+v", got)
			}
		})
	}
}

// TestSubstitutionResolutionFailsClosed holds the resolver's central promise: a
// reference it cannot satisfy is an error, never an empty string.
func TestSubstitutionResolutionFailsClosed(t *testing.T) {
	context := Context{
		Secrets: map[string]string{
			"db-password": "s3cret",
			SecretReference("db-password", SecretPartBcryptHash): "$2y$hash",
		},
		Discovered:     map[string]string{"catalog": "https://catalog:8443/"},
		VolumePaths:    map[string]string{"data": "/var/data"},
		EndpointValues: map[string]string{"api.desired-port": "8080"},
		Conf:           map[string]any{"server": map[string]any{"port": 8080}},
		CertsMountPath: "/etc/certs",
	}
	for _, testCase := range []struct{ value, want string }{
		{value: `{{secret("db-password")}}`, want: "s3cret"},
		{value: `{{secret("db-password", part: bcrypt_hash)}}`, want: "$2y$hash"},
		{value: `{{discovered.catalog}}`, want: "https://catalog:8443/"},
		{value: `{{volumes.data.PathOnDisk}}`, want: "/var/data"},
		{value: `{{endpoints.definitions.api.desired-port}}`, want: "8080"},
		{value: `{{conf.server.port}}`, want: "8080"},
		{value: `{{ssl.pem_path}}`, want: "/etc/certs/key.pem"},
		{value: `{{ssl.ca_path}}`, want: "/etc/certs/ca.cer"},
		{value: `{{ssl.cert_path}}`, want: "/etc/certs/cert.cer"},
	} {
		got, err := Resolve(testCase.value, context)
		if err != nil {
			t.Fatalf("resolve %s: %v", testCase.value, err)
		}
		if got != testCase.want {
			t.Fatalf("resolve %s = %q, want %q", testCase.value, got, testCase.want)
		}
	}

	for _, value := range []string{
		`{{secret("absent")}}`,
		`{{secret("db-password")}} and {{secret("absent")}}`,
		`{{discovered.absent}}`,
		`{{conf.server.absent}}`,
		`{{volumes.absent.PathOnDisk}}`,
	} {
		if _, err := Resolve(value, context); err == nil {
			t.Fatalf("resolving %s must fail rather than render empty", value)
		}
	}

	// A secret referenced plainly must not be satisfied by its bcrypt hash.
	partOnly := Context{Secrets: map[string]string{
		SecretReference("shared", SecretPartBcryptHash): "$2y$hash",
	}}
	if _, err := Resolve(`{{secret("shared")}}`, partOnly); err == nil {
		t.Fatal("a part-keyed secret must not satisfy a plain reference")
	}
}
