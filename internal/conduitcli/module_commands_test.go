package conduitcli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// lockService returns one exact lock, so a test can state the Hub's answer and
// nothing else. The embedded fakeService supplies the rest of the interface.
type lockService struct {
	*fakeService
	lock controlplane.ModuleLock
}

func (service lockService) LockModule(context.Context, controlplane.ModuleDefinition) (controlplane.ModuleLock, error) {
	return service.lock, nil
}

func resolvedModuleLock() controlplane.ModuleLock {
	return controlplane.ModuleLock{
		Module:        "foundation",
		Submodules:    []controlplane.LockedModuleReference{{Name: "logging", Version: "1.4.0"}},
		Entities:      []controlplane.LockedModuleEntity{{SourceModule: "foundation", Type: "helmChart", ProductID: "com.example:application", EntityName: "application", KubernetesNamespace: "apps"}},
		ContentDigest: testDigest,
	}
}

func runModuleLock(t *testing.T, service Service, flags ...string) (string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "module.yaml")
	definition := "name: foundation\nentities:\n  - type: helmChart\n    helmChart:\n      productId: com.example:application\n"
	if err := os.WriteFile(path, []byte(definition), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, service))
	root.SetArgs(append([]string{
		"--server", "http://127.0.0.1:8081", "--development-subject", "developer",
		"module", "lock", "--module-definition-file", path,
	}, flags...))
	// Execute first: return operands are evaluated left to right, so reading
	// the buffer in the return statement would read it before the command ran.
	err := root.Execute()
	return output.String(), err
}

// The flag used to be echoed straight into the output, so `--verify` printed
// {"verified":true} and exited 0 for a lock the Hub had never resolved — a CI
// pipeline asserting `.verified == true` passed on an empty document (issue
// #173). Each case below is a lock that is wrong in exactly one way, so a
// check that stops asserting one invariant cannot be covered by another.
func TestModuleLockVerifyRejectsAnUnresolvedLock(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		mutate func(*controlplane.ModuleLock)
		want   string
	}{
		"the zero lock a stub Hub returns": {
			mutate: func(lock *controlplane.ModuleLock) { *lock = controlplane.ModuleLock{} },
			want:   "lock.module",
		},
		"no root Module identity": {
			mutate: func(lock *controlplane.ModuleLock) { lock.Module = "" },
			want:   "lock.module: must name the root Module",
		},
		"no content digest": {
			mutate: func(lock *controlplane.ModuleLock) { lock.ContentDigest = "" },
			want:   "lock.contentDigest: must be sha256:",
		},
		"a content digest that is not a digest": {
			mutate: func(lock *controlplane.ModuleLock) { lock.ContentDigest = "pending" },
			want:   "lock.contentDigest: must be sha256:",
		},
		"no Entities resolved": {
			mutate: func(lock *controlplane.ModuleLock) { lock.Entities = nil },
			want:   "lock.entities: must resolve at least one Entity",
		},
		"an Entity without a Product coordinate": {
			mutate: func(lock *controlplane.ModuleLock) { lock.Entities[0].ProductID = "application" },
			want:   "lock.entities[0].productId: must be a group:name coordinate",
		},
		"an Entity with no name": {
			mutate: func(lock *controlplane.ModuleLock) { lock.Entities[0].EntityName = "" },
			want:   "lock.entities[0].entityName",
		},
		"an Entity attributed to no Module": {
			mutate: func(lock *controlplane.ModuleLock) { lock.Entities[0].SourceModule = "" },
			want:   "lock.entities[0].sourceModule",
		},
		"an Entity with no type": {
			mutate: func(lock *controlplane.ModuleLock) { lock.Entities[0].Type = "" },
			want:   "lock.entities[0].type",
		},
		"a submodule still pinned to \"local\"": {
			mutate: func(lock *controlplane.ModuleLock) { lock.Submodules[0].Version = "local" },
			want:   "lock.submodules[0].version: must be resolved from \"local\"",
		},
		"a submodule with no version at all": {
			mutate: func(lock *controlplane.ModuleLock) { lock.Submodules[0].Version = "" },
			want:   "lock.submodules[0].version: must be pinned to an exact published version",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			lock := resolvedModuleLock()
			test.mutate(&lock)
			output, err := runModuleLock(t, lockService{fakeService: &fakeService{}, lock: lock}, "--verify")
			if err == nil {
				t.Fatalf("--verify accepted an unresolved lock; output=%s", output)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error %q does not report %q", err.Error(), test.want)
			}
			if strings.Contains(output, `"verified"`) {
				t.Fatalf("a failed verification still printed a verdict: %s", output)
			}
		})
	}
}

func TestModuleLockVerifyAcceptsAResolvedLock(t *testing.T) {
	t.Parallel()
	output, err := runModuleLock(t, lockService{fakeService: &fakeService{}, lock: resolvedModuleLock()}, "--verify")
	if err != nil {
		t.Fatalf("--verify rejected a resolved lock: %v (%s)", err, output)
	}
	if !strings.Contains(output, `"verified": true`) || !strings.Contains(output, testDigest) {
		t.Fatalf("output=%s", output)
	}
}

// Without --verify nothing is checked, so the command still prints the lock a
// caller asked for rather than refusing it, and says verified: false.
func TestModuleLockWithoutVerifyPrintsAnUncheckedLock(t *testing.T) {
	t.Parallel()
	output, err := runModuleLock(t, lockService{fakeService: &fakeService{}, lock: controlplane.ModuleLock{}})
	if err != nil {
		t.Fatalf("lock without --verify failed: %v (%s)", err, output)
	}
	if !strings.Contains(output, `"verified": false`) {
		t.Fatalf("output=%s", output)
	}
}
