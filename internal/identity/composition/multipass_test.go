package composition

import (
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/multipassoffline"
)

func environment(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

// The unconfigured case is the normal one and is not an error. A deployment
// that says nothing about Multipass gains nothing by upgrading.
func TestFederationIsOffUnlessConfigured(t *testing.T) {
	t.Parallel()
	directory, err := Configure(environment(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if directory != nil {
		t.Fatalf("an unconfigured Hub built a directory: %#v", directory)
	}
}

func TestConfigureSelectsBetweenTheTwoAdapters(t *testing.T) {
	t.Parallel()
	real, err := Configure(environment(map[string]string{
		MultipassBaseURLVariable: "https://multipass.example.test",
		MultipassTokenVariable:   "secret-token",
		MultipassTimeoutVariable: "5s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if real == nil {
		t.Fatal("a configured base URL built no directory")
	}
	if _, offline := real.(*multipassoffline.Directory); offline {
		t.Fatal("a configured base URL built the offline fixture")
	}
	substitute, err := Configure(environment(map[string]string{MultipassOfflineVariable: "true"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, offline := substitute.(*multipassoffline.Directory); !offline {
		t.Fatalf("the offline opt-in built %#v", substitute)
	}
}

// Refused rather than resolved by precedence: a Hub that quietly picked one
// would federate against the loser's directory exactly never.
func TestTwoIdentitySourcesAreRefused(t *testing.T) {
	t.Parallel()
	_, err := Configure(environment(map[string]string{
		MultipassBaseURLVariable: "https://multipass.example.test",
		MultipassOfflineVariable: "true",
	}))
	if err == nil {
		t.Fatal("two identity sources were accepted")
	}
	if !strings.Contains(err.Error(), MultipassBaseURLVariable) ||
		!strings.Contains(err.Error(), MultipassOfflineVariable) {
		t.Fatalf("the error names neither variable: %v", err)
	}
}

func TestMisconfigurationIsAnErrorRatherThanASilentFallback(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name   string
		values map[string]string
	}{
		{name: "a token with no base URL", values: map[string]string{MultipassTokenVariable: "secret-token"}},
		{name: "a timeout with no base URL", values: map[string]string{MultipassTimeoutVariable: "5s"}},
		{
			name: "an unparseable timeout",
			values: map[string]string{
				MultipassBaseURLVariable: "https://multipass.example.test", MultipassTimeoutVariable: "soon",
			},
		},
		{
			name: "a non-positive timeout",
			values: map[string]string{
				MultipassBaseURLVariable: "https://multipass.example.test", MultipassTimeoutVariable: "0s",
			},
		},
		{
			name:   "a base URL that is not http or https",
			values: map[string]string{MultipassBaseURLVariable: "ftp://multipass.example.test"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			directory, err := Configure(environment(testCase.values))
			if err == nil {
				t.Fatalf("accepted, building %#v", directory)
			}
			if directory != nil {
				t.Fatalf("a refused configuration still returned a directory: %#v", directory)
			}
		})
	}
}

// The offline opt-in is an exact "true" once trimmed, matching the
// notification transport's own opt-in. Anything else leaves federation off, so
// a value nobody meant as an affirmative cannot quietly install a fixture
// directory in a real deployment.
func TestTheOfflineOptInIsExact(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"TRUE", "1", "yes", "on", "false", ""} {
		directory, err := Configure(environment(map[string]string{MultipassOfflineVariable: value}))
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		if directory != nil {
			t.Fatalf("%q turned the offline fixture on", value)
		}
	}
	// Surrounding whitespace is trimmed, because a value pasted into a
	// deployment manifest routinely carries some.
	directory, err := Configure(environment(map[string]string{MultipassOfflineVariable: " true "}))
	if err != nil {
		t.Fatal(err)
	}
	if directory == nil {
		t.Fatal("a padded \"true\" left federation off")
	}
}

// The bootstrap administrator list reaches the federation policy from the
// control plane rather than from a second environment read. Two copies of "who
// is an administrator" is the defect ResourceDecision's own comment records.
func TestFederationPolicyReservesTheBootstrapAdministrators(t *testing.T) {
	t.Parallel()
	policy, err := FederationPolicy(
		environment(map[string]string{
			MultipassUsernamePrefixVariable: " multipass: ",
			MultipassGroupPrefixVariable:    "multipass:",
		}),
		[]string{"root-admin", "   ", "second-admin"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if policy.UsernamePrefix != "multipass:" || policy.GroupPrefix != "multipass:" {
		t.Fatalf("policy = %#v", policy)
	}
	if strings.Join(policy.ReservedUsernames, ",") != "root-admin,second-admin" {
		t.Fatalf("reserved = %v; blanks must be dropped and the rest kept", policy.ReservedUsernames)
	}
}

func TestConfigureRefusesAMissingEnvironmentReader(t *testing.T) {
	t.Parallel()
	if _, err := Configure(nil); err == nil {
		t.Error("a nil environment reader was accepted")
	}
	if _, err := FederationPolicy(nil, nil); err == nil {
		t.Error("a nil environment reader was accepted for the policy")
	}
}
