package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"go.yaml.in/yaml/v3"
)

type ingressReference struct {
	APIVersion string
	Name       string
}

func deployedIngressReferences(
	ctx context.Context, runner CommandRunner, release, namespace string,
) ([]ingressReference, error) {
	manifest, err := runner.Run(ctx, "helm", "get", "manifest", release, "--namespace", namespace)
	if err != nil {
		return nil, DiagnosticError{Err: fmt.Errorf("inspect deployed manifest for external realization: %w", err), Output: string(manifest), Resources: []controlplane.ResourceDiagnostic{{
			Kind: "HelmRelease", Namespace: namespace, Name: release, Reason: "ManifestObservationFailed",
		}}}
	}
	ingresses, err := ingressReferences(manifest)
	if err != nil {
		return nil, fmt.Errorf("decode deployed manifest for external realization: %w", err)
	}
	return ingresses, nil
}

func observeIngressReferences(
	ctx context.Context, runner CommandRunner, ingresses []ingressReference, namespace string,
) error {
	for _, ingress := range ingresses {
		output, getErr := runner.Run(ctx, "kubectl", "get", "ingress", ingress.Name, "--namespace", namespace, "--output", "json")
		if getErr != nil {
			return ingressDiagnostic(ctx, runner, ingress, namespace, "IngressObservationFailed", getErr, output)
		}
		var observed struct {
			Metadata struct {
				UID        string `json:"uid"`
				Generation int64  `json:"generation"`
			} `json:"metadata"`
			Status struct {
				LoadBalancer struct {
					Ingress []struct {
						Hostname string `json:"hostname"`
						IP       string `json:"ip"`
					} `json:"ingress"`
				} `json:"loadBalancer"`
			} `json:"status"`
		}
		if decodeErr := json.Unmarshal(output, &observed); decodeErr != nil {
			return ingressDiagnostic(ctx, runner, ingress, namespace, "IngressObservationInvalid", decodeErr, output)
		}
		if observed.Metadata.UID == "" || observed.Metadata.Generation < 1 {
			return ingressDiagnostic(ctx, runner, ingress, namespace, "IngressIdentityMissing", fmt.Errorf("Ingress identity is incomplete"), output)
		}
		realized := false
		for _, address := range observed.Status.LoadBalancer.Ingress {
			if strings.TrimSpace(address.Hostname) != "" || strings.TrimSpace(address.IP) != "" {
				realized = true
				break
			}
		}
		if !realized {
			return ingressDiagnostic(ctx, runner, ingress, namespace, "IngressUnrealized", fmt.Errorf("Ingress has no load-balancer address"), output)
		}
	}
	return nil
}

func waitForIngressRealization(
	ctx context.Context, runner CommandRunner, release, namespace string, timeout, pollInterval time.Duration,
) error {
	ingresses, err := deployedIngressReferences(ctx, runner, release, namespace)
	if err != nil || len(ingresses) == 0 {
		return err
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastErr error
	for {
		err := observeIngressReferences(waitCtx, runner, ingresses, namespace)
		if err == nil {
			return nil
		}
		var diagnostic DiagnosticError
		if !errors.As(err, &diagnostic) || len(diagnostic.Resources) != 1 || diagnostic.Resources[0].Reason != "IngressUnrealized" {
			return err
		}
		lastErr = err
		timer := time.NewTimer(pollInterval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return lastErr
		case <-timer.C:
		}
	}
}

func ingressReferences(manifest []byte) ([]ingressReference, error) {
	decoder := yaml.NewDecoder(strings.NewReader(string(manifest)))
	result := make([]ingressReference, 0)
	seen := make(map[string]struct{})
	for {
		var object struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
			Metadata   struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		}
		err := decoder.Decode(&object)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if object.Kind != "Ingress" {
			continue
		}
		if object.APIVersion != "networking.k8s.io/v1" || object.Metadata.Name == "" {
			return nil, fmt.Errorf("Ingress requires networking.k8s.io/v1 and a name")
		}
		if _, exists := seen[object.Metadata.Name]; exists {
			return nil, fmt.Errorf("duplicate Ingress %q", object.Metadata.Name)
		}
		seen[object.Metadata.Name] = struct{}{}
		result = append(result, ingressReference{APIVersion: object.APIVersion, Name: object.Metadata.Name})
	}
	return result, nil
}

func ingressDiagnostic(
	ctx context.Context, runner CommandRunner, ingress ingressReference, namespace, reason string, cause error, output []byte,
) error {
	events, eventErr := runner.Run(ctx, "kubectl", "get", "events", "--namespace", namespace,
		"--field-selector", "involvedObject.kind=Ingress,involvedObject.name="+ingress.Name,
		"--sort-by=.lastTimestamp", "--output", "json")
	message := cause.Error()
	combined := string(output)
	if eventErr == nil {
		combined += "\ncontroller events:\n" + string(events)
	} else {
		message += "; controller events unavailable: " + eventErr.Error()
	}
	return DiagnosticError{Err: fmt.Errorf("Ingress %s/%s is not realized: %s", namespace, ingress.Name, message), Output: combined,
		Resources: []controlplane.ResourceDiagnostic{{APIVersion: ingress.APIVersion, Kind: "Ingress", Namespace: namespace,
			Name: ingress.Name, Reason: reason, Message: message}}}
}
