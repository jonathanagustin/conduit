package packagingtools

import (
	"context"
	"strings"
	"testing"
)

const goodManifest = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: static-web
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: static-web
  template:
    metadata:
      labels:
        app.kubernetes.io/name: static-web
    spec:
      securityContext:
        runAsNonRoot: true
      containers:
        - name: web
          image: registry.example/apps/static-web:1.0.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
            limits:
              cpu: 200m
              memory: 128Mi
          readinessProbe:
            httpGet:
              path: /
              port: http
          livenessProbe:
            httpGet:
              path: /
              port: http
          securityContext:
            runAsNonRoot: true
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
---
apiVersion: v1
kind: Service
metadata:
  name: static-web
spec:
  ports:
    - port: 8080
`

func TestValidateManifestsAcceptsTheSkillExample(t *testing.T) {
	report, err := New().ValidateManifests(context.Background(), goodManifest)
	if err != nil || !report.OK {
		t.Fatalf("report = %#v err=%v", report, err)
	}
}

func TestValidateManifestsRejectsAnUnpinnedImageAndMissingProbes(t *testing.T) {
	bad := strings.ReplaceAll(goodManifest, "@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "")
	bad = strings.ReplaceAll(bad, "readinessProbe:", "unused:")
	report, err := New().ValidateManifests(context.Background(), bad)
	if err != nil || report.OK {
		t.Fatalf("expected errors, got %#v %v", report, err)
	}
	var rules []string
	for _, finding := range report.Findings {
		if finding.Severity == "error" {
			rules = append(rules, finding.Rule)
		}
	}
	joined := strings.Join(rules, ",")
	if !strings.Contains(joined, "digest-pin") || !strings.Contains(joined, "probes") {
		t.Fatalf("rules = %s", joined)
	}
}
