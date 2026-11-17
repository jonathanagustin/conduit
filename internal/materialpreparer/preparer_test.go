package materialpreparer_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/materialpreparer"
	"github.com/jonathanagustin/conduit/internal/materialpreparer/kubetest"
)

const (
	sourceNamespace = "environment-system"
	managed         = "example"

	// Distinctive, so that finding either anywhere it should not be is
	// unambiguous.
	databasePassword = "s3cr3t-database-password-7f3a9c"
	rotatedPassword  = "s3cr3t-rotated-password-b81d2e"
	registryConfig   = `{"auths":{"registry.example":{"auth":"cmVnaXN0cnktdG9rZW4tNGU5YQ=="}}}`
)

// documentedRules is the RBAC docs/operations/material-preparer.md tells an
// operator to grant, restated here so that the preparer is tested under it
// and nothing wider: read-only, by name, in the source namespace; the four
// verbs and no list in a managed namespace.
func documentedRules() []kubetest.Rule {
	return []kubetest.Rule{
		{Namespace: sourceNamespace, Verbs: []string{"get"}, ResourceNames: []string{"database-source", "database-source-v8", "registry-source", "tls-source"}},
		{Namespace: managed, Verbs: []string{"get", "create", "update", "delete"}},
	}
}

type harness struct {
	t       *testing.T
	api     *kubetest.Server
	secrets materialpreparer.Secrets
	config  materialpreparer.Config
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	api := kubetest.New(t, sourceNamespace, documentedRules()...)
	api.Put(sourceNamespace, "database-source", kubetest.Object{Type: "Opaque", Data: map[string][]byte{
		"username": []byte("app"), "password": []byte(databasePassword), "unrequested": []byte("not-for-the-workload"),
	}})
	api.Put(sourceNamespace, "registry-source", kubetest.Object{
		Type: "kubernetes.io/dockerconfigjson", Data: map[string][]byte{".dockerconfigjson": []byte(registryConfig)},
	})
	secrets, err := materialpreparer.NewInClusterSecrets(api.Getenv(managed), api.Directory)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, api: api, secrets: secrets, config: materialpreparer.Config{
		SourceNamespace: sourceNamespace, ManagedNamespaces: []string{managed},
	}}
}

// run serves one request and returns the exit status and the diagnostic.
func (harness *harness) run(request string) (int, string) {
	harness.t.Helper()
	err := materialpreparer.Run(context.Background(), strings.NewReader(request), harness.config, harness.secrets)
	if err == nil {
		return materialpreparer.ExitOK, ""
	}
	var failure *materialpreparer.Failure
	if !errors.As(err, &failure) {
		harness.t.Fatalf("Run returned an error that names no exit status: %v", err)
	}
	return failure.Code, failure.Reason
}

func (harness *harness) expect(request string, want int) string {
	harness.t.Helper()
	code, reason := harness.run(request)
	if code != want {
		harness.t.Fatalf("exit status %d (%s), want %d\nrequest: %s", code, reason, want, request)
	}
	return reason
}

func access(assignment string, revision int, reference, name string, keys ...string) string {
	quoted := make([]string, len(keys))
	for index, key := range keys {
		quoted[index] = fmt.Sprintf("%q", key)
	}
	return fmt.Sprintf(`{"requirement":"r","assignment":%q,"revision":%d,"providerRef":%q,"kubernetesSecretName":%q,"keys":[%s]}`,
		assignment, revision, reference, name, strings.Join(quoted, ","))
}

func mutation(kind, action, namespace, current, previous string) string {
	body := fmt.Sprintf(`{"position":1,"action":%q,"namespace":%q`, action, namespace)
	if current != "" {
		body += `,"current":` + current
	}
	if previous != "" {
		body += `,"previous":` + previous
	}
	return fmt.Sprintf(`{"apiVersion":"platform.delivery/v1alpha1","kind":%q,"namespace":%q,"access":%s}}`, kind, namespace, body)
}

var databaseV7 = access("database", 7, "kubernetes-secret://database-source", "database-credentials", "username", "password")

func TestCreateProjectsOnlyTheRequestedKeysAndIsIdempotent(t *testing.T) {
	harness := newHarness(t)
	create := mutation("SecretMutation", "CREATE", managed, databaseV7, "")
	harness.expect(create, materialpreparer.ExitOK)

	written, found := harness.api.Get(managed, "database-credentials")
	if !found {
		t.Fatal("the target Secret was not created")
	}
	want := kubetest.Object{
		Type:   "Opaque",
		Labels: map[string]string{"app.kubernetes.io/managed-by": "platform-material-preparer"},
		Annotations: map[string]string{
			"platform.delivery/material-assignment": "database", "platform.delivery/material-revision": "7",
			"platform.delivery/material-source": "kubernetes-secret://database-source", "platform.delivery/material-kind": "secret",
		},
		Data:            map[string][]byte{"username": []byte("app"), "password": []byte(databasePassword)},
		ResourceVersion: written.ResourceVersion,
	}
	if !reflect.DeepEqual(written, want) {
		t.Fatalf("target Secret\n got %#v\nwant %#v", written, want)
	}

	// The same request again changes nothing a workload can see and succeeds.
	harness.expect(create, materialpreparer.ExitOK)
	again, _ := harness.api.Get(managed, "database-credentials")
	again.ResourceVersion = written.ResourceVersion
	if !reflect.DeepEqual(again, written) {
		t.Fatalf("a repeated CREATE changed the Secret: %#v", again)
	}
	// The source is read, never written, and nothing is ever listed.
	for _, request := range harness.api.Requests() {
		if request.Verb == "list" || (request.Namespace == sourceNamespace && request.Verb != "get") {
			t.Fatalf("unexpected API call: %s %s", request.Method, request.Path)
		}
		if request.Status == http.StatusForbidden {
			t.Fatalf("the documented RBAC refused %s %s", request.Method, request.Path)
		}
	}
}

func TestReplaceRotatesTheValueDropsStaleKeysAndRemovesARenamedSecret(t *testing.T) {
	harness := newHarness(t)
	harness.expect(mutation("SecretMutation", "CREATE", managed, databaseV7, ""), materialpreparer.ExitOK)

	// Revision 8 lives in its own source Secret, asks for one key fewer and
	// lands under a new Kubernetes name.
	harness.api.Put(sourceNamespace, "database-source-v8", kubetest.Object{Type: "Opaque", Data: map[string][]byte{"password": []byte(rotatedPassword)}})
	databaseV8 := access("database", 8, "kubernetes-secret://database-source-v8", "database-credentials-v8", "password")
	harness.expect(mutation("SecretMutation", "REPLACE", managed, databaseV8, databaseV7), materialpreparer.ExitOK)

	rotated, found := harness.api.Get(managed, "database-credentials-v8")
	if !found || !reflect.DeepEqual(rotated.Data, map[string][]byte{"password": []byte(rotatedPassword)}) ||
		rotated.Annotations["platform.delivery/material-revision"] != "8" {
		t.Fatalf("rotated Secret = %#v", rotated)
	}
	if _, stillThere := harness.api.Get(managed, "database-credentials"); stillThere {
		t.Fatal("the Secret under the previous name was left behind")
	}

	// A rotation in place replaces the data wholesale: a key no longer asked
	// for does not linger.
	inPlace := access("database", 9, "kubernetes-secret://database-source", "database-credentials-v8", "username")
	harness.expect(mutation("SecretMutation", "REPLACE", managed, inPlace, databaseV8), materialpreparer.ExitOK)
	replaced, _ := harness.api.Get(managed, "database-credentials-v8")
	if !reflect.DeepEqual(replaced.Data, map[string][]byte{"username": []byte("app")}) {
		t.Fatalf("an in-place rotation left stale keys: %v", keysOf(replaced.Data))
	}
}

func keysOf(data map[string][]byte) []string {
	keys := []string{}
	for key := range data {
		keys = append(keys, key)
	}
	return keys
}

func TestVerifyChangesNothingAndFailsWhenTheSecretIsNotWhatThePlanSays(t *testing.T) {
	harness := newHarness(t)
	verify := mutation("SecretMutation", "VERIFY", managed, databaseV7, databaseV7)
	harness.expect(verify, materialpreparer.ExitVerificationFailed)

	harness.expect(mutation("SecretMutation", "CREATE", managed, databaseV7, ""), materialpreparer.ExitOK)
	harness.api.Reset()
	harness.expect(verify, materialpreparer.ExitOK)
	for _, request := range harness.api.Requests() {
		if request.Verb != "get" || request.Namespace != managed {
			t.Fatalf("VERIFY issued %s %s; it must only read the target", request.Method, request.Path)
		}
	}
	// The Plan says revision 8; revision 7 is installed.
	harness.expect(mutation("SecretMutation", "VERIFY", managed,
		access("database", 8, "kubernetes-secret://database-source", "database-credentials", "username", "password"), ""),
		materialpreparer.ExitVerificationFailed)
	// The Plan expects a key the installed Secret does not have.
	harness.expect(mutation("SecretMutation", "VERIFY", managed,
		access("database", 7, "kubernetes-secret://database-source", "database-credentials", "username", "password", "port"), ""),
		materialpreparer.ExitVerificationFailed)
}

func TestDeleteRemovesOnlyWhatThePreparerOwnsAndAnAbsentSecretIsSuccess(t *testing.T) {
	harness := newHarness(t)
	remove := mutation("SecretMutation", "DELETE", managed, "", databaseV7)
	harness.expect(remove, materialpreparer.ExitOK) // already absent

	harness.expect(mutation("SecretMutation", "CREATE", managed, databaseV7, ""), materialpreparer.ExitOK)
	harness.expect(remove, materialpreparer.ExitOK)
	if _, found := harness.api.Get(managed, "database-credentials"); found {
		t.Fatal("DELETE left the Secret in place")
	}
}

// A Secret the preparer did not create is somebody else's: a Helm release
// record, a tenant's own Secret, another assignment's. It is neither
// overwritten nor deleted, whatever the request says.
func TestASecretThePreparerDoesNotOwnIsNeverOverwrittenOrDeleted(t *testing.T) {
	harness := newHarness(t)
	tenant := kubetest.Object{Type: "Opaque", Data: map[string][]byte{"password": []byte("the-tenants-own")}}
	harness.api.Put(managed, "database-credentials", tenant)
	another := kubetest.Object{
		Type: "Opaque", Data: map[string][]byte{"password": []byte("another-assignments")},
		Labels:      map[string]string{"app.kubernetes.io/managed-by": "platform-material-preparer"},
		Annotations: map[string]string{"platform.delivery/material-assignment": "cache", "platform.delivery/material-kind": "secret"},
	}
	harness.api.Put(managed, "cache-credentials", another)
	cacheNamed := access("database", 7, "kubernetes-secret://database-source", "cache-credentials", "password")

	for name, request := range map[string]string{
		"CREATE over a tenant's Secret":          mutation("SecretMutation", "CREATE", managed, databaseV7, ""),
		"DELETE of a tenant's Secret":            mutation("SecretMutation", "DELETE", managed, "", databaseV7),
		"VERIFY of a tenant's Secret":            mutation("SecretMutation", "VERIFY", managed, databaseV7, ""),
		"rollback of a CREATE it did not make":   mutation("SecretMutationRollback", "CREATE", managed, databaseV7, ""),
		"REPLACE over another assignment's":      mutation("SecretMutation", "REPLACE", managed, cacheNamed, cacheNamed),
		"DELETE of another assignment's":         mutation("SecretMutation", "DELETE", managed, "", cacheNamed),
		"a pull Secret over a tenant's Secret":   registryRequest(managed, "kubernetes-secret://registry-source", "database-credentials"),
		"a pull Secret over an app assignment's": registryRequest(managed, "kubernetes-secret://registry-source", "cache-credentials"),
	} {
		if code, reason := harness.run(request); code != materialpreparer.ExitRefused {
			t.Errorf("%s: exit status %d (%s), want refused", name, code, reason)
		}
	}
	for name, want := range map[string]kubetest.Object{"database-credentials": tenant, "cache-credentials": another} {
		got, found := harness.api.Get(managed, name)
		if !found || !reflect.DeepEqual(got.Data, want.Data) || !reflect.DeepEqual(got.Labels, want.Labels) {
			t.Errorf("%s was changed: %v", name, found)
		}
	}
}

func TestRollbackRestoresWhatTheMutationReplaced(t *testing.T) {
	harness := newHarness(t)
	harness.api.Put(sourceNamespace, "database-source-v8", kubetest.Object{Type: "Opaque", Data: map[string][]byte{"password": []byte(rotatedPassword)}})
	databaseV8 := access("database", 8, "kubernetes-secret://database-source-v8", "database-credentials", "password")

	// CREATE, rolled back: the Secret is gone.
	harness.expect(mutation("SecretMutation", "CREATE", managed, databaseV7, ""), materialpreparer.ExitOK)
	harness.expect(mutation("SecretMutationRollback", "CREATE", managed, databaseV7, ""), materialpreparer.ExitOK)
	if _, found := harness.api.Get(managed, "database-credentials"); found {
		t.Fatal("rolling back a CREATE left the Secret")
	}

	// REPLACE, rolled back: the previous revision's material is back.
	harness.expect(mutation("SecretMutation", "CREATE", managed, databaseV7, ""), materialpreparer.ExitOK)
	harness.expect(mutation("SecretMutation", "REPLACE", managed, databaseV8, databaseV7), materialpreparer.ExitOK)
	harness.expect(mutation("SecretMutationRollback", "REPLACE", managed, databaseV8, databaseV7), materialpreparer.ExitOK)
	restored, _ := harness.api.Get(managed, "database-credentials")
	if string(restored.Data["password"]) != databasePassword || restored.Annotations["platform.delivery/material-revision"] != "7" {
		t.Fatalf("rolling back a REPLACE did not restore revision 7: revision %q", restored.Annotations["platform.delivery/material-revision"])
	}

	// DELETE, rolled back: the Secret exists again.
	harness.expect(mutation("SecretMutation", "DELETE", managed, "", databaseV7), materialpreparer.ExitOK)
	harness.expect(mutation("SecretMutationRollback", "DELETE", managed, "", databaseV7), materialpreparer.ExitOK)
	if recreated, found := harness.api.Get(managed, "database-credentials"); !found || string(recreated.Data["password"]) != databasePassword {
		t.Fatal("rolling back a DELETE did not recreate the Secret")
	}
}

func registryRequest(namespace, reference, pullSecret string) string {
	return fmt.Sprintf(`{"apiVersion":"platform.delivery/v1alpha1","kind":"RegistryAccess","namespace":%q,"access":{"assignmentId":"primary","revision":4,`+
		`"sourceRegistry":"registry.example","runtimeRegistry":"registry.example","credentialRef":%q,"imagePullSecretName":%q}}`,
		namespace, reference, pullSecret)
}

func TestRegistryAccessCreatesAnImagePullSecretFromADockerConfigSource(t *testing.T) {
	harness := newHarness(t)
	request := registryRequest(managed, "kubernetes-secret://registry-source", "registry-pull")
	harness.expect(request, materialpreparer.ExitOK)
	harness.expect(request, materialpreparer.ExitOK) // idempotent

	pull, found := harness.api.Get(managed, "registry-pull")
	if !found || pull.Type != "kubernetes.io/dockerconfigjson" || string(pull.Data[".dockerconfigjson"]) != registryConfig || len(pull.Data) != 1 ||
		pull.Annotations["platform.delivery/material-kind"] != "registry" || pull.Annotations["platform.delivery/material-revision"] != "4" {
		t.Fatalf("pull Secret = %#v", pull.Annotations)
	}
	// A source that is not a registry credential is not turned into one.
	harness.expect(registryRequest(managed, "kubernetes-secret://database-source", "other-pull"), materialpreparer.ExitSourceUnusable)
	if _, found := harness.api.Get(managed, "other-pull"); found {
		t.Fatal("a pull Secret was created from a source that is not a registry credential")
	}
	// The Agent calls for an assignment carrying either field; one alone is
	// nothing to act on.
	harness.expect(registryRequest(managed, "kubernetes-secret://registry-source", ""), materialpreparer.ExitRefused)
	harness.expect(registryRequest(managed, "", "registry-pull"), materialpreparer.ExitRefused)
}

// A namespace outside the managed set, and a reference that could name
// anything other than a Secret in the source namespace, are refused before the
// Kubernetes API is asked anything.
func TestRequestsOutsideTheManagedNamespacesOrTheReferenceGrammarAreRefusedBeforeAnyAPICall(t *testing.T) {
	harness := newHarness(t)
	elsewhere := func(reference string) string {
		return access("database", 7, reference, "database-credentials", "password")
	}
	for name, request := range map[string]string{
		"an unmanaged namespace":            mutation("SecretMutation", "CREATE", "kube-system", databaseV7, ""),
		"the source namespace itself":       mutation("SecretMutation", "CREATE", sourceNamespace, databaseV7, ""),
		"an unmanaged namespace, DELETE":    mutation("SecretMutation", "DELETE", "kube-system", "", databaseV7),
		"an unmanaged namespace, rollback":  mutation("SecretMutationRollback", "CREATE", "kube-system", databaseV7, ""),
		"an unmanaged namespace, VERIFY":    mutation("SecretMutation", "VERIFY", "kube-system", databaseV7, ""),
		"an unmanaged namespace, registry":  registryRequest("kube-system", "kubernetes-secret://registry-source", "registry-pull"),
		"a namespace that is not a name":    mutation("SecretMutation", "CREATE", "example/../kube-system", databaseV7, ""),
		"a reference with a namespace":      mutation("SecretMutation", "CREATE", managed, elsewhere("kubernetes-secret://kube-system/admin-token"), ""),
		"a reference with a path traversal": mutation("SecretMutation", "CREATE", managed, elsewhere("kubernetes-secret://../../kube-system/secrets/admin"), ""),
		"a reference for another provider":  mutation("SecretMutation", "CREATE", managed, elsewhere("vault://secret/data/database"), ""),
		"a bare name":                       mutation("SecretMutation", "CREATE", managed, elsewhere("database-source"), ""),
		"a registry reference with a query": registryRequest(managed, "kubernetes-secret://registry-source?namespace=kube-system", "registry-pull"),
		"a target name that is not a name":  mutation("SecretMutation", "CREATE", managed, access("database", 7, "kubernetes-secret://database-source", "../kube-system/x", "password"), ""),
		"a key that is not a Secret key":    mutation("SecretMutation", "CREATE", managed, access("database", 7, "kubernetes-secret://database-source", "database-credentials", "pass/word"), ""),
		"an envelope and mutation that differ": `{"apiVersion":"platform.delivery/v1alpha1","kind":"SecretMutation","namespace":"example",` +
			`"access":{"action":"CREATE","namespace":"kube-system","current":` + databaseV7 + `}}`,
	} {
		code, reason := harness.run(request)
		if code != materialpreparer.ExitRefused && code != materialpreparer.ExitInvalidRequest {
			t.Errorf("%s: exit status %d (%s), want a refusal", name, code, reason)
		}
	}
	if requests := harness.api.Requests(); len(requests) != 0 {
		t.Fatalf("a refused request reached the Kubernetes API: %s %s", requests[0].Method, requests[0].Path)
	}
	// Managing no namespace at all serves nothing.
	harness.config.ManagedNamespaces = nil
	harness.expect(mutation("SecretMutation", "CREATE", managed, databaseV7, ""), materialpreparer.ExitMisconfigured)
}

func TestUnusableSourcesAndMalformedRequestsEachHaveTheirOwnStatus(t *testing.T) {
	harness := newHarness(t)
	harness.api.Put(sourceNamespace, "tls-source", kubetest.Object{Type: "kubernetes.io/dockerconfigjson", Data: map[string][]byte{".dockerconfigjson": []byte("not json")}})
	for name, test := range map[string]struct {
		request string
		want    int
	}{
		"a key the source does not hold": {mutation("SecretMutation", "CREATE", managed,
			access("database", 7, "kubernetes-secret://database-source", "database-credentials", "password", "port"), ""), materialpreparer.ExitSourceUnusable},
		"a registry source that is not JSON": {registryRequest(managed, "kubernetes-secret://tls-source", "registry-pull"), materialpreparer.ExitSourceUnusable},
		"image pre-pull": {`{"apiVersion":"platform.delivery/v1alpha1","kind":"ImagePrePull","namespace":"example","access":{"images":[]}}`,
			materialpreparer.ExitUnsupported},
		"not JSON":                {`{`, materialpreparer.ExitInvalidRequest},
		"another apiVersion":      {`{"apiVersion":"platform.delivery/v2","kind":"SecretMutation","namespace":"example","access":{}}`, materialpreparer.ExitInvalidRequest},
		"an unknown kind":         {`{"apiVersion":"platform.delivery/v1alpha1","kind":"Shell","namespace":"example","access":{}}`, materialpreparer.ExitInvalidRequest},
		"an unknown action":       {mutation("SecretMutation", "PURGE", managed, databaseV7, ""), materialpreparer.ExitInvalidRequest},
		"CREATE with no current":  {mutation("SecretMutation", "CREATE", managed, "", ""), materialpreparer.ExitInvalidRequest},
		"DELETE with no previous": {mutation("SecretMutation", "DELETE", managed, "", ""), materialpreparer.ExitInvalidRequest},
		"no keys": {mutation("SecretMutation", "CREATE", managed,
			access("database", 7, "kubernetes-secret://database-source", "database-credentials"), ""), materialpreparer.ExitInvalidRequest},
		"an oversized request": {strings.Repeat(" ", materialpreparer.MaximumRequestBytes+1), materialpreparer.ExitInvalidRequest},
	} {
		if code, reason := harness.run(test.request); code != test.want {
			t.Errorf("%s: exit status %d (%s), want %d", name, code, reason, test.want)
		}
	}
	// A source Secret that does not exist is the source's problem, and the
	// documented RBAC answers 403 for a name it does not list -- which the
	// preparer reports as an API failure rather than guessing which it was.
	harness.expect(mutation("SecretMutation", "CREATE", managed,
		access("database", 7, "kubernetes-secret://unlisted-source", "database-credentials", "password"), ""), materialpreparer.ExitAPIFailure)
	if _, found := harness.api.Get(managed, "database-credentials"); found {
		t.Fatal("a failed request left a Secret behind")
	}
}

func TestAMissingSourceSecretIsReportedAsTheSourcesProblem(t *testing.T) {
	api := kubetest.New(t, sourceNamespace) // no RBAC table: a 404 is a 404
	secrets, err := materialpreparer.NewInClusterSecrets(api.Getenv(managed), api.Directory)
	if err != nil {
		t.Fatal(err)
	}
	err = materialpreparer.Run(context.Background(), strings.NewReader(mutation("SecretMutation", "CREATE", managed, databaseV7, "")),
		materialpreparer.Config{SourceNamespace: sourceNamespace, ManagedNamespaces: []string{managed}}, secrets)
	var failure *materialpreparer.Failure
	if !errors.As(err, &failure) || failure.Code != materialpreparer.ExitSourceUnusable {
		t.Fatalf("missing source: %v", err)
	}
}

// Another writer creating the target between the preparer's read and its
// write is not a failure: the preparer reads again and, finding a Secret it
// owns, replaces it.
func TestAWriteThatLosesARaceIsRetriedOnce(t *testing.T) {
	harness := newHarness(t)
	raced := false
	harness.api.Before(func(request kubetest.Request) (int, string) {
		if request.Verb == "create" && !raced {
			raced = true
			harness.api.Put(managed, "database-credentials", kubetest.Object{
				Type: "Opaque", Data: map[string][]byte{"password": []byte("the-other-replicas-write")},
				Labels:      map[string]string{"app.kubernetes.io/managed-by": "platform-material-preparer"},
				Annotations: map[string]string{"platform.delivery/material-assignment": "database", "platform.delivery/material-kind": "secret"},
			})
			return http.StatusConflict, ""
		}
		return 0, ""
	})
	harness.expect(mutation("SecretMutation", "CREATE", managed, databaseV7, ""), materialpreparer.ExitOK)
	written, _ := harness.api.Get(managed, "database-credentials")
	if !raced || string(written.Data["password"]) != databasePassword || written.Annotations["platform.delivery/material-revision"] != "7" {
		t.Fatalf("after a lost race the Secret holds revision %q", written.Annotations["platform.delivery/material-revision"])
	}
}

// Secret bytes go from the API to the API. Every other place a byte could go
// -- a diagnostic, an error, a URL, a header -- is checked here, across the
// success paths and every failure path that has read the source by the time
// it fails, including an API server that echoes the rejected object back.
func TestSecretBytesAppearNowhereButInTheSecretsWrittenToTheAPI(t *testing.T) {
	harness := newHarness(t)
	secretValues := []string{databasePassword, rotatedPassword, registryConfig, "cmVnaXN0cnktdG9rZW4tNGU5YQ==", "not-for-the-workload", harness.api.Token}
	harness.api.Put(sourceNamespace, "database-source-v8", kubetest.Object{Type: "Opaque", Data: map[string][]byte{"password": []byte(rotatedPassword)}})
	databaseV8 := access("database", 8, "kubernetes-secret://database-source-v8", "database-credentials", "password")

	var said bytes.Buffer
	serve := func(request string) {
		t.Helper()
		_, reason := harness.run(request)
		said.WriteString(reason + "\n")
	}
	serve(mutation("SecretMutation", "CREATE", managed, databaseV7, ""))
	serve(mutation("SecretMutation", "VERIFY", managed, databaseV7, ""))
	serve(mutation("SecretMutation", "REPLACE", managed, databaseV8, databaseV7))
	serve(mutation("SecretMutationRollback", "REPLACE", managed, databaseV8, databaseV7))
	serve(registryRequest(managed, "kubernetes-secret://registry-source", "registry-pull"))
	// Failures after the source has been read.
	serve(mutation("SecretMutation", "CREATE", managed, access("database", 7, "kubernetes-secret://database-source", "x", "password", "missing"), ""))
	serve(registryRequest(managed, "kubernetes-secret://database-source", "other-pull"))
	harness.api.Put(managed, "tenant-owned", kubetest.Object{Type: "Opaque", Data: map[string][]byte{"password": []byte("tenant")}})
	serve(mutation("SecretMutation", "CREATE", managed, access("database", 7, "kubernetes-secret://database-source", "tenant-owned", "password"), ""))
	// An API server that rejects a write and quotes the object in its error.
	harness.api.Before(func(request kubetest.Request) (int, string) {
		if request.Verb == "create" || request.Verb == "update" {
			return http.StatusUnprocessableEntity, `{"kind":"Status","message":"Secret is invalid: ` + string(request.Body) + ` password=` + databasePassword + `"}`
		}
		return 0, ""
	})
	code, reason := harness.run(mutation("SecretMutation", "CREATE", managed, access("database", 7, "kubernetes-secret://database-source", "rejected", "password"), ""))
	if code != materialpreparer.ExitAPIFailure {
		t.Fatalf("a rejected write: exit status %d (%s)", code, reason)
	}
	said.WriteString(reason + "\n")

	for _, value := range secretValues {
		if strings.Contains(said.String(), value) {
			t.Errorf("a diagnostic disclosed secret material %q:\n%s", value, said.String())
		}
	}
	// The base64 form is how a Secret travels in a request body; it must not
	// travel anywhere else either.
	wrote := 0
	for _, request := range harness.api.Requests() {
		for _, value := range secretValues[:5] {
			if strings.Contains(request.Path, value) {
				t.Errorf("a request path carries secret material: %s", request.Path)
			}
		}
		if request.Verb != "create" && request.Verb != "update" {
			if len(request.Body) != 0 && request.Verb != "delete" {
				t.Errorf("%s %s carried a body", request.Method, request.Path)
			}
			continue
		}
		if request.Namespace != managed {
			t.Errorf("a Secret was written outside the managed namespace: %s", request.Path)
		}
		wrote++
	}
	if wrote == 0 {
		t.Fatal("no Secret was written; this test checked nothing")
	}
}

func TestInClusterCredentialsAreRequiredAndReadFromThePod(t *testing.T) {
	api := kubetest.New(t, sourceNamespace)
	if _, err := materialpreparer.NewInClusterSecrets(func(string) string { return "" }, api.Directory); err == nil {
		t.Fatal("a client was built with no API address")
	}
	if _, err := materialpreparer.NewInClusterSecrets(api.Getenv(managed), t.TempDir()); err == nil {
		t.Fatal("a client was built with no ServiceAccount CA")
	}
	if namespace, err := materialpreparer.PodNamespace(api.Directory); err != nil || namespace != sourceNamespace {
		t.Fatalf("Pod namespace = %q, %v", namespace, err)
	}
	if namespaces, err := materialpreparer.ParseNamespaces(" example, other ,"); err != nil || !reflect.DeepEqual(namespaces, []string{"example", "other"}) {
		t.Fatalf("namespaces = %v, %v", namespaces, err)
	}
	if _, err := materialpreparer.ParseNamespaces("example,*"); err == nil {
		t.Fatal("a wildcard was accepted as a managed namespace")
	}
	// A wrong token is the API's refusal, reported as such.
	api.Token = "rotated-away"
	secrets, err := materialpreparer.NewInClusterSecrets(api.Getenv(managed), api.Directory)
	if err != nil {
		t.Fatal(err)
	}
	err = materialpreparer.Run(context.Background(), strings.NewReader(mutation("SecretMutation", "CREATE", managed, databaseV7, "")),
		materialpreparer.Config{SourceNamespace: sourceNamespace, ManagedNamespaces: []string{managed}}, secrets)
	var failure *materialpreparer.Failure
	if !errors.As(err, &failure) || failure.Code != materialpreparer.ExitAPIFailure {
		t.Fatalf("an unauthenticated API call: %v", err)
	}
}
