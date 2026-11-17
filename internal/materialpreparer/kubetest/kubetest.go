// Package kubetest is a Kubernetes API server small enough to read, for
// testing the material preparer against the requests it really issues.
//
// It serves Secrets only, over TLS, with bearer-token authentication,
// resourceVersion preconditions and an RBAC table. The RBAC table is the point:
// a test states the exact rules docs/operations/material-preparer.md tells an
// operator to grant, and the preparer has to work under those and nothing
// more. Every request is recorded, so a test can also say what was never
// asked for -- a list, a write to the source namespace, a call for a namespace
// that should have been refused before any call was made.
package kubetest

import (
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Rule is one RBAC grant: these verbs on Secrets in this namespace, optionally
// restricted to named Secrets. As in Kubernetes, a resourceNames restriction
// cannot authorize create.
type Rule struct {
	Namespace     string
	Verbs         []string
	ResourceNames []string
}

// Request is one call the server received.
type Request struct {
	Verb, Namespace, Name string
	Method, Path          string
	Body                  []byte
	Status                int
}

// Object is a stored Secret.
type Object struct {
	Type            string
	Labels          map[string]string
	Annotations     map[string]string
	Data            map[string][]byte
	ResourceVersion string
}

type Server struct {
	// Token is the bearer token the server accepts.
	Token string
	// Directory is a ServiceAccount directory holding ca.crt, token and
	// namespace for this server.
	Directory string

	server *httptest.Server
	mutex  sync.Mutex
	rules  []Rule
	// Before runs ahead of each request and may return a status to answer
	// with instead, along with a body. It is how a test injects a failure or
	// plays a second writer.
	before   func(Request) (int, string)
	objects  map[string]Object
	requests []Request
	version  int
}

// New starts a server whose Pod namespace is podNamespace and which enforces
// rules. With no rules everything authenticated is allowed.
func New(t *testing.T, podNamespace string, rules ...Rule) *Server {
	t.Helper()
	fake := &Server{Token: "test-serviceaccount-token", rules: rules, objects: map[string]Object{}}
	fake.server = httptest.NewTLSServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	fake.Directory = t.TempDir()
	authority := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fake.server.Certificate().Raw})
	for name, contents := range map[string][]byte{
		"ca.crt": authority, "token": []byte(fake.Token + "\n"), "namespace": []byte(podNamespace),
	} {
		if err := os.WriteFile(filepath.Join(fake.Directory, name), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return fake
}

// Environment is what a Pod's environment and the preparer's own variables
// would hold for this server.
func (fake *Server) Environment(managedNamespaces string) map[string]string {
	host, port, _ := net.SplitHostPort(fake.server.Listener.Addr().String())
	return map[string]string{
		"KUBERNETES_SERVICE_HOST":              host,
		"KUBERNETES_SERVICE_PORT":              port,
		"MATERIAL_PREPARER_SERVICEACCOUNT_DIR": fake.Directory,
		"MATERIAL_PREPARER_NAMESPACES":         managedNamespaces,
	}
}

// Getenv is Environment as a lookup function.
func (fake *Server) Getenv(managedNamespaces string) func(string) string {
	environment := fake.Environment(managedNamespaces)
	return func(name string) string { return environment[name] }
}

// Put stores a Secret directly, as an operator or another controller would.
func (fake *Server) Put(namespace, name string, object Object) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.version++
	object.ResourceVersion = strconv.Itoa(fake.version)
	fake.objects[namespace+"/"+name] = object
}

// Get returns a stored Secret.
func (fake *Server) Get(namespace, name string) (Object, bool) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	object, found := fake.objects[namespace+"/"+name]
	return object, found
}

// Requests returns every call received so far.
func (fake *Server) Requests() []Request {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]Request(nil), fake.requests...)
}

// Reset forgets the recorded calls.
func (fake *Server) Reset() {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.requests = nil
}

// Before installs the interception hook. It is called without the server's
// lock held, so it may call Put.
func (fake *Server) Before(hook func(Request) (int, string)) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.before = hook
}

func (fake *Server) allowed(verb, namespace, name string) bool {
	if len(fake.rules) == 0 {
		return true
	}
	for _, rule := range fake.rules {
		if rule.Namespace != namespace || !contains(rule.Verbs, verb) {
			continue
		}
		if len(rule.ResourceNames) == 0 {
			return true
		}
		if verb != "create" && verb != "list" && contains(rule.ResourceNames, name) {
			return true
		}
	}
	return false
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

type wireSecret struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name            string            `json:"name"`
		Namespace       string            `json:"namespace,omitempty"`
		Labels          map[string]string `json:"labels,omitempty"`
		Annotations     map[string]string `json:"annotations,omitempty"`
		ResourceVersion string            `json:"resourceVersion,omitempty"`
	} `json:"metadata"`
	Type string            `json:"type,omitempty"`
	Data map[string][]byte `json:"data,omitempty"`
}

func (fake *Server) serve(writer http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	recorded := Request{Method: request.Method, Path: request.URL.Path, Body: body}
	respond := func(status int, payload any) {
		recorded.Status = status
		fake.mutex.Lock()
		fake.requests = append(fake.requests, recorded)
		fake.mutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		switch value := payload.(type) {
		case nil:
		case string:
			_, _ = io.WriteString(writer, value)
		default:
			_ = json.NewEncoder(writer).Encode(value)
		}
	}
	// /api/v1/namespaces/{namespace}/secrets[/{name}]
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) < 5 || len(parts) > 6 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "namespaces" || parts[4] != "secrets" {
		respond(http.StatusNotFound, nil)
		return
	}
	recorded.Namespace = parts[3]
	if len(parts) == 6 {
		recorded.Name = parts[5]
	}
	switch {
	case request.Method == http.MethodGet && recorded.Name == "":
		recorded.Verb = "list"
	case request.Method == http.MethodGet:
		recorded.Verb = "get"
	case request.Method == http.MethodPost && recorded.Name == "":
		recorded.Verb = "create"
	case request.Method == http.MethodPut && recorded.Name != "":
		recorded.Verb = "update"
	case request.Method == http.MethodDelete && recorded.Name != "":
		recorded.Verb = "delete"
	default:
		respond(http.StatusMethodNotAllowed, nil)
		return
	}
	if request.Header.Get("Authorization") != "Bearer "+fake.Token {
		respond(http.StatusUnauthorized, nil)
		return
	}
	var submitted wireSecret
	if recorded.Verb == "create" || recorded.Verb == "update" {
		if err := json.Unmarshal(body, &submitted); err != nil {
			respond(http.StatusBadRequest, nil)
			return
		}
		if recorded.Verb == "create" {
			recorded.Name = submitted.Metadata.Name
		}
	}
	if !fake.allowed(recorded.Verb, recorded.Namespace, recorded.Name) {
		respond(http.StatusForbidden, nil)
		return
	}
	fake.mutex.Lock()
	hook := fake.before
	fake.mutex.Unlock()
	if hook != nil {
		if status, payload := hook(recorded); status != 0 {
			respond(status, payload)
			return
		}
	}
	fake.mutex.Lock()
	key := recorded.Namespace + "/" + recorded.Name
	existing, found := fake.objects[key]
	status, payload := http.StatusOK, any(nil)
	switch recorded.Verb {
	case "list":
		status = http.StatusNotImplemented
	case "get":
		if !found {
			status = http.StatusNotFound
			break
		}
		var response wireSecret
		response.APIVersion, response.Kind, response.Type, response.Data = "v1", "Secret", existing.Type, existing.Data
		response.Metadata.Name, response.Metadata.Namespace = recorded.Name, recorded.Namespace
		response.Metadata.Labels, response.Metadata.Annotations = existing.Labels, existing.Annotations
		response.Metadata.ResourceVersion = existing.ResourceVersion
		payload = response
	case "create":
		if found {
			status = http.StatusConflict
			break
		}
		fake.version++
		fake.objects[key] = Object{
			Type: submitted.Type, Labels: submitted.Metadata.Labels, Annotations: submitted.Metadata.Annotations,
			Data: submitted.Data, ResourceVersion: strconv.Itoa(fake.version),
		}
		status = http.StatusCreated
	case "update":
		switch {
		case !found:
			status = http.StatusNotFound
		case submitted.Metadata.ResourceVersion != existing.ResourceVersion || submitted.Metadata.Name != recorded.Name:
			status = http.StatusConflict
		default:
			fake.version++
			fake.objects[key] = Object{
				Type: submitted.Type, Labels: submitted.Metadata.Labels, Annotations: submitted.Metadata.Annotations,
				Data: submitted.Data, ResourceVersion: strconv.Itoa(fake.version),
			}
		}
	case "delete":
		var options struct {
			Preconditions struct {
				ResourceVersion string `json:"resourceVersion"`
			} `json:"preconditions"`
		}
		_ = json.Unmarshal(body, &options)
		switch {
		case !found:
			status = http.StatusNotFound
		case options.Preconditions.ResourceVersion != existing.ResourceVersion:
			status = http.StatusConflict
		default:
			delete(fake.objects, key)
		}
	}
	fake.mutex.Unlock()
	respond(status, payload)
}
