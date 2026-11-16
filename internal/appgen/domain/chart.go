package domain

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// ChartFile is one file relative to the chart root.
type ChartFile struct {
	Path string
	Body []byte
}

const deploymentTemplate = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Values.name }}
  labels:
    app.kubernetes.io/name: {{ .Values.name | quote }}
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: {{ .Values.name | quote }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: {{ .Values.name | quote }}
    spec:
      automountServiceAccountToken: false
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        runAsGroup: 65532
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: app
          image: {{ required "image must be a repository@sha256 digest" .Values.image | quote }}
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: {{ .Values.port }}
              protocol: TCP
          env:
            - name: PORT
              value: {{ .Values.port | quote }}
            - name: HOME
              value: /tmp
            - name: PYTHONDONTWRITEBYTECODE
              value: "1"
          securityContext:
            runAsNonRoot: true
            runAsUser: 65532
            runAsGroup: 65532
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
          resources:
            requests:
              cpu: {{ .Values.resources.requests.cpu | quote }}
              memory: {{ .Values.resources.requests.memory | quote }}
            limits:
              cpu: {{ .Values.resources.limits.cpu | quote }}
              memory: {{ .Values.resources.limits.memory | quote }}
          readinessProbe:
            httpGet:
              path: /health
              port: http
            initialDelaySeconds: 1
            periodSeconds: 5
            timeoutSeconds: 2
            failureThreshold: 3
          livenessProbe:
            httpGet:
              path: /health
              port: http
            initialDelaySeconds: 2
            periodSeconds: 10
            timeoutSeconds: 2
            failureThreshold: 3
          volumeMounts:
            - name: tmp
              mountPath: /tmp
      volumes:
        - name: tmp
          emptyDir:
            sizeLimit: 16Mi
`

const serviceTemplate = `apiVersion: v1
kind: Service
metadata:
  name: {{ .Values.name }}
  labels:
    app.kubernetes.io/name: {{ .Values.name | quote }}
spec:
  selector:
    app.kubernetes.io/name: {{ .Values.name | quote }}
  ports:
    - name: http
      port: {{ .Values.port }}
      targetPort: http
      protocol: TCP
`

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ChartFiles returns the chart root. The image value is empty until StampImage.
func ChartFiles(name, version string, values []byte) ([]ChartFile, error) {
	if !namePattern(name) {
		return nil, fmt.Errorf("chart name is not a lowercase identifier")
	}
	if strings.TrimSpace(version) == "" {
		return nil, fmt.Errorf("chart version is required")
	}
	return []ChartFile{
		{Path: "Chart.yaml", Body: []byte(fmt.Sprintf("apiVersion: v2\nname: %s\nversion: %s\ndescription: Generated HTTP application %s\n", name, version, name))},
		{Path: "values.yaml", Body: append([]byte(nil), values...)},
		{Path: "templates/deployment.yaml", Body: []byte(deploymentTemplate)},
		{Path: "templates/service.yaml", Body: []byte(serviceTemplate)},
	}, nil
}

// ImageRef joins a repository and a sha256 digest.
func ImageRef(repository, digest string) (string, error) {
	repository = strings.TrimSpace(repository)
	digest = strings.TrimSpace(digest)
	if repository == "" || strings.ContainsAny(repository, " \t@") {
		return "", fmt.Errorf("image repository is not a name")
	}
	if !digestPattern.MatchString(digest) {
		return "", fmt.Errorf("image digest is not sha256")
	}
	return repository + "@" + digest, nil
}

// ImageRepository strips a tag or digest from an engine reference.
func ImageRepository(ref string) string {
	ref = strings.TrimSpace(ref)
	if i := strings.LastIndex(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	slash := strings.LastIndex(ref, "/")
	tail := ref
	head := ""
	if slash >= 0 {
		head = ref[:slash+1]
		tail = ref[slash+1:]
	}
	if colon := strings.LastIndex(tail, ":"); colon >= 0 {
		tail = tail[:colon]
	}
	return head + tail
}

// StampChartValues writes repository@digest into chart values.
func StampChartValues(values []byte, repository, digest string) ([]byte, error) {
	ref, err := ImageRef(repository, digest)
	if err != nil {
		return nil, err
	}
	replacement := []byte(fmt.Sprintf("image: %q\n", ref))
	if bytes.Contains(values, []byte(imageValueUnset)) {
		return bytes.Replace(values, []byte(imageValueUnset), replacement, 1), nil
	}
	if bytes.Contains(values, []byte("\nimage:")) || bytes.HasPrefix(values, []byte("image:")) {
		return nil, fmt.Errorf("chart values already name an image")
	}
	out := append([]byte(nil), values...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return append(out, replacement...), nil
}

// StampImage pins the built image in the chart values and the package manifest.
func StampImage(files PackageFiles, repository, digest string) (PackageFiles, error) {
	stamped, err := StampChartValues(files.ChartValues, repository, digest)
	if err != nil {
		return PackageFiles{}, err
	}
	ref, err := ImageRef(repository, digest)
	if err != nil {
		return PackageFiles{}, err
	}
	if !bytes.Contains(files.Zarf, []byte(imagePinUnset)) {
		return PackageFiles{}, fmt.Errorf("package manifest has no image pin")
	}
	files.ChartValues = stamped
	files.Zarf = bytes.Replace(files.Zarf, []byte(imagePinUnset), []byte(fmt.Sprintf("    images:\n      - %q\n", ref)), 1)
	chart := make([]ChartFile, len(files.Chart))
	for i, file := range files.Chart {
		chart[i] = file
		if file.Path == "values.yaml" {
			chart[i].Body = append([]byte(nil), stamped...)
		}
	}
	files.Chart = chart
	return files, nil
}
