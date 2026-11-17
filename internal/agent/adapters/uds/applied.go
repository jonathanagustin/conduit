package uds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// The applied record.
//
// Zarf's deployed-package record says which package is deployed and at which
// generation. It does not say which variables the deployment was given, which
// Release the Hub was deploying, or -- for a bundle -- which bundle the package
// came from. Without those the executor could only echo the Plan back, so it
// reported the configuration as INDETERMINATE and no Plan could succeed.
//
// So the executor writes down what it did. After a deploy has succeeded and
// Zarf's own record has been read back, it stores an appliedRecord in a
// ConfigMap named conduit-applied-<zarf package name> in the step's namespace.
// Observe reads that ConfigMap; it never fills these fields from the step.
//
// What a report built from the record means, exactly:
//
//	"Conduit deployed this Release with these values, that deploy succeeded,
//	and the Zarf package it produced has not been deployed again since."
//
// The last clause is checked, not assumed: the record stores the digest and
// generation Zarf recorded for that deploy, and Zarf adds one to the generation
// on every deploy (src/pkg/packager/deploy.go, v0.82.0). A later deploy by
// anyone -- another tool, an operator, a rollback -- leaves a record that no
// longer matches, and the configuration is reported INDETERMINATE again.
//
// What it does not mean:
//
//   - It is not a read of live cluster configuration. A `kubectl edit` or a
//     `helm upgrade` against what the package installed changes neither Zarf's
//     generation nor this record, so APPLIED does not detect that drift.
//   - It is not tamper-evident. Whoever can write ConfigMaps in the namespace
//     can write this one. It is a record of the Spoke's own action, in the
//     same trust position as the identity the Helm executor stores in a
//     release description.
//   - For a bundle it does not prove the other packages of the bundle are
//     unchanged: only the one named package is observed.
//
// CONDUIT-INVENTED: agent.AppliedPackageRecord
// Displaced-by: a Zarf or UDS release that records the variables and source reference a deployment was given in its own deployed-package state, or a published contract for reading them back.
type appliedRecord struct {
	InstallationID string `json:"installationId"`
	ReleaseDigest  string `json:"releaseDigest"`
	Version        string `json:"version"`
	ConfigDigest   string `json:"configDigest"`
	// ValuesDigest is the digest of the exact KEY=value arguments passed to
	// the CLI, in the order they were passed.
	ValuesDigest   string `json:"valuesDigest"`
	ArtifactKind   string `json:"artifactKind"`
	ArtifactDigest string `json:"artifactDigest"`
	// ZarfDigest and ZarfGeneration are what Zarf recorded for the deploy this
	// record describes. They bind the record to that one deployment.
	ZarfPackage    string `json:"zarfPackage"`
	ZarfDigest     string `json:"zarfDigest"`
	ZarfGeneration int    `json:"zarfGeneration"`
}

const (
	appliedRecordPrefix = "conduit-applied-"
	appliedRecordKey    = "record"
)

func appliedRecordName(zarfPackage string) string { return appliedRecordPrefix + zarfPackage }

// describes reports whether the record is about the deployment Zarf currently
// holds. A record that fails this is evidence of nothing current.
func (record appliedRecord) describes(name string, deployed *deployedPackage) bool {
	return deployed != nil && record.ZarfPackage == name &&
		record.ZarfDigest != "" && record.ZarfDigest == strings.TrimSpace(deployed.Digest) &&
		record.ZarfGeneration > 0 && record.ZarfGeneration == deployed.Generation
}

// valuePairs is the KEY=value arguments the step's values become, in the order
// they are passed. It is the same mapping the deploy command uses.
func valuePairs(step controlplane.PlanStep) ([]string, error) {
	flag, name := "--set-variables", zarfVariableName
	if isBundle(step) {
		flag, name = "--set", bundleVariableName
	}
	flags, err := setFlags(step, flag, name)
	if err != nil {
		return nil, err
	}
	pairs := make([]string, 0, len(flags)/2)
	for index := 1; index < len(flags); index += 2 {
		pairs = append(pairs, flags[index])
	}
	return pairs, nil
}

func valuesDigest(pairs []string) string {
	sum := sha256.Sum256([]byte(strings.Join(pairs, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// writeApplied stores the record for a deploy that has just succeeded. It
// replaces any earlier record: delete then create, because the runner has no
// standard input to feed `kubectl apply`. A reader between the two sees no
// record, which reports INDETERMINATE -- the safe direction.
func (e Executor) writeApplied(ctx context.Context, namespace string, record appliedRecord) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode applied record: %w", err)
	}
	name := appliedRecordName(record.ZarfPackage)
	if err := e.deleteApplied(ctx, namespace, record.ZarfPackage); err != nil {
		return err
	}
	out, err := e.runner().Run(ctx, e.kubectlBin(), "create", "configmap", name, "-n", namespace,
		"--from-literal="+appliedRecordKey+"="+string(encoded))
	if err != nil {
		return agent.DiagnosticError{Err: fmt.Errorf("write applied record %s/%s: %w", namespace, name, err), Output: strings.TrimSpace(string(out))}
	}
	return nil
}

func (e Executor) deleteApplied(ctx context.Context, namespace, zarfPackage string) error {
	name := appliedRecordName(zarfPackage)
	out, err := e.runner().Run(ctx, e.kubectlBin(), "delete", "configmap", name, "-n", namespace, "--ignore-not-found")
	if err != nil {
		return agent.DiagnosticError{Err: fmt.Errorf("clear applied record %s/%s: %w", namespace, name, err), Output: strings.TrimSpace(string(out))}
	}
	return nil
}

// readApplied returns the stored record, or nil when there is none. A record
// that does not parse is an error, not an absence: something wrote it.
func (e Executor) readApplied(ctx context.Context, namespace, zarfPackage string) (*appliedRecord, error) {
	name := appliedRecordName(zarfPackage)
	out, err := e.runner().Run(ctx, e.kubectlBin(), "get", "configmap", name, "-n", namespace,
		"--ignore-not-found", "-o", "jsonpath={.data."+appliedRecordKey+"}")
	if err != nil {
		return nil, agent.DiagnosticError{Err: fmt.Errorf("read applied record %s/%s: %w", namespace, name, err), Output: strings.TrimSpace(string(out))}
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	var record appliedRecord
	if err := json.Unmarshal([]byte(trimmed), &record); err != nil {
		return nil, fmt.Errorf("parse applied record %s/%s: %w", namespace, name, err)
	}
	return &record, nil
}
