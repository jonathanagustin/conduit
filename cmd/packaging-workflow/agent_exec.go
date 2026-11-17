package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/repoclone"
)

// agentExecModelName is written into the deterministic agent's replies. It is
// model-authored text: the planner ignores it and binds the operator's
// CONDUIT_MODEL_NAME or the unverified marker instead.
const agentExecModelName = "conduit-packaging-agent-v1"

func addAgentExecCommand(root *cobra.Command) {
	var provider string
	cmd := &cobra.Command{
		Use:   "agent-exec",
		Short: "Run one deterministic offline packaging fixture turn",
		Long:  "Reads a prompt on stdin and writes a deterministic fixture reply. Host model delegation is unsupported. Configure the Hub or worker with an HTTP model endpoint for model calls.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if provider != "" && provider != providerDeterministic {
				return localcmd.ErrLocalCommandUnsupported
			}
			raw, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return err
			}
			var reply string
			if provider == "" || provider == providerDeterministic {
				reply, err = runAgentExecPrompt(string(raw))
			} else {
				reply, err = runProviderPrompt(cmd.Context(), provider, string(raw))
			}
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), reply)
			return err
		},
	}
	cmd.Flags().StringVar(&provider, "provider", os.Getenv("CONDUIT_AGENT_PROVIDER"), "deterministic only; host provider delegation is unsupported")
	root.AddCommand(cmd)
}

func runAgentExecPrompt(prompt string) (string, error) {
	switch {
	case strings.Contains(prompt, "Inspect the bound source repository for this planning session."):
		return agentExecInspectPhase(prompt)
	case strings.Contains(prompt, "You are preparing to package a source repository. Decide what you must ask the operator"):
		return agentExecQuestionsPhase(prompt)
	case strings.Contains(prompt, "Write the packaging plan for this source repository."):
		return agentExecPlanPhase(prompt)
	case strings.Contains(prompt, `reply with {"source":"...","zarf":"...","cr":"..."`):
		return agentExecPackageProposal(prompt)
	case strings.Contains(prompt, `reply with {"dockerfile":"...","dockerignore":"..."`):
		return agentExecDockerfileProposal(prompt)
	case strings.Contains(prompt, `field must be "memory"`):
		return marshalCompact(map[string]any{
			"field":     "memory",
			"value":     "512Mi",
			"rationale": "Right-size container memory within the 1Gi governance ceiling.",
		})
	default:
		return agentExecInspectPhase(prompt)
	}
}

func agentExecInspectPhase(prompt string) (string, error) {
	if !strings.Contains(prompt, "Tool results:\n") || !strings.Contains(prompt, "inspect_repo:") {
		return marshalCompact(map[string]any{
			"toolCalls": []map[string]any{
				{"name": "inspect_repo", "arguments": map[string]any{}},
			},
		})
	}
	inspected := extractInspectRepoFromPrompt(prompt)
	if !strings.Contains(prompt, "read_file:") && len(inspected.Tree) > 0 {
		target := chooseKeyFile(inspected.Tree)
		return marshalCompact(map[string]any{
			"toolCalls": []map[string]any{
				{"name": "read_file", "arguments": map[string]any{"path": target}},
			},
		})
	}
	lang := inspected.Language
	if lang == "" {
		lang = "typescript"
	}
	pm := inspected.PackageManager
	if pm == "" {
		pm = "yarn"
	}
	kind := inspected.Kind
	if kind == "" {
		kind = "service"
	}
	startCmd := inspected.StartCommand
	if startCmd == "" {
		startCmd = "yarn start"
	}
	ports := inspected.Ports
	if len(ports) == 0 {
		ports = []int{8080}
	}
	summary := fmt.Sprintf(
		"Inspected repository (%s / %s, kind=%s, start=%q, ports=%v, hasDockerfile=%t, %d files).\n%s",
		lang, pm, kind, startCmd, ports, inspected.HasDockerfile, len(inspected.Tree),
		mustMarshalString(map[string]any{"model": agentExecModelName}),
	)
	return summary, nil
}

func agentExecQuestionsPhase(prompt string) (string, error) {
	if !strings.Contains(prompt, "read_file:") {
		target := chooseKeyFile(extractTreeLinesFromPrompt(prompt))
		return marshalCompact(map[string]any{
			"toolCalls": []map[string]any{
				{"name": "read_file", "arguments": map[string]any{"path": target}},
			},
		})
	}
	return marshalCompact(map[string]any{
		"model": agentExecModelName,
		"questions": []map[string]any{
			{
				"id":       "node_version",
				"prompt":   "Which Node.js runtime version should build and serve this package?",
				"choices":  []string{"18", "20"},
				"required": true,
			},
		},
	})
}

func agentExecPlanPhase(prompt string) (string, error) {
	if !strings.Contains(prompt, "read_file:") {
		target := chooseKeyFile(extractTreeLinesFromPrompt(prompt))
		return marshalCompact(map[string]any{
			"toolCalls": []map[string]any{
				{"name": "read_file", "arguments": map[string]any{"path": target}},
			},
		})
	}
	lower := strings.ToLower(prompt)
	isExcalidraw := strings.Contains(lower, "excalidraw")
	nodeVer := "20"
	if strings.Contains(prompt, ": 18") {
		nodeVer = "18"
	}
	appLabel := "Node web service"
	buildSteps := []string{
		"npm ci",
		"npm run build",
	}
	networkAllow := []string{"registry.npmjs.org:443"}
	if isExcalidraw {
		appLabel = "Excalidraw collaborative whiteboard web application"
		buildSteps = []string{
			"yarn install --frozen-lockfile",
			"yarn build:app:docker",
		}
		networkAllow = []string{"registry.npmjs.org:443", "registry.yarnpkg.com:443"}
	}
	return marshalCompact(map[string]any{
		"model":        agentExecModelName,
		"summary":      fmt.Sprintf("%s packaged for airgap delivery with Zarf and Defense Unicorns UDS on Node %s.", appLabel, nodeVer),
		"baseImage":    fmt.Sprintf("cgr.dev/chainguard/node:%s-dev@sha256:a7e8f028496a951ef5f97fd5de75d98ef5a74383148a346e6858264de0b211a8", nodeVer),
		"buildSteps":   buildSteps,
		"ports":        []int{8080},
		"networkAllow": networkAllow,
		"sso": map[string]any{
			"required": true,
			"notes":    "Keycloak OIDC SSO via Defense Unicorns UDS Package expose rules",
		},
		"risks": []string{
			"Static asset bundle requires non-root HTTP listener on port 8080",
		},
	})
}

func agentExecPackageProposal(prompt string) (string, error) {
	appName := "sample-app"
	if strings.Contains(strings.ToLower(prompt), "excalidraw") {
		appName = "excalidraw"
	}
	writer := fixturePackageWriter{repoURL: appName, planText: prompt}
	proposal, _, err := writer.ProposePackage(context.Background(), domain.PackagingContext{})
	if err != nil {
		return "", err
	}
	return marshalCompact(proposal)
}

func agentExecDockerfileProposal(_ string) (string, error) {
	return marshalCompact(domain.DockerfileProposal{
		Dockerfile: strings.Join([]string{
			"FROM cgr.dev/chainguard/node@sha256:a7e8f028496a951ef5f97fd5de75d98ef5a74383148a346e6858264de0b211a8",
			"WORKDIR /app",
			"COPY package.json ./",
			"USER 65532:65532",
			"EXPOSE 8080",
			`CMD ["node", "server.js"]`,
		}, "\n") + "\n",
		Dockerignore: ".git\nnode_modules\n.env\n",
		Rationale:    "Multi-stage Chainguard digest-pinned Dockerfile running as non-root 65532:65532 on port 8080.",
	})
}

func extractInspectRepoFromPrompt(prompt string) domain.RepoInspect {
	var out domain.RepoInspect
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inspect_repo: ") {
			raw := strings.TrimPrefix(line, "inspect_repo: ")
			_ = json.Unmarshal([]byte(raw), &out)
		}
	}
	return out
}

func extractTreeLinesFromPrompt(prompt string) []string {
	if inspected := extractInspectRepoFromPrompt(prompt); len(inspected.Tree) > 0 {
		return inspected.Tree
	}
	idx := strings.Index(prompt, "\nTree:\n")
	if idx < 0 {
		return []string{"package.json"}
	}
	rest := prompt[idx+len("\nTree:\n"):]
	var files []string
	for _, line := range strings.Split(rest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Before you answer") || strings.HasPrefix(line, "You may call tools") || strings.HasPrefix(line, "Operator answers") {
			break
		}
		files = append(files, line)
	}
	if len(files) == 0 {
		return []string{"package.json"}
	}
	return files
}

func chooseKeyFile(tree []string) string {
	preferred := []string{"package.json", "Dockerfile", "README.md", "go.mod", "pyproject.toml"}
	for _, want := range preferred {
		for _, item := range tree {
			if item == want {
				return item
			}
		}
	}
	if len(tree) > 0 {
		return tree[0]
	}
	return "package.json"
}

func marshalCompact(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func mustMarshalString(v any) string {
	s, _ := marshalCompact(v)
	return s
}

func workspaceAwareClone(_ string) plannerCloneFunc {
	return repoclone.Clone
}

type plannerCloneFunc = func(ctx context.Context, repoURL, ref string) (repoclone.Checkout, func(), error)
