package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	metricsapplication "github.com/jonathanagustin/conduit/internal/observability/application"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestPostgresConfigureReconciliationPrimesOrdinaryPlanLifecycle(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required for the PostgreSQL integration gate")
	}
	ctx := t.Context()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	base, err := controlplane.NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	dsnFile := writeFile(t, "postgres-dsn", []byte(dsn+"\n"))
	configuration := map[string]string{
		"REPOSITORY_KIND": "postgres", "POSTGRES_DSN_FILE": dsnFile,
	}
	repository, closeReconciliation, supervisor, _, err := configureReconciliation(ctx,
		func(name string) string { return configuration[name] }, base)
	if err != nil {
		t.Fatal(err)
	}
	defer closeReconciliation()
	if supervisor == nil || repository == base {
		t.Fatalf("production reconciliation composition = repository %T, supervisor %v", repository, supervisor)
	}
	if _, err := database.ExecContext(ctx, `
TRUNCATE conduit_repository_blobs;
UPDATE conduit_repository_state SET generation = 0, state = '{}'::jsonb WHERE singleton = TRUE;
TRUNCATE conduit_reconciliation_desired, conduit_reconciliation_observations,
    conduit_reconciliation_work, conduit_reconciliation_decisions,
    conduit_reconciliation_audit, conduit_reconciliation_proposal_outbox,
    conduit_reconciliation_plan_receipts`); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join("..", "..", "examples", "example-foundation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state controlplane.DesiredState
	if err := json.Unmarshal(contents, &state); err != nil {
		t.Fatal(err)
	}
	record, err := controlplane.NewLifecycle(repository).CreatePlan(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Prime(ctx); err != nil {
		t.Fatal(err)
	}
	plans, err := repository.ListPlans(ctx, state.Environment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].ID != record.ID {
		t.Fatalf("ordinary Plan lifecycle produced %#v, want one %s", plans, record.ID)
	}
	var receipts, outstanding int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM conduit_reconciliation_plan_receipts").Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM conduit_reconciliation_proposal_outbox WHERE exhausted = false").Scan(&outstanding); err != nil {
		t.Fatal(err)
	}
	if receipts != len(record.Plan.Steps) || outstanding != 0 {
		t.Fatalf("receipts=%d steps=%d outstanding=%d", receipts, len(record.Plan.Steps), outstanding)
	}
}

func writeFile(t *testing.T, name string, contents []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func pemPKCS8(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// The Bundle signing key is what makes a Bundle attributable. Every way of
// getting it wrong here ends with the Hub either signing with something it
// should not, or refusing to start.
func TestLoadBundleSigningKey(t *testing.T) {
	t.Parallel()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	valid := pemPKCS8(t, private)

	t.Run("no path leaves signing unconfigured rather than failing", func(t *testing.T) {
		t.Parallel()
		for _, path := range []string{"", "   "} {
			key, err := loadBundleSigningKey(path)
			if err != nil || key != nil {
				t.Errorf("%q gave (%v, %v), want (nil, nil)", path, key, err)
			}
		}
	})

	t.Run("loads an Ed25519 PKCS#8 key", func(t *testing.T) {
		t.Parallel()
		key, err := loadBundleSigningKey(writeFile(t, "key.pem", valid))
		if err != nil {
			t.Fatal(err)
		}
		if !key.Equal(private) {
			t.Error("loaded key does not match the file")
		}
	})

	t.Run("refuses anything that is not exactly one Ed25519 key", func(t *testing.T) {
		t.Parallel()
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		for name, contents := range map[string][]byte{
			// A second block is how an extra key gets smuggled past a reader
			// that stops after the first one.
			"a second PEM block": append(append([]byte{}, valid...), valid...),
			"trailing garbage":   append(append([]byte{}, valid...), []byte("not pem\n")...),
			"not PEM at all":     []byte("just some bytes\n"),
			"an RSA key":         pemPKCS8(t, rsaKey),
			"empty":              {},
		} {
			if _, err := loadBundleSigningKey(writeFile(t, "key.pem", contents)); err == nil {
				t.Errorf("%s was accepted as a signing key", name)
			}
		}
	})

	t.Run("reports an unreadable file", func(t *testing.T) {
		t.Parallel()
		if _, err := loadBundleSigningKey(filepath.Join(t.TempDir(), "absent")); err == nil {
			t.Error("a missing key file was accepted")
		}
	})
}

// These are the keys whose signatures the Hub will trust. A key that decodes
// to the wrong length is not an Ed25519 key, and treating it as one means
// verification that cannot succeed rather than one that fails loudly.
func TestLoadTrustedBundleKeys(t *testing.T) {
	t.Parallel()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("no path leaves the trust map unconfigured", func(t *testing.T) {
		t.Parallel()
		keys, err := loadTrustedBundleKeys("  ")
		if err != nil || keys != nil {
			t.Errorf("got (%v, %v), want (nil, nil)", keys, err)
		}
	})

	t.Run("accepts base64 with and without padding", func(t *testing.T) {
		t.Parallel()
		// Ed25519 public keys are 32 bytes, so standard base64 always pads.
		// Both spellings appear in the wild and both must load.
		for name, encoded := range map[string]string{
			"padded":   base64.StdEncoding.EncodeToString(public),
			"unpadded": base64.RawStdEncoding.EncodeToString(public),
		} {
			path := writeFile(t, "keys.json", []byte(`{"signer-1":"`+encoded+`"}`))
			keys, err := loadTrustedBundleKeys(path)
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			if !keys["signer-1"].Equal(public) {
				t.Errorf("%s: key did not round-trip", name)
			}
		}
	})

	t.Run("names the key it refuses", func(t *testing.T) {
		t.Parallel()
		for name, document := range map[string]string{
			"not base64":  `{"broken-key":"!!!!"}`,
			"wrong size":  `{"broken-key":"` + base64.StdEncoding.EncodeToString(public[:16]) + `"}`,
			"too long":    `{"broken-key":"` + base64.StdEncoding.EncodeToString(append(append([]byte{}, public...), 0)) + `"}`,
			"empty value": `{"broken-key":""}`,
		} {
			_, err := loadTrustedBundleKeys(writeFile(t, "keys.json", []byte(document)))
			if err == nil {
				t.Errorf("%s was accepted", name)
				continue
			}
			// With several keys configured, an error that does not say which
			// one is wrong leaves an operator diffing base64 by eye.
			if !strings.Contains(err.Error(), "broken-key") {
				t.Errorf("%s: error %q does not name the key", name, err)
			}
		}
	})

	t.Run("refuses a document that is not a string map", func(t *testing.T) {
		t.Parallel()
		for _, document := range []string{`[]`, `{"k":1}`, `not json`} {
			if _, err := loadTrustedBundleKeys(writeFile(t, "keys.json", []byte(document))); err == nil {
				t.Errorf("%q was accepted", document)
			}
		}
	})
}

func TestSplitCommaSeparated(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		given string
		want  []string
	}{
		"empty":              {"", []string{}},
		"only separators":    {" , ,, ", []string{}},
		"trims surroundings": {" a , b ", []string{"a", "b"}},
		// Duplicates are dropped but the first occurrence keeps its position,
		// so the value reads the same way it was written.
		"deduplicates in order": {"b,a,b,c,a", []string{"b", "a", "c"}},
	} {
		got := splitCommaSeparated(test.given)
		if len(got) != len(test.want) {
			t.Errorf("%s: %q gave %#v, want %#v", name, test.given, got, test.want)
			continue
		}
		for i := range got {
			if got[i] != test.want[i] {
				t.Errorf("%s: %q gave %#v, want %#v", name, test.given, got, test.want)
				break
			}
		}
	}
}

func TestRepositoryConfigurationFailsClosed(t *testing.T) {
	t.Parallel()
	getenv := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	if _, closeRepository, err := openRepository(context.Background(), getenv(map[string]string{
		"REPOSITORY_KIND": "unknown",
	})); err == nil {
		closeRepository()
		t.Fatal("unknown repository kind was accepted")
	}
	if _, closeRepository, err := openRepository(context.Background(), getenv(map[string]string{
		"REPOSITORY_KIND": "postgres",
	})); err == nil {
		closeRepository()
		t.Fatal("PostgreSQL repository without a mounted DSN was accepted")
	}
	fileRoot := filepath.Join(t.TempDir(), "state")
	repository, closeRepository, err := openRepository(context.Background(), getenv(map[string]string{
		"REPOSITORY_KIND": "file", "DATA_DIRECTORY": fileRoot,
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRepository()
	if _, ok := repository.(*controlplane.FileRepository); !ok {
		t.Fatalf("file mode returned %T", repository)
	}
}

func TestReadSingleLineSecretFile(t *testing.T) {
	t.Parallel()
	valid := writeFile(t, "dsn", []byte("postgres://conduit@example.test/conduit\n"))
	if value, err := readSingleLineSecretFile(valid, "POSTGRES_DSN_FILE"); err != nil || value != "postgres://conduit@example.test/conduit" {
		t.Fatalf("valid DSN = %q, %v", value, err)
	}
	for name, contents := range map[string][]byte{
		"empty":     {},
		"multiline": []byte("first\nsecond\n"),
		"nul":       []byte("first\x00second"),
		"oversized": bytes.Repeat([]byte("x"), 64*1024+1),
	} {
		if _, err := readSingleLineSecretFile(writeFile(t, name, contents), "POSTGRES_DSN_FILE"); err == nil {
			t.Errorf("%s secret file was accepted", name)
		}
	}
}

func TestLoadOIDCOperatorIdentityResolverProfiles(t *testing.T) {
	t.Parallel()
	getenv := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	if resolver, err := loadOIDCOperatorIdentityResolver(context.Background(), getenv(nil)); err != nil || resolver != nil {
		t.Fatalf("unset OIDC gave (%v, %v), want (nil, nil)", resolver, err)
	}
	if _, err := loadOIDCOperatorIdentityResolver(context.Background(), getenv(map[string]string{
		"OPERATOR_OIDC_CLIENT_ID": "configured-without-issuer",
	})); err == nil || !strings.Contains(err.Error(), "OPERATOR_OIDC_ISSUER") {
		t.Fatalf("partial OIDC configuration was not rejected: %v", err)
	}
	for _, profile := range []string{"multipass", "cognito-access", "generic"} {
		resolver, err := loadOIDCOperatorIdentityResolver(context.Background(), getenv(map[string]string{
			"OPERATOR_OIDC_PROFILE":   profile,
			"OPERATOR_OIDC_ISSUER":    "https://identity.example.test",
			"OPERATOR_OIDC_JWKS_URL":  "https://identity.example.test/jwks.json",
			"OPERATOR_OIDC_CLIENT_ID": "conduit",
		}))
		if err != nil || resolver == nil {
			t.Errorf("%s profile gave (%v, %v), want a resolver", profile, resolver, err)
		}
	}
	if _, err := loadOIDCOperatorIdentityResolver(context.Background(), getenv(map[string]string{
		"OPERATOR_OIDC_PROFILE": "unknown", "OPERATOR_OIDC_ISSUER": "https://identity.example.test",
		"OPERATOR_OIDC_JWKS_URL": "https://identity.example.test/jwks.json", "OPERATOR_OIDC_CLIENT_ID": "conduit",
	})); err == nil {
		t.Fatal("unknown OIDC profile was accepted")
	}
}

// A timeout configured for a scanner that was never configured is a typo with
// no other symptom: the Hub would start and simply never scan.
func TestLoadArtifactScannerRefusesATimeoutWithNoScanner(t *testing.T) {
	t.Parallel()

	scanner, err := loadArtifactScanner("", "")
	if err != nil || scanner != nil {
		t.Errorf("unset gave (%v, %v), want (nil, nil)", scanner, err)
	}
	if _, err := loadArtifactScanner("", "30s"); err == nil {
		t.Error("a timeout with no scanner command was accepted")
	} else if !strings.Contains(err.Error(), "EXTERNAL_ARTIFACT_SCANNER") {
		t.Errorf("error %q does not name the variable to set", err)
	}
	if _, err := loadArtifactScanner("/bin/true", "not-a-duration"); err == nil {
		t.Error("an unparseable timeout was accepted")
	}
}

// Metrics must add no reachable surface until an operator asks for one. The
// Hub's own port carries authenticated operator and Agent traffic; a scrape
// endpoint is neither, so it gets its own listener or none.
func TestMetricsListenerIsOffUnlessAnAddressIsSet(t *testing.T) {
	reader := func(context.Context) (metricsapplication.ReconciliationStats, error) {
		return metricsapplication.ReconciliationStats{}, nil
	}
	if stop := startMetricsListener(t.Context(), "", reader); stop != nil {
		stop()
		t.Error("an empty METRICS_ADDRESS started a listener")
	}
	if stop := startMetricsListener(t.Context(), "   ", reader); stop != nil {
		stop()
		t.Error("a blank METRICS_ADDRESS started a listener")
	}
}

// The file repository has none of the queues these metrics describe. Exporting
// zeroes for it would tell a scraper the fleet is idle, which is a worse answer
// than no answer.
func TestMetricsListenerDeclinesWithoutQueueStatistics(t *testing.T) {
	if stop := startMetricsListener(t.Context(), "127.0.0.1:0", nil); stop != nil {
		stop()
		t.Error("a listener started with no stats reader; it would have exported nothing but zeroes")
	}
}

// And when it is asked for, it serves, and it serves the lag signal the whole
// endpoint exists for.
func TestMetricsListenerServesWhenConfigured(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	// Freed immediately so the listener under test can take the port; the
	// alternative is a hardcoded port that collides on a busy machine.
	_ = listener.Close()

	stop := startMetricsListener(t.Context(), address, func(context.Context) (metricsapplication.ReconciliationStats, error) {
		return metricsapplication.ReconciliationStats{WorkPending: 4, OldestPendingAge: 42 * time.Second}, nil
	})
	if stop == nil {
		t.Fatal("no listener started")
	}
	defer stop()

	var body string
	for attempt := 0; attempt < 50; attempt++ {
		response, err := http.Get("http://" + address + "/metrics")
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		payload, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		body = string(payload)
		break
	}
	if body == "" {
		t.Fatal("the metrics listener never answered")
	}
	for _, required := range []string{
		`conduit_reconciliation_pending{queue="reconciliation"} 4`,
		`conduit_reconciliation_oldest_pending_seconds{queue="reconciliation"} 42`,
	} {
		if !strings.Contains(body, required) {
			t.Errorf("missing %q in:\n%s", required, body)
		}
	}
}
