package gitless_test

// The acceptance harness: one real Gitless GitOps reconcile, with nothing faked.
//
// Every other test in this context substitutes something. The domain tests hand
// Admit a constructed attestation, the adapter tests use httptest servers and a
// stub cosign, and the equivalence test drives both convergers against doubles.
// All of that is worth having and none of it answers the question this file
// asks: does the whole thing work against a registry, a signature, a policy and
// a cluster that do not know they are being tested?
//
// So this runs the real path:
//
//	ociresolve      → a real OCI registry, real anonymous bearer flow
//	cosignverify    → a real cosign binary against a real keyless signature
//	domain.Admit    → the real admission decision
//	resolutionlog   → a real fsynced append
//	fluxconverge    → real manifests applied to a real Flux API server
//
// The artifact is ghcr.io/fluxcd/source-controller. It is used because it is
// public, keyless-signed by a GitHub Actions workflow, and not ours -- so the
// publisher policy below is matching a real certificate issued to somebody else,
// which is exactly the situation a policy exists for. Conduit's own artifacts
// are private, so a test that used them could only run where the credential is.
//
// It is skipped unless GITLESS_ACCEPTANCE=1, because it needs a cluster, a
// network and a binary that most environments running these tests do not have.
// scripts/test-gitless-acceptance.sh sets it.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/configpolicy"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/cosignverify"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/fluxconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/ociresolve"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/resolutionlog"
	"github.com/jonathanagustin/conduit/internal/gitless/application"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

const (
	acceptanceRepository = "ghcr.io/fluxcd/source-controller"
	acceptanceChannel    = "v1.7.1"
)

// The real publisher of the artifact above, as cosign reports it. Written out
// rather than discovered, because a policy derived from the thing it is meant
// to constrain would accept anything.
const (
	realIssuer   = `^https://token\.actions\.githubusercontent\.com$`
	realIdentity = `^https://github\.com/fluxcd/gha-workflows/\.github/workflows/controller-release\.yaml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$`
	// Conduit's own publisher. The same artifact must be refused under it.
	conduitIdentity = `^https://github\.com/jonathanagustin/conduit/\.github/workflows/platform-images\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$`
)

func requireAcceptance(t *testing.T) {
	t.Helper()
	if os.Getenv("GITLESS_ACCEPTANCE") != "1" {
		t.Skip("set GITLESS_ACCEPTANCE=1 to run the end-to-end harness")
	}
	if _, err := exec.LookPath("cosign"); err != nil {
		t.Skip("cosign is not installed")
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl is not installed")
	}
	// A cluster that is not answering is not a failing harness -- it is an
	// absent one. This sandbox's kubelet dies under the filesystem's reserved
	// blocks, and a red test that means "the machine went away" trains people
	// to ignore it.
	if output, err := exec.Command("kubectl", "get", "--raw", "/readyz").CombinedOutput(); err != nil {
		t.Skipf("no reachable cluster: %s", strings.TrimSpace(string(output)))
	}
	if _, err := exec.Command("kubectl", "get", "crd", "ocirepositories.source.toolkit.fluxcd.io").Output(); err != nil {
		t.Skip("the reachable cluster has no Flux CRDs")
	}
}

// kubectlStore is the ManifestStore that puts rendered manifests into a real
// cluster. It exists only here: production publishes them as an OCI artifact
// for a cluster to pull, and applying them directly is a test affordance, not a
// deployment model.
type kubectlStore struct {
	t         *testing.T
	directory string
	namespace string
	applied   string
}

func (store *kubectlStore) Put(ctx context.Context, environment string, manifest []byte) error {
	path := filepath.Join(store.directory, environment+".yaml")
	if err := os.WriteFile(path, manifest, 0o600); err != nil {
		return err
	}
	store.applied = path
	output, err := exec.CommandContext(ctx, "kubectl", "apply", "--server-side", "--field-manager=conduit-gitless", "-f", path).CombinedOutput()
	store.t.Logf("kubectl apply: %s", strings.TrimSpace(string(output)))
	return err
}

func (store *kubectlStore) CurrentDigest(ctx context.Context, environment string) (string, error) {
	output, err := exec.CommandContext(ctx, "kubectl", "get", "ocirepository",
		"conduit-"+environment, "-n", store.namespace, "-o", "jsonpath={.spec.ref.digest}").Output()
	if err != nil {
		// Absent is not an error: an environment that has never converged has
		// no digest, which is what the empty string means.
		return "", nil
	}
	return strings.TrimSpace(string(output)), nil
}

func writePolicy(t *testing.T, identity string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policies.json")
	body, err := json.Marshal(map[string]any{
		"environments": map[string]any{
			"acceptance": map[string]string{
				"certificateIssuer":   realIssuer,
				"certificateIdentity": identity,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func buildSource(t *testing.T, identity string, store *kubectlStore) *application.Source {
	t.Helper()
	policies, err := configpolicy.Load(writePolicy(t, identity))
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}
	log, err := resolutionlog.Open(filepath.Join(t.TempDir(), "resolutions.jsonl"))
	if err != nil {
		t.Fatalf("open resolution log: %v", err)
	}
	converger, err := fluxconverge.New(store, fluxconverge.Options{
		// The manifest declares the namespace it belongs to, so the apply below
		// does not have to be told separately. Two sources for one fact is how
		// the first run of this harness failed.
		Namespace:          store.namespace,
		ServiceAccountName: "platform-runtime-reconciler",
		PullSecretName:     "conduit-runtime-pull",
	})
	if err != nil {
		t.Fatalf("build converger: %v", err)
	}
	source, err := application.New(
		ociresolve.New(nil, ociresolve.Anonymous{}),
		cosignverify.New(""),
		policies,
		log,
		converger,
	)
	if err != nil {
		t.Fatalf("build source: %v", err)
	}
	return source
}

func selector() domain.Selector {
	return domain.Selector{Kind: domain.ByChannel, Repository: acceptanceRepository, Channel: acceptanceChannel}
}

// Each test gets its own namespace. The first version of this harness shared
// one and deleted it asynchronously, so a later test found an earlier test's
// objects and reported a refusal as having reached the cluster -- a failure in
// the harness that read exactly like a failure in the product.
func newStore(t *testing.T) *kubectlStore {
	t.Helper()
	name := "gitless-" + strings.ToLower(strings.ReplaceAll(t.Name(), "_", "-"))
	if len(name) > 60 {
		name = name[:60]
	}
	name = strings.Trim(name, "-")
	if output, err := exec.Command("kubectl", "create", "namespace", name).CombinedOutput(); err != nil {
		t.Fatalf("create namespace %s: %s", name, output)
	}
	t.Cleanup(func() {
		_ = exec.Command("kubectl", "delete", "namespace", name, "--now", "--wait=false").Run()
	})
	return &kubectlStore{t: t, directory: t.TempDir(), namespace: name}
}

// The whole loop, admitted.
func TestAcceptanceARealSignedChannelReachesARealCluster(t *testing.T) {
	requireAcceptance(t)
	store := newStore(t)
	source := buildSource(t, realIdentity, store)

	outcome, err := source.Reconcile(t.Context(), "acceptance", selector())
	if err != nil {
		t.Fatalf("the real path refused a correctly signed artifact: %v", err)
	}
	pin := outcome.Admission.Pin()
	if err := pin.Validate(); err != nil {
		t.Fatalf("admitted an invalid pin %s: %v", pin, err)
	}
	issuer, identity := outcome.Admission.Publisher()
	t.Logf("admitted %s\n  signed by %s\n  issued by %s", pin, identity, issuer)
	if !strings.Contains(identity, "fluxcd/gha-workflows") {
		t.Errorf("the admission recorded an unexpected publisher: %q", identity)
	}

	// The cluster must now hold an OCIRepository pinned to exactly the digest
	// that was admitted -- not the channel that was asked for.
	deployed, err := store.CurrentDigest(t.Context(), "acceptance")
	if err != nil {
		t.Fatalf("read back the deployed digest: %v", err)
	}
	if deployed != pin.Digest {
		t.Fatalf("the cluster converged on %q, not the admitted %q", deployed, pin.Digest)
	}

	// And it must carry a tag nowhere, or a moved channel could change what the
	// cluster runs without anything being admitted.
	rendered, err := os.ReadFile(store.applied)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"tag:", "semver:"} {
		if strings.Contains(string(rendered), forbidden) {
			t.Errorf("the applied manifest carries a mutable %s selector", forbidden)
		}
	}
}

// The same artifact, the same signature, a different policy. Nothing may reach
// the cluster.
//
// This is the case that makes the harness worth running: it proves the refusal
// survives the whole real path, not just the domain unit test. A verifier that
// quietly said yes, or a converger that ran before admission, would show up
// here and nowhere else.
func TestAcceptanceARealSignatureIsRefusedUnderAForeignPolicy(t *testing.T) {
	requireAcceptance(t)
	store := newStore(t)
	source := buildSource(t, conduitIdentity, store)

	_, err := source.Reconcile(t.Context(), "acceptance", selector())
	if err == nil {
		t.Fatal("an artifact signed by a foreign publisher was deployed")
	}
	var refused *application.RefusedError
	if !strings.Contains(err.Error(), "UNTRUSTED_IDENTITY") {
		if ok := asRefused(err, &refused); !ok {
			t.Fatalf("expected a policy refusal, got %v", err)
		}
	}
	t.Logf("refused as expected: %v", err)

	if store.applied != "" {
		t.Fatal("a refused source produced a manifest")
	}
	// Nothing may exist in the cluster either -- a refusal that still left an
	// object behind would be a refusal in name only.
	output, _ := exec.Command("kubectl", "get", "ocirepository", "-n", store.namespace, "-o", "name").CombinedOutput()
	if strings.Contains(string(output), "ocirepository") {
		t.Fatalf("a refused source reached the cluster: %s", output)
	}
}

func asRefused(err error, target **application.RefusedError) bool {
	refused, ok := err.(*application.RefusedError)
	if ok {
		*target = refused
	}
	return ok
}

// Reconciling again must not redeploy. Gitless GitOps restates desired state
// continuously, and an approach that redeployed every interval would turn a
// schedule into a deployment storm.
func TestAcceptanceReconcilingTwiceDeploysOnce(t *testing.T) {
	requireAcceptance(t)
	store := newStore(t)
	source := buildSource(t, realIdentity, store)

	if _, err := source.Reconcile(t.Context(), "acceptance", selector()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	store.applied = ""
	outcome, err := source.Reconcile(t.Context(), "acceptance", selector())
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if outcome.Moved {
		t.Error("an unchanged channel reported a move")
	}
	if store.applied != "" {
		t.Fatal("an unchanged channel was applied to the cluster again")
	}
}
