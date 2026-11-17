package main

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/materialpreparer"
	"github.com/jonathanagustin/conduit/internal/materialpreparer/kubetest"
)

const (
	sourceNamespace  = "environment-system"
	managedNamespace = "example"
	databasePassword = "s3cr3t-database-password-7f3a9c"
	registryConfig   = `{"auths":{"registry.example":{"auth":"cmVnaXN0cnktdG9rZW4tNGU5YQ=="}}}`
)

// buildPreparer compiles this package the way Containerfile.agent does, so the
// tests below run the executable an Agent would run rather than a function
// that resembles it.
func buildPreparer(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "platform-material-preparer")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the preparer: %v\n%s", err, output)
	}
	return binary
}

func seededAPI(t *testing.T) *kubetest.Server {
	t.Helper()
	// The RBAC docs/operations/material-preparer.md states, and nothing wider.
	api := kubetest.New(t, sourceNamespace,
		kubetest.Rule{Namespace: sourceNamespace, Verbs: []string{"get"}, ResourceNames: []string{"database-source", "registry-source"}},
		kubetest.Rule{Namespace: managedNamespace, Verbs: []string{"get", "create", "update", "delete"}},
	)
	api.Put(sourceNamespace, "database-source", kubetest.Object{Type: "Opaque", Data: map[string][]byte{
		"username": []byte("app"), "password": []byte(databasePassword),
	}})
	api.Put(sourceNamespace, "registry-source", kubetest.Object{
		Type: "kubernetes.io/dockerconfigjson", Data: map[string][]byte{".dockerconfigjson": []byte(registryConfig)},
	})
	return api
}

// The contract, end to end: the Agent's own ExternalMaterialPreparer -- the
// code cmd/platform-spoke-agent wires to EXTERNAL_MATERIAL_PREPARER -- starts
// the built executable as a process, writes the request it really writes, and
// reads the exit status it really reads. Nothing here is a seam: if the Agent's
// request shape and this preparer's reading of it drift apart, this fails.
func TestTheAgentsOwnCallerDrivesTheBuiltPreparer(t *testing.T) {
	binary := buildPreparer(t)
	api := seededAPI(t)
	// The Agent starts the preparer with its own environment, which in a Pod
	// is where the API address and the chart-rendered namespace list are.
	for name, value := range api.Environment(managedNamespace) {
		t.Setenv(name, value)
	}
	preparer, err := agent.NewExternalMaterialPreparer(binary)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Registry access: an image pull Secret from a dockerconfigjson source.
	registry := controlplane.RegistryAccess{
		AssignmentID: "primary", Revision: 4, SourceRegistry: "registry.example", RuntimeRegistry: "registry.example",
		CredentialRef: "kubernetes-secret://registry-source", ImagePullSecretName: "registry-pull",
	}
	if err := preparer.Prepare(ctx, registry, managedNamespace); err != nil {
		t.Fatalf("prepare registry access: %v", err)
	}
	pull, found := api.Get(managedNamespace, "registry-pull")
	if !found || pull.Type != "kubernetes.io/dockerconfigjson" || string(pull.Data[".dockerconfigjson"]) != registryConfig {
		t.Fatalf("image pull Secret: found=%t type=%q", found, pull.Type)
	}

	// A Secret mutation: create, verify, then roll back.
	current := controlplane.SecretAccess{
		Requirement: "database", Assignment: "database", Revision: 7, ProviderRef: "kubernetes-secret://database-source",
		KubernetesSecretName: "database-credentials", Keys: []string{"password"},
	}
	create := controlplane.SecretMutation{Position: 1, Action: controlplane.SecretCreate, Namespace: managedNamespace, Current: &current}
	if err := preparer.ApplySecretMutation(ctx, create); err != nil {
		t.Fatalf("apply CREATE: %v", err)
	}
	created, found := api.Get(managedNamespace, "database-credentials")
	if !found || len(created.Data) != 1 || string(created.Data["password"]) != databasePassword ||
		created.Annotations[materialpreparer.RevisionAnnotation] != "7" {
		t.Fatalf("created Secret: found=%t keys=%d revision=%q", found, len(created.Data), created.Annotations[materialpreparer.RevisionAnnotation])
	}
	verify := controlplane.SecretMutation{Position: 1, Action: controlplane.SecretVerify, Namespace: managedNamespace, Current: &current, Previous: &current}
	if err := preparer.ApplySecretMutation(ctx, verify); err != nil {
		t.Fatalf("apply VERIFY: %v", err)
	}
	stale := current
	stale.Revision = 8
	drifted := controlplane.SecretMutation{Position: 1, Action: controlplane.SecretVerify, Namespace: managedNamespace, Current: &stale}
	if err := preparer.ApplySecretMutation(ctx, drifted); err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("VERIFY of a revision that is not installed: %v, want exit status 7", err)
	}
	if err := preparer.RollbackSecretMutation(ctx, create); err != nil {
		t.Fatalf("roll back CREATE: %v", err)
	}
	if _, found := api.Get(managedNamespace, "database-credentials"); found {
		t.Fatal("rolling back the CREATE left the Secret in place")
	}

	// A namespace the Agent does not manage is refused, and the refusal is
	// what the Agent gets to report.
	outside := create
	outside.Namespace = "kube-system"
	if err := preparer.ApplySecretMutation(ctx, outside); err == nil || !strings.Contains(err.Error(), "exit status 4") {
		t.Fatalf("a mutation for an unmanaged namespace: %v, want exit status 4", err)
	}
	if err := preparer.Prepare(ctx, registry, "kube-system"); err == nil || !strings.Contains(err.Error(), "exit status 4") {
		t.Fatalf("registry access for an unmanaged namespace: %v, want exit status 4", err)
	}
	for _, request := range api.Requests() {
		if request.Namespace == "kube-system" {
			t.Fatalf("a refused request reached the API: %s %s", request.Method, request.Path)
		}
	}
	// Image pre-pull is declined rather than acknowledged.
	images := []controlplane.Artifact{{Repository: "registry.example/app", Tag: "1.0.0", Digest: "sha256:" + strings.Repeat("a", 64)}}
	if err := preparer.PrepareImages(ctx, images, []controlplane.RegistryAccess{registry}, managedNamespace); err == nil ||
		!strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("image pre-pull: %v, want exit status 3", err)
	}
	// Nothing the Agent can report ever contains material: all it has is the
	// status.
	for _, err := range []error{preparer.ApplySecretMutation(ctx, outside), preparer.ApplySecretMutation(ctx, drifted)} {
		if strings.Contains(err.Error(), databasePassword) {
			t.Fatalf("the Agent's error carries secret material: %v", err)
		}
	}
}

// The Agent discards what the preparer writes, but an operator running it by
// hand does not, and neither does a wrapper script. Standard output stays
// empty and standard error names objects, never values -- on success, on a
// refusal, and when the API itself answers with the object in its error.
func TestTheBuiltPreparerWritesNoSecretMaterialToItsOutputOrItsArguments(t *testing.T) {
	binary := buildPreparer(t)
	api := seededAPI(t)
	secretValues := []string{databasePassword, registryConfig, "cmVnaXN0cnktdG9rZW4tNGU5YQ==", api.Token}
	invoke := func(request string, arguments ...string) (int, string, string) {
		t.Helper()
		process := exec.Command(binary, arguments...)
		process.Stdin = strings.NewReader(request)
		var stdout, stderr bytes.Buffer
		process.Stdout, process.Stderr = &stdout, &stderr
		for name, value := range api.Environment(managedNamespace) {
			process.Env = append(process.Env, name+"="+value)
		}
		err := process.Run()
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return code, stdout.String(), stderr.String()
	}
	create := `{"apiVersion":"platform.delivery/v1alpha1","kind":"SecretMutation","namespace":"example","access":{"position":1,"action":"CREATE","namespace":"example",` +
		`"current":{"requirement":"database","assignment":"database","revision":7,"providerRef":"kubernetes-secret://database-source","kubernetesSecretName":"database-credentials","keys":["password"]}}}`
	registry := `{"apiVersion":"platform.delivery/v1alpha1","kind":"RegistryAccess","namespace":"example","access":{"assignmentId":"primary","revision":4,` +
		`"credentialRef":"kubernetes-secret://registry-source","imagePullSecretName":"registry-pull"}}`
	missingKey := strings.Replace(create, `"keys":["password"]`, `"keys":["password","port"]`, 1)
	outside := strings.ReplaceAll(create, `"namespace":"example"`, `"namespace":"kube-system"`)

	var everything strings.Builder
	for name, test := range map[string]struct {
		request string
		want    int
	}{
		"create":                 {create, materialpreparer.ExitOK},
		"registry":               {registry, materialpreparer.ExitOK},
		"a key the source lacks": {missingKey, materialpreparer.ExitSourceUnusable},
		"an unmanaged namespace": {outside, materialpreparer.ExitRefused},
		"not a request":          {databasePassword, materialpreparer.ExitInvalidRequest},
	} {
		code, stdout, stderr := invoke(test.request)
		if code != test.want {
			t.Errorf("%s: exit status %d, want %d (stderr: %s)", name, code, test.want, stderr)
		}
		if stdout != "" {
			t.Errorf("%s: the preparer wrote to standard output: %q", name, stdout)
		}
		everything.WriteString(stderr)
	}
	// An API server that quotes the rejected Secret back in its error.
	api.Before(func(request kubetest.Request) (int, string) {
		if request.Verb == "update" || request.Verb == "create" {
			return 422, `{"message":"invalid: ` + databasePassword + `"}`
		}
		return 0, ""
	})
	code, stdout, stderr := invoke(create)
	if code != materialpreparer.ExitAPIFailure || stdout != "" {
		t.Errorf("a rejected write: exit status %d, stdout %q", code, stdout)
	}
	everything.WriteString(stderr)
	for _, value := range secretValues {
		if strings.Contains(everything.String(), value) {
			t.Errorf("standard error disclosed secret material %q:\n%s", value, everything.String())
		}
	}
	if everything.Len() == 0 {
		t.Fatal("no diagnostic was produced; this test checked nothing")
	}

	// Arguments are refused outright, so there is no supported way to put a
	// request -- or a credential -- on a command line.
	api.Reset()
	if code, stdout, _ := invoke(create, "--request", create); code != materialpreparer.ExitInvalidRequest || stdout != "" {
		t.Errorf("an invocation with arguments: exit status %d, stdout %q", code, stdout)
	}
	if requests := api.Requests(); len(requests) != 0 {
		t.Errorf("an invocation with arguments reached the API: %s %s", requests[0].Method, requests[0].Path)
	}
}

func TestThePreparerRefusesToRunWithoutItsConfiguration(t *testing.T) {
	api := seededAPI(t)
	request := `{"apiVersion":"platform.delivery/v1alpha1","kind":"ImagePrePull","namespace":"example","access":{}}`
	for name, strip := range map[string]string{
		"no managed namespaces": "MATERIAL_PREPARER_NAMESPACES",
		"no API address":        "KUBERNETES_SERVICE_HOST",
	} {
		environment := api.Environment(managedNamespace)
		delete(environment, strip)
		var diagnostics bytes.Buffer
		code := run(context.Background(), nil, strings.NewReader(request), &diagnostics, func(name string) string { return environment[name] })
		if code != materialpreparer.ExitMisconfigured || diagnostics.Len() == 0 {
			t.Errorf("%s: exit status %d (%s), want misconfigured", name, code, diagnostics.String())
		}
	}
	// With everything in place the same request is served -- and declined on
	// its merits, with its own status.
	var diagnostics bytes.Buffer
	if code := run(context.Background(), nil, strings.NewReader(request), &diagnostics, api.Getenv(managedNamespace)); code != materialpreparer.ExitUnsupported {
		t.Fatalf("exit status %d (%s), want unsupported", code, diagnostics.String())
	}
	// The source namespace defaults to the Pod's own and can be named instead.
	environment := api.Environment(managedNamespace)
	environment["MATERIAL_PREPARER_SOURCE_NAMESPACE"] = "another-namespace"
	create := `{"apiVersion":"platform.delivery/v1alpha1","kind":"SecretMutation","namespace":"example","access":{"action":"CREATE","namespace":"example",` +
		`"current":{"assignment":"database","revision":7,"providerRef":"kubernetes-secret://database-source","kubernetesSecretName":"database-credentials","keys":["password"]}}}`
	diagnostics.Reset()
	_ = run(context.Background(), nil, strings.NewReader(create), &diagnostics, func(name string) string { return environment[name] })
	requests := api.Requests()
	if len(requests) == 0 || requests[len(requests)-1].Namespace != "another-namespace" {
		t.Fatalf("the named source namespace was not the one read: %#v", requests)
	}
}
