package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type ingressRunner struct {
	base          *helmRunner
	ingress       string
	ingressQueue  []string
	events        string
	ingressGetErr error
	commands      [][]string
}

func (runner *ingressRunner) Run(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	runner.commands = append(runner.commands, append([]string{name}, arguments...))
	if name == "helm" && len(arguments) >= 2 && arguments[0] == "get" && arguments[1] == "manifest" {
		return []byte("apiVersion: networking.k8s.io/v1\nkind: Ingress\nmetadata:\n  name: public-origin\n"), nil
	}
	if name == "kubectl" && len(arguments) >= 2 && arguments[1] == "ingress" {
		if len(runner.ingressQueue) > 0 {
			result := runner.ingressQueue[0]
			runner.ingressQueue = runner.ingressQueue[1:]
			return []byte(result), nil
		}
		return []byte(runner.ingress), runner.ingressGetErr
	}
	if name == "kubectl" && len(arguments) >= 2 && arguments[1] == "events" {
		return []byte(runner.events), nil
	}
	return runner.base.Run(ctx, name, arguments...)
}

func TestHelmObserveRequiresIngressExternalRealization(t *testing.T) {
	step := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Product: "app:foundation", Version: "1.2.3",
		ReleaseDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Namespace:     "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart: controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}

	t.Run("realized address permits exact reported state", func(t *testing.T) {
		base := &helmRunner{step: step, installed: true}
		runner := &ingressRunner{base: base, ingress: `{"metadata":{"uid":"uid-1","generation":3},"status":{"loadBalancer":{"ingress":[{"hostname":"internal.example"}]}}}`}
		report, err := (HelmExecutor{Runner: runner}).Observe(context.Background(), step)
		if err != nil {
			t.Fatal(err)
		}
		if report.Health != controlplane.HealthHealthy || report.ReleaseDigest != step.ReleaseDigest {
			t.Fatalf("report = %#v", report)
		}
	})

	t.Run("missing address fails with bounded controller evidence", func(t *testing.T) {
		base := &helmRunner{step: step, installed: true}
		runner := &ingressRunner{base: base,
			ingress: `{"metadata":{"uid":"uid-1","generation":3},"status":{"loadBalancer":{}}}`,
			events:  `{"items":[{"reason":"FailedDeployModel","message":"UnauthorizedOperation: ec2:CreateSecurityGroup"}]}`,
		}
		_, err := (HelmExecutor{Runner: runner, Timeout: time.Nanosecond}).Observe(context.Background(), step)
		if err == nil || !strings.Contains(err.Error(), "no load-balancer address") {
			t.Fatalf("Observe error = %v", err)
		}
		var diagnostic DiagnosticError
		if !errors.As(err, &diagnostic) || len(diagnostic.Resources) != 1 ||
			diagnostic.Resources[0].Reason != "IngressUnrealized" ||
			!strings.Contains(diagnostic.Output, "ec2:CreateSecurityGroup") {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
		commands := fmt.Sprint(runner.commands)
		if !strings.Contains(commands, "involvedObject.kind=Ingress,involvedObject.name=public-origin") {
			t.Fatalf("controller event query is not object-scoped: %s", commands)
		}
	})

	t.Run("asynchronous controller gets the bounded realization window", func(t *testing.T) {
		base := &helmRunner{step: step, installed: true}
		runner := &ingressRunner{base: base, ingressQueue: []string{
			`{"metadata":{"uid":"uid-1","generation":3},"status":{"loadBalancer":{}}}`,
			`{"metadata":{"uid":"uid-1","generation":3},"status":{"loadBalancer":{"ingress":[{"ip":"192.0.2.1"}]}}}`,
		}}
		if _, err := (HelmExecutor{Runner: runner, Timeout: time.Second, IngressPollInterval: time.Nanosecond}).Observe(context.Background(), step); err != nil {
			t.Fatal(err)
		}
		manifestReads := 0
		for _, command := range runner.commands {
			if len(command) >= 3 && command[0] == "helm" && command[1] == "get" && command[2] == "manifest" {
				manifestReads++
			}
		}
		if manifestReads != 1 {
			t.Fatalf("deployed manifest reads = %d, want 1", manifestReads)
		}
	})
}

func TestIngressReferencesRejectAmbiguousOrLegacyObjects(t *testing.T) {
	for name, manifest := range map[string]string{
		"legacy":    "apiVersion: extensions/v1beta1\nkind: Ingress\nmetadata:\n  name: old\n",
		"unnamed":   "apiVersion: networking.k8s.io/v1\nkind: Ingress\nmetadata: {}\n",
		"duplicate": "apiVersion: networking.k8s.io/v1\nkind: Ingress\nmetadata:\n  name: same\n---\napiVersion: networking.k8s.io/v1\nkind: Ingress\nmetadata:\n  name: same\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ingressReferences([]byte(manifest)); err == nil {
				t.Fatal("accepted ambiguous or unsupported Ingress")
			}
		})
	}
}

func TestHelmObserveRejectsIncompleteIngressIdentity(t *testing.T) {
	step := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Product: "app:foundation", Version: "1.2.3",
		ReleaseDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Namespace:     "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart: controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	for name, observed := range map[string]string{
		"missing uid":     `{"metadata":{"generation":1},"status":{"loadBalancer":{"ingress":[{"ip":"192.0.2.1"}]}}}`,
		"zero generation": `{"metadata":{"uid":"uid-1","generation":0},"status":{"loadBalancer":{"ingress":[{"ip":"192.0.2.1"}]}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			base := &helmRunner{step: step, installed: true}
			runner := &ingressRunner{base: base, ingress: observed}
			_, err := (HelmExecutor{Runner: runner}).Observe(context.Background(), step)
			var diagnostic DiagnosticError
			if !errors.As(err, &diagnostic) || len(diagnostic.Resources) != 1 || diagnostic.Resources[0].Reason != "IngressIdentityMissing" {
				t.Fatalf("Observe error = %#v", err)
			}
		})
	}
}
