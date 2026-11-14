package packagingtools

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

const (
	ruleDockerfileDigest   = "dockerfile-digest"
	ruleDockerfileUser     = "dockerfile-user"
	ruleDockerfileSecrets  = "dockerfile-secrets"
	ruleDockerfilePort     = "dockerfile-port"
	ruleDockerfileIgnore   = "dockerfile-ignore"
	ruleDockerfileStages   = "dockerfile-stages"
	ruleDockerfileRuntime  = "dockerfile-runtime"
	reasonDockerfileDigest = "dockerfile: base image must be pinned by a sha256 digest"
	reasonDockerfileUser   = "dockerfile: final stage must run as a non-root user"
	reasonDockerfileSecret = "dockerfile: must not include secrets"
	reasonDockerfilePort   = "dockerfile: must listen on a high port"
	reasonDockerfileIgnore = "dockerfile: a dockerignore is required"
)

var (
	digestPin      = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)
	secretName     = regexp.MustCompile(`(?i)(password|secret|token|api[_-]?key|private[_-]?key)`)
	secretFile     = regexp.MustCompile(`(?i)(^|/)(\.env(\..+)?|id_rsa|id_ed25519|id_dsa|id_ecdsa|.*\.(pem|p12|pfx|key)|secrets?(s)?(\..+)?|credentials(\..+)?)$`)
	compilerTokens = regexp.MustCompile(`(?i)\b(golang|gcc|g\+\+|clang|rustc|cargo|maven|jdk|openjdk|build-essential|npm ci|npm install|yarn install|pnpm install|pip install|go build)\b`)
)

type dockerInstruction struct {
	cmd  string
	args string
	line int
}

func (service *Service) ValidateDockerfile(_ context.Context, dockerfile, dockerignore string) (domain.ManifestReport, error) {
	return validateDockerfile(dockerfile, dockerignore), nil
}

func validateDockerfile(dockerfile, dockerignore string) domain.ManifestReport {
	if strings.TrimSpace(dockerfile) == "" {
		return report(domain.ManifestFinding{Path: "Dockerfile", Rule: ruleDockerfileDigest, Severity: sevError, Message: "dockerfile is empty"})
	}
	instructions := parseDockerfile(dockerfile)
	var findings []domain.ManifestFinding
	fromCount := 0
	var lastFrom int
	for _, inst := range instructions {
		if inst.cmd != "FROM" {
			continue
		}
		fromCount++
		lastFrom = inst.line
		image := fromImage(inst.args)
		if strings.EqualFold(image, "scratch") {
			continue
		}
		if !digestPin.MatchString(image) {
			findings = append(findings, domain.ManifestFinding{
				Path: fmt.Sprintf("Dockerfile:%d", inst.line), Rule: ruleDockerfileDigest,
				Severity: sevError, Message: reasonDockerfileDigest,
			})
		}
	}
	if fromCount == 0 {
		findings = append(findings, domain.ManifestFinding{
			Path: "Dockerfile", Rule: ruleDockerfileDigest, Severity: sevError, Message: reasonDockerfileDigest,
		})
	}
	if fromCount == 1 {
		findings = append(findings, domain.ManifestFinding{
			Path: "Dockerfile", Rule: ruleDockerfileStages, Severity: sevWarning, Message: "prefer a multi-stage build",
		})
	}
	user := finalUser(instructions, lastFrom)
	if user == "" || isRootUser(user) {
		findings = append(findings, domain.ManifestFinding{
			Path: "Dockerfile", Rule: ruleDockerfileUser, Severity: sevError, Message: reasonDockerfileUser,
		})
	}
	for _, inst := range instructions {
		if secretFinding, ok := dockerfileSecret(inst); ok {
			findings = append(findings, secretFinding)
		}
	}
	for _, inst := range instructions {
		if inst.cmd != "EXPOSE" {
			continue
		}
		for _, field := range strings.Fields(inst.args) {
			field = strings.SplitN(field, "/", 2)[0]
			port, err := strconv.Atoi(field)
			if err == nil && port > 0 && port < 1024 {
				findings = append(findings, domain.ManifestFinding{
					Path: fmt.Sprintf("Dockerfile:%d", inst.line), Rule: ruleDockerfilePort,
					Severity: sevError, Message: reasonDockerfilePort,
				})
				break
			}
		}
	}
	if finalStageHasCompiler(instructions, lastFrom) {
		findings = append(findings, domain.ManifestFinding{
			Path: "Dockerfile", Rule: ruleDockerfileRuntime, Severity: sevWarning, Message: "keep the final stage free of compilers and package installs",
		})
	}
	if strings.TrimSpace(dockerignore) == "" {
		findings = append(findings, domain.ManifestFinding{
			Path: ".dockerignore", Rule: ruleDockerfileIgnore, Severity: sevError, Message: reasonDockerfileIgnore,
		})
	}
	return report(findings...)
}

func dockerfileGateReasons(dockerfile, dockerignore string) []string {
	if strings.TrimSpace(dockerfile) == "" {
		return nil
	}
	result := validateDockerfile(dockerfile, dockerignore)
	var reasons []string
	seen := map[string]bool{}
	for _, finding := range result.Findings {
		if finding.Severity != sevError {
			continue
		}
		if seen[finding.Message] {
			continue
		}
		seen[finding.Message] = true
		reasons = append(reasons, finding.Message)
	}
	return reasons
}

func parseDockerfile(text string) []dockerInstruction {
	var (
		out      []dockerInstruction
		pending  string
		start    int
		physical int
	)
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		physical++
		line := strings.TrimSpace(raw)
		if pending != "" {
			line = pending + line
			pending = ""
		} else {
			start = physical
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, "\\") {
			pending = strings.TrimSpace(strings.TrimSuffix(line, "\\")) + " "
			continue
		}
		cmd, args, ok := splitInstruction(line)
		if !ok {
			continue
		}
		out = append(out, dockerInstruction{cmd: cmd, args: args, line: start})
	}
	return out
}

func splitInstruction(line string) (string, string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", "", false
	}
	cmd := strings.ToUpper(fields[0])
	args := strings.TrimSpace(line[len(fields[0]):])
	return cmd, args, true
}

func fromImage(args string) string {
	fields := strings.Fields(args)
	var image string
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if strings.HasPrefix(field, "--") {
			if !strings.Contains(field, "=") && i+1 < len(fields) {
				i++
			}
			continue
		}
		image = field
		break
	}
	if image == "" {
		return ""
	}
	if at := strings.Index(strings.ToLower(image), " as "); at >= 0 {
		image = strings.TrimSpace(image[:at])
	}
	return image
}

func finalUser(instructions []dockerInstruction, lastFrom int) string {
	user := ""
	for _, inst := range instructions {
		if inst.line < lastFrom {
			continue
		}
		if inst.cmd == "USER" {
			user = strings.Fields(inst.args)[0]
			if len(strings.Fields(inst.args)) > 0 {
				user = strings.Fields(inst.args)[0]
			}
		}
	}
	return user
}

func isRootUser(user string) bool {
	user = strings.TrimSpace(user)
	name, _, _ := strings.Cut(user, ":")
	name = strings.TrimSpace(name)
	return name == "" || name == "0" || strings.EqualFold(name, "root")
}

func dockerfileSecret(inst dockerInstruction) (domain.ManifestFinding, bool) {
	finding := domain.ManifestFinding{
		Path: fmt.Sprintf("Dockerfile:%d", inst.line), Rule: ruleDockerfileSecrets,
		Severity: sevError, Message: reasonDockerfileSecret,
	}
	switch inst.cmd {
	case "COPY", "ADD":
		sources := copySources(inst.args)
		for _, source := range sources {
			if secretFile.MatchString(filepathToSlash(source)) {
				return finding, true
			}
		}
	case "ENV", "ARG":
		for _, assign := range splitKV(inst.args) {
			name, value, ok := strings.Cut(assign, "=")
			if !ok {
				continue
			}
			if secretName.MatchString(strings.TrimSpace(name)) && strings.TrimSpace(value) != "" {
				return finding, true
			}
		}
	}
	return domain.ManifestFinding{}, false
}

func copySources(args string) []string {
	fields := strings.Fields(args)
	var sources []string
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if strings.HasPrefix(field, "--") {
			if !strings.Contains(field, "=") && i+1 < len(fields) {
				i++
			}
			continue
		}
		sources = append(sources, field)
	}
	if len(sources) <= 1 {
		return sources
	}
	return sources[:len(sources)-1]
}

func splitKV(args string) []string {
	args = strings.TrimSpace(args)
	if strings.Contains(args, "=") && !strings.Contains(args, " ") {
		return []string{args}
	}
	parts := strings.Fields(args)
	if len(parts) == 2 && !strings.Contains(parts[0], "=") {
		return []string{parts[0] + "=" + parts[1]}
	}
	return parts
}

func finalStageHasCompiler(instructions []dockerInstruction, lastFrom int) bool {
	for _, inst := range instructions {
		if inst.line < lastFrom {
			continue
		}
		if inst.cmd == "RUN" && compilerTokens.MatchString(inst.args) {
			return true
		}
	}
	return false
}

func filepathToSlash(path string) string {
	return strings.ReplaceAll(path, `\`, "/")
}

func firstDockerfileReason(dockerfile, dockerignore string) string {
	reasons := dockerfileGateReasons(dockerfile, dockerignore)
	if len(reasons) == 0 {
		return ""
	}
	return reasons[0]
}
