// Package materialpreparer is the reference implementation of the executable
// the Spoke Agent runs as EXTERNAL_MATERIAL_PREPARER.
//
// The contract is internal/agent/material.go, and it is narrow: the Agent
// starts the executable with no arguments, writes one JSON request to its
// standard input, discards everything it writes, and reads the exit status.
// So the exit status is the whole response, and Failure.Code is this
// package's vocabulary for it.
//
// The provider here is Kubernetes itself. An Environment keeps its material in
// source Secrets in one namespace it owns; a request names one of them by an
// opaque reference, and this package projects the keys the request asks for
// into a Secret in the namespace the Plan targets. A vault is integrated by
// composition -- something else (External Secrets Operator, a CSI driver, a
// person) materialises the source Secret -- so there is no vault client here
// and no dependency for one.
//
// Three rules hold everywhere below and are tested as rules:
//
//   - Secret bytes are read from the API and written to the API. They never
//     reach a log line, an error, standard output or an argument vector.
//   - A request is served only for a namespace the Agent was told it manages.
//   - A Secret this package did not create is never overwritten or deleted.
package materialpreparer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// APIVersion is the only request version this preparer serves.
const APIVersion = "platform.delivery/v1alpha1"

// Exit statuses. The Agent reports "external material preparer failed: exit
// status N" into the Plan's failure, so N is what an operator has to go on.
const (
	ExitOK = 0
	// ExitMisconfigured: the preparer cannot run here at all -- no in-cluster
	// credentials, or no managed namespaces were configured.
	ExitMisconfigured = 1
	// ExitInvalidRequest: standard input was not a request this version reads.
	ExitInvalidRequest = 2
	// ExitUnsupported: a well-formed request for something this reference
	// implementation does not do. Image pre-pull is the one such request.
	ExitUnsupported = 3
	// ExitRefused: the request was understood and declined on a rule: a
	// namespace outside the managed set, a reference outside the grammar, or a
	// target Secret this preparer does not own.
	ExitRefused = 4
	// ExitSourceUnusable: the source Secret is absent, of the wrong type, or
	// lacks a key the request needs.
	ExitSourceUnusable = 5
	// ExitAPIFailure: the Kubernetes API refused or could not be reached.
	// The one status worth retrying unchanged.
	ExitAPIFailure = 6
	// ExitVerificationFailed: a VERIFY found the target Secret absent or not
	// what the Plan says is installed.
	ExitVerificationFailed = 7
)

// Failure is a refusal with the exit status that reports it. Reason is written
// to standard error for a person running the preparer by hand; it names
// objects and keys and never carries a value.
type Failure struct {
	Code   int
	Reason string
}

func (failure *Failure) Error() string { return failure.Reason }

func fail(code int, format string, arguments ...any) *Failure {
	return &Failure{Code: code, Reason: fmt.Sprintf(format, arguments...)}
}

// ManagedByLabel marks a Secret this preparer created. It is the ownership
// test: without it a Secret is somebody else's and is left alone.
const (
	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedByValue = "platform-material-preparer"

	AssignmentAnnotation = "platform.delivery/material-assignment"
	RevisionAnnotation   = "platform.delivery/material-revision"
	SourceAnnotation     = "platform.delivery/material-source"
	KindAnnotation       = "platform.delivery/material-kind"

	kindSecret   = "secret"
	kindRegistry = "registry"

	dockerConfigType = "kubernetes.io/dockerconfigjson"
	dockerConfigKey  = ".dockerconfigjson"
	opaqueType       = "Opaque"
)

// ReferenceScheme is the only provider reference this preparer resolves:
// kubernetes-secret://NAME, a Secret called NAME in the source namespace. The
// reference deliberately has no namespace part, so a Plan cannot aim the
// preparer at a Secret outside the one namespace the Environment set aside.
const ReferenceScheme = "kubernetes-secret://"

var (
	dnsLabel     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	dnsSubdomain = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	secretKey    = regexp.MustCompile(`^[-._a-zA-Z0-9]{1,253}$`)
)

// Secret is the part of a Kubernetes Secret this package reads and writes.
type Secret struct {
	Name            string
	Type            string
	Labels          map[string]string
	Annotations     map[string]string
	Data            map[string][]byte
	ResourceVersion string
}

// Errors a Secrets implementation returns for the two outcomes the preparer
// treats as answers rather than failures.
var (
	ErrNotFound = errors.New("secret not found")
	ErrConflict = errors.New("secret changed or already exists")
)

// Secrets is the Kubernetes API as far as this package needs it. The RBAC a
// deployment grants is exactly these verbs: get in the source namespace; get,
// create, update and delete in each managed namespace.
type Secrets interface {
	Get(ctx context.Context, namespace, name string) (Secret, error)
	Create(ctx context.Context, namespace string, secret Secret) error
	// Update replaces a Secret, conditional on the ResourceVersion it carries.
	Update(ctx context.Context, namespace string, secret Secret) error
	// Delete removes a Secret, conditional on resourceVersion.
	Delete(ctx context.Context, namespace, name, resourceVersion string) error
}

// Config is what the Environment tells the preparer about itself.
type Config struct {
	// SourceNamespace holds the Environment's source Secrets.
	SourceNamespace string
	// ManagedNamespaces are the only namespaces a request may target.
	ManagedNamespaces []string
}

// ParseNamespaces reads a comma-separated namespace list, as the Spoke chart
// renders rbac.managedNamespaces into MATERIAL_PREPARER_NAMESPACES.
func ParseNamespaces(value string) ([]string, error) {
	var namespaces []string
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if !dnsLabel.MatchString(entry) {
			return nil, fmt.Errorf("%q is not a namespace name", entry)
		}
		namespaces = append(namespaces, entry)
	}
	return namespaces, nil
}

type envelope struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Namespace  string          `json:"namespace"`
	Access     json.RawMessage `json:"access"`
}

// The request bodies restate the Agent's JSON rather than importing its types.
// The preparer is a separate executable an Environment may replace, so what it
// depends on is the wire shape; the round-trip test in
// cmd/platform-material-preparer drives it from the Agent's own caller, which
// is what would notice the two drifting apart.
type registryAccess struct {
	AssignmentID        string `json:"assignmentId"`
	Revision            int    `json:"revision"`
	CredentialRef       string `json:"credentialRef"`
	ImagePullSecretName string `json:"imagePullSecretName"`
}

type secretAccess struct {
	Assignment           string   `json:"assignment"`
	Revision             int      `json:"revision"`
	ProviderRef          string   `json:"providerRef"`
	KubernetesSecretName string   `json:"kubernetesSecretName"`
	Keys                 []string `json:"keys"`
}

type secretMutation struct {
	Action    string        `json:"action"`
	Namespace string        `json:"namespace"`
	Current   *secretAccess `json:"current"`
	Previous  *secretAccess `json:"previous"`
}

// MaximumRequestBytes bounds standard input. The Agent's requests are a few
// hundred bytes; anything near this is not one of them.
const MaximumRequestBytes = 1 << 20

// CONDUIT-INVENTED: agent.KubernetesMaterialPreparer
// Displaced-by: a published or sanitized Apollo agent secret-provider contract defining how an Environment resolves registry credentials and installation secrets.
//
// Run serves one request read from input. A nil error is exit status 0; any
// other error is a *Failure naming the status.
func Run(ctx context.Context, input io.Reader, config Config, secrets Secrets) error {
	if secrets == nil || !dnsLabel.MatchString(config.SourceNamespace) {
		return fail(ExitMisconfigured, "a source namespace and Kubernetes credentials are required")
	}
	if len(config.ManagedNamespaces) == 0 {
		return fail(ExitMisconfigured, "no managed namespaces are configured, so no request can be served")
	}
	contents, err := io.ReadAll(io.LimitReader(input, MaximumRequestBytes+1))
	if err != nil || len(contents) > MaximumRequestBytes {
		return fail(ExitInvalidRequest, "the request could not be read or exceeds %d bytes", MaximumRequestBytes)
	}
	var request envelope
	if err := json.Unmarshal(contents, &request); err != nil {
		return fail(ExitInvalidRequest, "the request is not JSON")
	}
	if request.APIVersion != APIVersion {
		return fail(ExitInvalidRequest, "apiVersion must be %s", APIVersion)
	}
	preparer := preparer{config: config, secrets: secrets}
	switch request.Kind {
	case "RegistryAccess":
		var access registryAccess
		if err := json.Unmarshal(request.Access, &access); err != nil {
			return fail(ExitInvalidRequest, "access is not a RegistryAccess")
		}
		return preparer.registry(ctx, request.Namespace, access)
	case "SecretMutation", "SecretMutationRollback":
		var mutation secretMutation
		if err := json.Unmarshal(request.Access, &mutation); err != nil {
			return fail(ExitInvalidRequest, "access is not a SecretMutation")
		}
		// The envelope and the mutation each carry the namespace. They are the
		// same field written twice by the Agent; a request where they differ
		// was not written by it, and which one to believe is not ours to pick.
		if mutation.Namespace != request.Namespace {
			return fail(ExitInvalidRequest, "the mutation's namespace does not match the request's")
		}
		if request.Kind == "SecretMutationRollback" {
			return preparer.rollback(ctx, mutation)
		}
		return preparer.mutate(ctx, mutation)
	case "ImagePrePull":
		// Declined rather than acknowledged. Answering 0 would have the Agent
		// record that immutable images were pre-pulled when nothing pulled
		// them; an Environment that requires pre-pull supplies a preparer that
		// can reach its nodes.
		return fail(ExitUnsupported, "image pre-pull is not implemented by the reference preparer")
	default:
		return fail(ExitInvalidRequest, "unknown request kind")
	}
}

type preparer struct {
	config  Config
	secrets Secrets
}

// target is one Secret the preparer is asked to make exist.
type target struct {
	namespace, name, kind string
	assignment            string
	revision              int
	reference             string
	keys                  []string
}

func (preparer preparer) authorize(namespace string) error {
	if !dnsLabel.MatchString(namespace) || !slices.Contains(preparer.config.ManagedNamespaces, namespace) {
		return fail(ExitRefused, "namespace %q is not one this Agent manages", namespace)
	}
	return nil
}

func sourceName(reference string) (string, error) {
	name, found := strings.CutPrefix(reference, ReferenceScheme)
	if !found || !dnsSubdomain.MatchString(name) {
		return "", fail(ExitRefused, "the provider reference is not %sNAME", ReferenceScheme)
	}
	return name, nil
}

func (preparer preparer) registry(ctx context.Context, namespace string, access registryAccess) error {
	if err := preparer.authorize(namespace); err != nil {
		return err
	}
	// The Agent calls for any assignment carrying either field. One without
	// the other is nothing this provider can act on, and saying so beats
	// guessing a name or reporting a pull Secret that does not exist.
	if access.CredentialRef == "" || access.ImagePullSecretName == "" {
		return fail(ExitRefused, "registry assignment %q needs both credentialRef and imagePullSecretName", access.AssignmentID)
	}
	return preparer.project(ctx, target{
		namespace: namespace, name: access.ImagePullSecretName, kind: kindRegistry,
		assignment: access.AssignmentID, revision: access.Revision,
		reference: access.CredentialRef, keys: []string{dockerConfigKey},
	})
}

func secretTarget(namespace string, access secretAccess) target {
	return target{
		namespace: namespace, name: access.KubernetesSecretName, kind: kindSecret,
		assignment: access.Assignment, revision: access.Revision,
		reference: access.ProviderRef, keys: access.Keys,
	}
}

func (preparer preparer) mutate(ctx context.Context, mutation secretMutation) error {
	if err := preparer.authorize(mutation.Namespace); err != nil {
		return err
	}
	switch mutation.Action {
	case "CREATE", "REPLACE":
		if mutation.Current == nil {
			return fail(ExitInvalidRequest, "%s carries no current assignment", mutation.Action)
		}
		if err := preparer.project(ctx, secretTarget(mutation.Namespace, *mutation.Current)); err != nil {
			return err
		}
		// A rotation that also renames the Kubernetes Secret leaves the old
		// name behind unless it is removed here.
		if previous := mutation.Previous; previous != nil && previous.KubernetesSecretName != mutation.Current.KubernetesSecretName {
			return preparer.remove(ctx, secretTarget(mutation.Namespace, *previous))
		}
		return nil
	case "VERIFY":
		if mutation.Current == nil {
			return fail(ExitInvalidRequest, "VERIFY carries no current assignment")
		}
		return preparer.verify(ctx, secretTarget(mutation.Namespace, *mutation.Current))
	case "DELETE":
		if mutation.Previous == nil {
			return fail(ExitInvalidRequest, "DELETE carries no previous assignment")
		}
		return preparer.remove(ctx, secretTarget(mutation.Namespace, *mutation.Previous))
	default:
		return fail(ExitInvalidRequest, "unknown secret mutation action")
	}
}

// rollback undoes a mutation the Agent applied earlier in a Plan that then
// failed. It restores what the mutation's Previous describes, from the source
// that reference names NOW: this provider keeps no history. An Environment
// that needs a rollback to restore the old bytes gives each revision its own
// source Secret, so that Previous.ProviderRef still names the old material.
func (preparer preparer) rollback(ctx context.Context, mutation secretMutation) error {
	if err := preparer.authorize(mutation.Namespace); err != nil {
		return err
	}
	switch mutation.Action {
	case "CREATE":
		if mutation.Current == nil {
			return fail(ExitInvalidRequest, "CREATE carries no current assignment")
		}
		return preparer.remove(ctx, secretTarget(mutation.Namespace, *mutation.Current))
	case "REPLACE":
		if mutation.Current == nil || mutation.Previous == nil {
			return fail(ExitInvalidRequest, "REPLACE carries both a current and a previous assignment")
		}
		if err := preparer.project(ctx, secretTarget(mutation.Namespace, *mutation.Previous)); err != nil {
			return err
		}
		if mutation.Previous.KubernetesSecretName != mutation.Current.KubernetesSecretName {
			return preparer.remove(ctx, secretTarget(mutation.Namespace, *mutation.Current))
		}
		return nil
	case "DELETE":
		if mutation.Previous == nil {
			return fail(ExitInvalidRequest, "DELETE carries no previous assignment")
		}
		return preparer.project(ctx, secretTarget(mutation.Namespace, *mutation.Previous))
	case "VERIFY":
		return nil
	default:
		return fail(ExitInvalidRequest, "unknown secret mutation action")
	}
}

func (preparer preparer) validate(target target) error {
	if !dnsSubdomain.MatchString(target.name) {
		return fail(ExitInvalidRequest, "the target Secret name is not a Kubernetes name")
	}
	if strings.TrimSpace(target.assignment) == "" || len(target.assignment) > 253 {
		return fail(ExitInvalidRequest, "the assignment name is required")
	}
	if len(target.keys) == 0 {
		return fail(ExitInvalidRequest, "assignment %q asks for no keys", target.assignment)
	}
	for _, key := range target.keys {
		if !secretKey.MatchString(key) {
			return fail(ExitInvalidRequest, "assignment %q asks for a key that is not a Secret key name", target.assignment)
		}
	}
	return nil
}

// owned reports whether an existing Secret is this assignment's to change.
func owned(existing Secret, target target) error {
	if existing.Labels[ManagedByLabel] != ManagedByValue {
		return fail(ExitRefused, "Secret %s/%s exists and was not created by this preparer; it is left untouched",
			target.namespace, target.name)
	}
	if existing.Annotations[AssignmentAnnotation] != target.assignment || existing.Annotations[KindAnnotation] != target.kind {
		return fail(ExitRefused, "Secret %s/%s belongs to another assignment; it is left untouched", target.namespace, target.name)
	}
	return nil
}

// project makes the target Secret hold exactly the requested keys of the
// source Secret. It is idempotent: a second run with nothing changed writes
// the same object, and a run that lost a race reads again rather than failing.
func (preparer preparer) project(ctx context.Context, target target) error {
	if err := preparer.validate(target); err != nil {
		return err
	}
	name, err := sourceName(target.reference)
	if err != nil {
		return err
	}
	if target.namespace == preparer.config.SourceNamespace && target.name == name {
		return fail(ExitRefused, "the target Secret is the source Secret")
	}
	source, err := preparer.secrets.Get(ctx, preparer.config.SourceNamespace, name)
	switch {
	case errors.Is(err, ErrNotFound):
		return fail(ExitSourceUnusable, "source Secret %s/%s does not exist", preparer.config.SourceNamespace, name)
	case err != nil:
		return fail(ExitAPIFailure, "read source Secret %s/%s: %s", preparer.config.SourceNamespace, name, apiReason(err))
	}
	desired := Secret{
		Name: target.name, Type: opaqueType,
		Labels: map[string]string{ManagedByLabel: ManagedByValue},
		Annotations: map[string]string{
			AssignmentAnnotation: target.assignment, RevisionAnnotation: strconv.Itoa(target.revision),
			SourceAnnotation: target.reference, KindAnnotation: target.kind,
		},
		Data: map[string][]byte{},
	}
	if target.kind == kindRegistry {
		if source.Type != dockerConfigType {
			return fail(ExitSourceUnusable, "source Secret %s/%s is not of type %s", preparer.config.SourceNamespace, name, dockerConfigType)
		}
		desired.Type = dockerConfigType
	}
	for _, key := range target.keys {
		value, present := source.Data[key]
		if !present || len(value) == 0 {
			return fail(ExitSourceUnusable, "source Secret %s/%s has no value for key %q", preparer.config.SourceNamespace, name, key)
		}
		desired.Data[key] = value
	}
	if target.kind == kindRegistry && !json.Valid(desired.Data[dockerConfigKey]) {
		return fail(ExitSourceUnusable, "source Secret %s/%s does not hold a JSON registry configuration", preparer.config.SourceNamespace, name)
	}
	// Two attempts: the second is for a write that lost a race to another
	// writer between the read and the write. Losing twice is reported.
	for attempt := 0; attempt < 2; attempt++ {
		existing, err := preparer.secrets.Get(ctx, target.namespace, target.name)
		switch {
		case errors.Is(err, ErrNotFound):
			err = preparer.secrets.Create(ctx, target.namespace, desired)
		case err != nil:
			return fail(ExitAPIFailure, "read Secret %s/%s: %s", target.namespace, target.name, apiReason(err))
		default:
			if err := owned(existing, target); err != nil {
				return err
			}
			desired.ResourceVersion = existing.ResourceVersion
			err = preparer.secrets.Update(ctx, target.namespace, desired)
		}
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrNotFound) {
			return fail(ExitAPIFailure, "write Secret %s/%s: %s", target.namespace, target.name, apiReason(err))
		}
	}
	return fail(ExitAPIFailure, "Secret %s/%s kept changing underneath the write", target.namespace, target.name)
}

// remove deletes the target Secret if this assignment owns it. An absent
// Secret is success: the state asked for already holds.
func (preparer preparer) remove(ctx context.Context, target target) error {
	if !dnsSubdomain.MatchString(target.name) {
		return fail(ExitInvalidRequest, "the target Secret name is not a Kubernetes name")
	}
	for attempt := 0; attempt < 2; attempt++ {
		existing, err := preparer.secrets.Get(ctx, target.namespace, target.name)
		switch {
		case errors.Is(err, ErrNotFound):
			return nil
		case err != nil:
			return fail(ExitAPIFailure, "read Secret %s/%s: %s", target.namespace, target.name, apiReason(err))
		}
		if err := owned(existing, target); err != nil {
			return err
		}
		err = preparer.secrets.Delete(ctx, target.namespace, target.name, existing.ResourceVersion)
		switch {
		case err == nil, errors.Is(err, ErrNotFound):
			return nil
		case !errors.Is(err, ErrConflict):
			return fail(ExitAPIFailure, "delete Secret %s/%s: %s", target.namespace, target.name, apiReason(err))
		}
	}
	return fail(ExitAPIFailure, "Secret %s/%s kept changing underneath the delete", target.namespace, target.name)
}

// verify changes nothing. It answers whether the Secret the Plan believes is
// installed is installed: present, this assignment's, at this revision, with
// every key it should have.
func (preparer preparer) verify(ctx context.Context, target target) error {
	if err := preparer.validate(target); err != nil {
		return err
	}
	existing, err := preparer.secrets.Get(ctx, target.namespace, target.name)
	switch {
	case errors.Is(err, ErrNotFound):
		return fail(ExitVerificationFailed, "Secret %s/%s does not exist", target.namespace, target.name)
	case err != nil:
		return fail(ExitAPIFailure, "read Secret %s/%s: %s", target.namespace, target.name, apiReason(err))
	}
	if err := owned(existing, target); err != nil {
		return err
	}
	if existing.Annotations[RevisionAnnotation] != strconv.Itoa(target.revision) {
		return fail(ExitVerificationFailed, "Secret %s/%s is at revision %q, not %d",
			target.namespace, target.name, existing.Annotations[RevisionAnnotation], target.revision)
	}
	for _, key := range target.keys {
		if len(existing.Data[key]) == 0 {
			return fail(ExitVerificationFailed, "Secret %s/%s has no value for key %q", target.namespace, target.name, key)
		}
	}
	return nil
}

// apiReason is what an API error may contribute to a diagnostic. The API
// client in this package builds its errors from a status code and nothing
// else, so this is a pass-through; it exists so that a different Secrets
// implementation has one place where its errors meet standard error.
func apiReason(err error) string {
	var status *StatusError
	if errors.As(err, &status) {
		return status.Error()
	}
	return "the Kubernetes API request failed"
}
