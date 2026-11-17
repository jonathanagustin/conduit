package materialpreparer

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultServiceAccountDirectory is where Kubernetes projects a Pod's
// credentials. The preparer runs inside the Agent's Pod and so acts with the
// Agent's ServiceAccount; it has no identity of its own.
const DefaultServiceAccountDirectory = "/var/run/secrets/kubernetes.io/serviceaccount"

// StatusError is a Kubernetes API response that was not a success. It carries
// the status code and nothing from the response body: an API error body can
// quote the object that was rejected, and for this client that object is a
// Secret.
type StatusError struct {
	Operation string
	Status    int
}

func (err *StatusError) Error() string {
	return fmt.Sprintf("%s answered HTTP %d", err.Operation, err.Status)
}

// InClusterSecrets talks to the Kubernetes API with the Pod's own credentials,
// using only the standard library. It issues the four requests the Secrets
// interface names and no others -- in particular it never lists, so the RBAC
// it needs does not include reading every Secret in a namespace.
type InClusterSecrets struct {
	base      string
	tokenPath string
	client    *http.Client
}

// NewInClusterSecrets reads the API address from the environment Kubernetes
// sets in every Pod and the credentials from the ServiceAccount directory.
func NewInClusterSecrets(getenv func(string) string, directory string) (*InClusterSecrets, error) {
	host, port := getenv("KUBERNETES_SERVICE_HOST"), getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, errors.New("KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT are not set; the preparer runs inside a Pod")
	}
	authority, err := os.ReadFile(filepath.Join(directory, "ca.crt"))
	if err != nil {
		return nil, errors.New("the ServiceAccount CA bundle could not be read")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(authority) {
		return nil, errors.New("the ServiceAccount CA bundle holds no certificate")
	}
	return &InClusterSecrets{
		base:      "https://" + net.JoinHostPort(host, port),
		tokenPath: filepath.Join(directory, "token"),
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
				// The API address is the in-cluster Service. A proxy configured
				// for the Agent's registry traffic has no business carrying a
				// ServiceAccount token.
				Proxy: nil,
			},
			// A redirect would resend the bearer token to wherever it pointed.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// PodNamespace is the namespace the Pod runs in, which is where the
// reference deployment keeps its source Secrets unless told otherwise.
func PodNamespace(directory string) (string, error) {
	contents, err := os.ReadFile(filepath.Join(directory, "namespace"))
	if err != nil {
		return "", errors.New("the Pod's namespace could not be read")
	}
	return strings.TrimSpace(string(contents)), nil
}

type apiSecret struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   apiMetadata       `json:"metadata"`
	Type       string            `json:"type,omitempty"`
	Data       map[string][]byte `json:"data,omitempty"`
}

type apiMetadata struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
}

func (secrets *InClusterSecrets) path(namespace, name string) string {
	path := secrets.base + "/api/v1/namespaces/" + url.PathEscape(namespace) + "/secrets"
	if name != "" {
		path += "/" + url.PathEscape(name)
	}
	return path
}

// do issues one request. The token is read per request because the kubelet
// rotates the projected file; the response body is returned only on success
// and is otherwise drained and dropped.
func (secrets *InClusterSecrets) do(ctx context.Context, operation, method, target string, body any) ([]byte, error) {
	token, err := os.ReadFile(secrets.tokenPath)
	if err != nil {
		return nil, errors.New("the ServiceAccount token could not be read")
	}
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, errors.New("the request could not be encoded")
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return nil, errors.New("the request could not be built")
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := secrets.client.Do(request)
	if err != nil {
		// Not wrapped: a transport error quotes the URL, which is harmless, but
		// one rule is easier to keep than two.
		return nil, fmt.Errorf("%s could not reach the Kubernetes API", operation)
	}
	defer response.Body.Close()
	contents, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	switch {
	case response.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case response.StatusCode == http.StatusConflict:
		return nil, ErrConflict
	case response.StatusCode < 200 || response.StatusCode > 299:
		return nil, &StatusError{Operation: operation, Status: response.StatusCode}
	case readErr != nil:
		return nil, fmt.Errorf("%s response could not be read", operation)
	}
	return contents, nil
}

func (secrets *InClusterSecrets) Get(ctx context.Context, namespace, name string) (Secret, error) {
	contents, err := secrets.do(ctx, "get Secret", http.MethodGet, secrets.path(namespace, name), nil)
	if err != nil {
		return Secret{}, err
	}
	var decoded apiSecret
	if err := json.Unmarshal(contents, &decoded); err != nil {
		return Secret{}, errors.New("get Secret returned a body that is not a Secret")
	}
	return Secret{
		Name: decoded.Metadata.Name, Type: decoded.Type, Labels: decoded.Metadata.Labels,
		Annotations: decoded.Metadata.Annotations, Data: decoded.Data, ResourceVersion: decoded.Metadata.ResourceVersion,
	}, nil
}

func encodeSecret(namespace string, secret Secret) apiSecret {
	return apiSecret{
		APIVersion: "v1", Kind: "Secret", Type: secret.Type, Data: secret.Data,
		Metadata: apiMetadata{
			Name: secret.Name, Namespace: namespace, Labels: secret.Labels,
			Annotations: secret.Annotations, ResourceVersion: secret.ResourceVersion,
		},
	}
}

func (secrets *InClusterSecrets) Create(ctx context.Context, namespace string, secret Secret) error {
	secret.ResourceVersion = ""
	_, err := secrets.do(ctx, "create Secret", http.MethodPost, secrets.path(namespace, ""), encodeSecret(namespace, secret))
	return err
}

func (secrets *InClusterSecrets) Update(ctx context.Context, namespace string, secret Secret) error {
	if secret.ResourceVersion == "" {
		return errors.New("an update requires the resourceVersion it replaces")
	}
	_, err := secrets.do(ctx, "update Secret", http.MethodPut, secrets.path(namespace, secret.Name), encodeSecret(namespace, secret))
	return err
}

func (secrets *InClusterSecrets) Delete(ctx context.Context, namespace, name, resourceVersion string) error {
	options := map[string]any{
		"apiVersion": "v1", "kind": "DeleteOptions",
		"preconditions": map[string]string{"resourceVersion": resourceVersion},
	}
	_, err := secrets.do(ctx, "delete Secret", http.MethodDelete, secrets.path(namespace, name), options)
	return err
}
