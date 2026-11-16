package cluster

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type provisionerStub struct {
	err     error
	deletes int
}

func (p *provisionerStub) Create(context.Context, domain.Workspace) error { return p.err }
func (p *provisionerStub) Delete(context.Context, string) error           { p.deletes++; return p.err }
func TestUnavailablePrimaryDoesNotClaimRecordedDeletion(t *testing.T) {
	primary := &provisionerStub{err: domain.ErrNoCluster}
	secondary := &provisionerStub{}
	fallback := Fallback{Primary: primary, Secondary: secondary}
	if err := fallback.Create(context.Background(), domain.Workspace{}); err != nil {
		t.Fatal(err)
	}
	if err := fallback.Delete(context.Background(), "workspace"); !errors.Is(err, domain.ErrNoCluster) {
		t.Fatalf("unavailable cluster reported success: %v", err)
	}
	if secondary.deletes != 0 {
		t.Fatal("recorded fallback cannot prove cluster deletion")
	}
	primary.err = nil
	if err := fallback.Delete(context.Background(), "workspace"); err != nil {
		t.Fatal(err)
	}
	if primary.deletes != 2 || secondary.deletes != 0 {
		t.Fatal("delete authority changed")
	}
}

func TestCreateRequiresOperatorPolicyAndEnforceablePeers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config *domain.EgressConfig
	}{
		{name: "missing"},
		{name: "DNS requires a qualified backend", config: &domain.EgressConfig{GitHost: "git.example.internal"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kubectl, calls := fakeKubectl(t, false)
			p := &Provisioner{kubectl: kubectl, egress: tc.config}
			if tc.config != nil {
				cfg, err := domain.NormalizeAndValidateEgress(*tc.config)
				if err != nil {
					t.Fatal(err)
				}
				p.egress = &cfg
			}
			if err := p.Create(t.Context(), domain.Workspace{ID: "example"}); !errors.Is(err, domain.ErrDisabled) {
				t.Fatalf("unconfigured isolation admitted: %v", err)
			}
			if _, err := os.Stat(calls); !os.IsNotExist(err) {
				t.Fatalf("unconfigured request touched kubectl: %v", err)
			}
		})
	}
}

func TestCreateAppliesPolicyBeforePodAndStopsOnPolicyFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "policy failure"
		}
		t.Run(name, func(t *testing.T) {
			kubectl, calls := fakeKubectl(t, fail)
			p, err := NewWithEgress("example.invalid/workspace@sha256:"+strings.Repeat("1", 64), domain.EgressConfig{GitHost: "10.25.0.1", Registry: "10.25.0.2", RegistryPort: 5000})
			if err != nil {
				t.Fatal(err)
			}
			p.kubectl = kubectl
			err = p.Create(t.Context(), domain.Workspace{ID: "example", Owner: "operator"})
			if fail && err == nil {
				t.Fatal("failed policy apply accepted")
			}
			if !fail && err != nil {
				t.Fatal(err)
			}
			raw, readErr := os.ReadFile(calls)
			if readErr != nil {
				t.Fatal(readErr)
			}
			expected := "probe\npolicy\npod\n"
			if fail {
				expected = "probe\npolicy\n"
			}
			if string(raw) != expected {
				t.Fatalf("unsafe apply order: %q, want %q", raw, expected)
			}
		})
	}
}

func fakeKubectl(t *testing.T, failPolicy bool) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX kubectl fixture")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "kubectl")
	calls := filepath.Join(dir, "calls")
	script := `#!/bin/sh
set -eu
cd -- "$(dirname -- "$0")"
if [ "$1" = get ]; then
 printf 'probe\n' >> calls
 exit 0
fi
[ "$1" = apply ]
body=$(cat)
case "$body" in
 *'"kind":"NetworkPolicy"'*)
  printf 'policy\n' >> calls
  [ ! -e fail-policy ]
  ;;
 *'"kind":"Pod"'*) printf 'pod\n' >> calls ;;
 *) exit 72 ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if failPolicy {
		if err := os.WriteFile(filepath.Join(dir, "fail-policy"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path, calls
}
