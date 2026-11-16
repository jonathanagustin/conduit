// Package conduitcli is the conduit command tree: the operator-facing client
// for the delivery Hub. It owns argument parsing, profile and connection
// configuration, and output formatting. Governed mutations call the Hub's
// public API; explicitly local tools keep their output separate from Hub evidence.
package conduitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	override "github.com/jonathanagustin/conduit/internal/installoverride/domain"
	"github.com/jonathanagustin/conduit/internal/installoverride/ports"
	"io"
	"os"
	"strings"

	"github.com/jonathanagustin/conduit/internal/buildinfo"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"
)

// Service is the semantic application boundary used by CLI commands. It keeps
// Cobra and transport concerns outside the control-plane domain.
type Service interface {
	ListArtifactRegistries(context.Context) ([]controlplane.ArtifactRegistryRecord, error)
	InstallModule(context.Context, string, string, string, string, map[string]string) (controlplane.ModuleInstallation, error)
	PublishProductRelease(context.Context, controlplane.HelmProductReleaseBundle) (operator.ProductReleasePublication, error)
	PublishTerraformProductRelease(context.Context, controlplane.TerraformProductManifest) (controlplane.ProductPublication, error)
	ListProductPublications(context.Context, string) ([]controlplane.ProductPublication, error)
	ListEnvironments(context.Context) ([]controlplane.EnvironmentCatalogRecord, error)
	GetEnvironment(context.Context, string) (controlplane.EnvironmentCatalogRecord, error)
	ListEntities(context.Context, string) ([]controlplane.Entity, error)
	ListProducts(context.Context) ([]controlplane.ProductCatalogRecord, error)
	ListProductReleases(context.Context, string) ([]controlplane.Release, error)
	GetProductRelease(context.Context, string, string) (controlplane.Release, error)
	ListModules(context.Context) ([]controlplane.ModuleRelease, error)
	GetEnvironmentProfile(context.Context, string) (controlplane.EnvironmentProfile, error)
	PutEnvironmentProfile(context.Context, string, controlplane.EnvironmentProfile) (operator.EnvironmentProfileChangeResult, error)
	GetEnvironmentVersions(context.Context, string) ([]controlplane.TargetHubEntityVersion, error)
	PublishModules(context.Context, []controlplane.ModuleDefinition) ([]controlplane.ModuleRelease, error)
	LockModule(context.Context, controlplane.ModuleDefinition) (controlplane.ModuleLock, error)
	ScanRelease(context.Context, controlplane.Release) (controlplane.ArtifactScanResult, error)
	IssueEntityCommand(context.Context, controlplane.IssueCommandRequest) (controlplane.PlanRecord, error)
	UploadArtifact(context.Context, string, io.Reader) (controlplane.BlobDescriptor, error)
	ListChangeRequests(context.Context, string) ([]controlplane.ChangeRequest, error)
	GetChangeRequest(context.Context, string) (controlplane.ChangeRequest, error)
	ApproveChangeRequest(context.Context, string, string) (operator.ChangeRequestActionResult, error)
	RejectChangeRequest(context.Context, string, string) (operator.ChangeRequestActionResult, error)
	ReopenChangeRequest(context.Context, string) (operator.ChangeRequestActionResult, error)
	CancelChangeRequest(context.Context, string, string) (operator.ChangeRequestActionResult, error)
	PutTeam(context.Context, string, controlplane.Team) (controlplane.Team, error)
	CreateResourceGrant(context.Context, controlplane.ResourceGrant) (controlplane.ResourceGrant, error)
	PutApprovalPolicy(context.Context, string, controlplane.ApprovalPolicy) (controlplane.ApprovalPolicy, error)
	ProposePlan(context.Context, controlplane.DesiredState) (controlplane.PlanRecord, error)
	ApprovePlan(context.Context, string, string) (controlplane.PlanRecord, error)
	CancelPlan(context.Context, string, string) (controlplane.PlanRecord, error)
	GetPlan(context.Context, string) (controlplane.PlanRecord, error)
	ListPlans(context.Context, operator.PlanListQuery) (operator.PlanListResult, error)
	ListPackagingRuns(context.Context, operator.PackagingRunQuery) (operator.PackagingRunList, error)
	GetReportedState(context.Context, string) (controlplane.EnvironmentReport, error)
	ListSuppressions(context.Context, string) ([]controlplane.SuppressionRecord, error)
	RemoveSuppression(context.Context, string) (controlplane.SuppressionRecord, error)
}

type serviceFactory func(context.Context, ConnectionConfig) (Service, func(), error)

type Runtime struct {
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	Getenv         func(string) string
	ServiceFactory serviceFactory
}

type commandState struct {
	runtime Runtime
	config  ConnectionConfig
	output  string
	profile string
}

func NewRoot(runtime Runtime) *cobra.Command {
	if runtime.Stdin == nil {
		runtime.Stdin = os.Stdin
	}
	if runtime.Stdout == nil {
		runtime.Stdout = os.Stdout
	}
	if runtime.Stderr == nil {
		runtime.Stderr = os.Stderr
	}
	if runtime.Getenv == nil {
		runtime.Getenv = os.Getenv
	}
	if runtime.ServiceFactory == nil {
		runtime.ServiceFactory = newService
	}
	state := &commandState{runtime: runtime, output: "json"}
	state.config.Server = firstNonEmpty(runtime.Getenv("CONDUIT_SERVER"), runtime.Getenv("CONTROL_PLANE_URL"))
	state.config.CAFile = runtime.Getenv("CONDUIT_CA_FILE")
	state.config.ClientCertFile = runtime.Getenv("CONDUIT_CLIENT_CERT_FILE")
	state.config.ClientKeyFile = runtime.Getenv("CONDUIT_CLIENT_KEY_FILE")
	state.config.SPIFFEEndpoint = firstNonEmpty(runtime.Getenv("CONDUIT_SPIFFE_ENDPOINT"), runtime.Getenv("SPIFFE_ENDPOINT_SOCKET"))
	state.config.HubSPIFFEID = firstNonEmpty(runtime.Getenv("CONDUIT_HUB_SPIFFE_ID"), runtime.Getenv("CONTROL_PLANE_SPIFFE_ID"))
	state.config.BearerTokenFile = runtime.Getenv("CONDUIT_BEARER_TOKEN_FILE")
	state.config.OIDCIssuer = runtime.Getenv("CONDUIT_OIDC_ISSUER")
	state.config.OIDCClientID = runtime.Getenv("CONDUIT_OIDC_CLIENT_ID")
	state.config.OIDCClientSecretFile = runtime.Getenv("CONDUIT_OIDC_CLIENT_SECRET_FILE")
	state.config.OIDCTokenURL = runtime.Getenv("CONDUIT_OIDC_TOKEN_URL")
	state.config.OIDCScope = runtime.Getenv("CONDUIT_OIDC_SCOPE")
	state.config.OIDCCAFile = runtime.Getenv("CONDUIT_OIDC_CA_FILE")
	state.config.SpaceID = runtime.Getenv("CONDUIT_SPACE_ID")
	state.config.Timeout = defaultTimeout

	root := &cobra.Command{
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return validateOutputFormat(state.output)
		},
		Use:           "conduit",
		Short:         "Operate the Conduit Hub/Spoke delivery control plane",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(runtime.Stdout)
	root.SetIn(runtime.Stdin)
	root.SetErr(runtime.Stderr)
	flags := root.PersistentFlags()
	flags.StringVar(&state.config.SpaceID, "space-id", state.config.SpaceID, "native Space selection for Environment listing and publication (CONDUIT_SPACE_ID)")
	flags.StringVar(&state.config.Server, "server", state.config.Server, "Conduit Hub base URL (CONDUIT_SERVER)")
	flags.DurationVar(&state.config.Timeout, "timeout", defaultTimeout, "request timeout")
	flags.StringVarP(&state.output, "output", "o", "json", "output format: json, yaml, csv, or table")
	flags.StringVar(&state.profile, "profile", runtime.Getenv("CONDUIT_PROFILE"), "named local connection profile")
	flags.StringVar(&state.config.CAFile, "ca-file", state.config.CAFile, "Hub CA PEM file (CONDUIT_CA_FILE)")
	flags.StringVar(&state.config.ClientCertFile, "client-cert-file", state.config.ClientCertFile, "operator client certificate PEM file")
	flags.StringVar(&state.config.ClientKeyFile, "client-key-file", state.config.ClientKeyFile, "operator client private key PEM file")
	flags.StringVar(&state.config.SPIFFEEndpoint, "spiffe-endpoint", state.config.SPIFFEEndpoint, "SPIFFE Workload API address")
	flags.StringVar(&state.config.HubSPIFFEID, "hub-spiffe-id", state.config.HubSPIFFEID, "exact Conduit Hub SPIFFE ID")
	flags.StringVar(&state.config.BearerTokenFile, "bearer-token-file", state.config.BearerTokenFile, "mode-0600 Multipass bearer token file")
	flags.StringVar(&state.config.OIDCIssuer, "oidc-issuer", state.config.OIDCIssuer, "OIDC issuer for the client-credentials grant (CONDUIT_OIDC_ISSUER)")
	flags.StringVar(&state.config.OIDCClientID, "oidc-client-id", state.config.OIDCClientID, "OIDC client id (CONDUIT_OIDC_CLIENT_ID)")
	flags.StringVar(&state.config.OIDCClientSecretFile, "oidc-client-secret-file", state.config.OIDCClientSecretFile, "mode-0600 OIDC client secret file (CONDUIT_OIDC_CLIENT_SECRET_FILE)")
	flags.StringVar(&state.config.OIDCTokenURL, "oidc-token-url", state.config.OIDCTokenURL, "OIDC token endpoint; discovered from the issuer when omitted (CONDUIT_OIDC_TOKEN_URL)")
	flags.StringVar(&state.config.OIDCScope, "oidc-scope", state.config.OIDCScope, "optional OIDC scope (CONDUIT_OIDC_SCOPE)")
	flags.StringVar(&state.config.OIDCCAFile, "oidc-ca-file", state.config.OIDCCAFile, "CA PEM for the OIDC issuer when it is not public (CONDUIT_OIDC_CA_FILE)")
	flags.StringVar(&state.config.DevelopmentSubject, "development-subject", "", "explicit loopback-only insecure development identity")
	flags.StringSliceVar(&state.config.DevelopmentGroups, "development-groups", nil, "groups for the loopback-only development identity")

	root.AddCommand(versionCommand(state))
	root.AddCommand(completionCommand(root))
	root.AddCommand(configureCommand(state))
	root.AddCommand(profileCommand(state))
	root.AddCommand(artifactRegistryCommand(state))
	root.AddCommand(changeRequestCommand(state))
	root.AddCommand(planCommand(state))
	root.AddCommand(environmentCommand(state))
	root.AddCommand(spaceCommand(state))
	root.AddCommand(moduleCommand(state))
	root.AddCommand(productReleaseCommand(state))
	root.AddCommand(productCommand(state))
	root.AddCommand(cveCommand(state))
	root.AddCommand(helmChartCommand(state))
	root.AddCommand(labelCommand(state))
	root.AddCommand(releaseChannelCommand(state))
	root.AddCommand(terminalCommand(state))
	root.AddCommand(entityCommand(state))
	root.AddCommand(suppressionCommand(state))
	root.AddCommand(teamCommand(state))
	root.AddCommand(grantCommand(state))
	root.AddCommand(approvalPolicyCommand(state))
	root.AddCommand(runCommand(state))
	root.AddCommand(bundleCommand(state))
	// Added last: it attaches subcommands to groups the calls above create.
	addApolloCoverageCommands(root, state)
	return root
}

func artifactRegistryCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{
		Use:   "artifact-registry",
		Short: "Inspect Artifact Registries assigned to visible Environments",
	}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all visible Artifact Registries",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return state.execute(command, func(service Service) (any, error) {
				records, err := service.ListArtifactRegistries(command.Context())
				if err != nil {
					return nil, err
				}
				return map[string]any{"artifactRegistries": records}, nil
			})
		},
	})
	return command
}

func changeRequestCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{
		Use:     "changerequest",
		Aliases: []string{"change-request"},
		Short:   "Inspect and review Change Requests",
	}
	var environment, author, status, title string
	var limit int
	list := &cobra.Command{
		Use:   "list",
		Short: "List visible Change Requests",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return state.execute(command, func(service Service) (any, error) {
				requests, err := service.ListChangeRequests(command.Context(), environment)
				if err != nil {
					return nil, err
				}
				filtered := make([]controlplane.ChangeRequest, 0, len(requests))
				for _, request := range requests {
					if author != "" && request.Author.Username != author {
						continue
					}
					if status != "" && !strings.EqualFold(request.Status, status) {
						continue
					}
					if title != "" && !strings.Contains(strings.ToLower(request.Title), strings.ToLower(title)) {
						continue
					}
					filtered = append(filtered, request)
					if limit > 0 && len(filtered) == limit {
						break
					}
				}
				return map[string]any{"changeRequests": filtered}, nil
			})
		},
	}
	list.Flags().StringVarP(&environment, "environment", "e", "", "filter by Environment ID")
	list.Flags().StringVar(&author, "author", "", "filter by exact author username")
	list.Flags().StringVar(&status, "status", "", "filter by status")
	list.Flags().StringVar(&title, "title", "", "filter by title substring")
	list.Flags().IntVar(&limit, "limit", 0, "maximum number of results; zero returns all")
	command.AddCommand(list)
	command.AddCommand(&cobra.Command{
		Use:   "get REQUEST_ID",
		Short: "Get a Change Request",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return state.execute(command, func(service Service) (any, error) {
				return service.GetChangeRequest(command.Context(), args[0])
			})
		},
	})
	command.AddCommand(changeRequestReviewCommand(state, "approve"))
	command.AddCommand(changeRequestReviewCommand(state, "reject"))
	command.AddCommand(changeRequestStateCommand(state, "reopen"))
	command.AddCommand(changeRequestStateCommand(state, "cancel"))
	addChangeActivityCommands(command, state)
	return command
}

func changeRequestReviewCommand(state *commandState, action string) *cobra.Command {
	var rationale string
	var dryRun bool
	command := &cobra.Command{
		Use:   action + " REQUEST_ID",
		Short: strings.ToUpper(action[:1]) + action[1:] + " a Change Request",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(rationale) == "" {
				return errors.New("--rationale is required")
			}
			return state.execute(command, func(service Service) (any, error) {
				if dryRun {
					request, err := service.GetChangeRequest(command.Context(), args[0])
					return map[string]any{"dryRun": true, "action": action, "changeRequest": request}, err
				}
				if action == "approve" {
					return service.ApproveChangeRequest(command.Context(), args[0], rationale)
				}
				return service.RejectChangeRequest(command.Context(), args[0], rationale)
			})
		},
	}
	command.Flags().StringVar(&rationale, "rationale", "", "review rationale")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate and display the action without mutating state")
	return command
}

func changeRequestStateCommand(state *commandState, action string) *cobra.Command {
	var reason string
	var dryRun bool
	command := &cobra.Command{
		Use:   action + " REQUEST_ID",
		Short: strings.ToUpper(action[:1]) + action[1:] + " a Change Request",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if action == "cancel" && strings.TrimSpace(reason) == "" {
				return errors.New("--reason is required")
			}
			return state.execute(command, func(service Service) (any, error) {
				if dryRun {
					request, err := service.GetChangeRequest(command.Context(), args[0])
					return map[string]any{"dryRun": true, "action": action, "changeRequest": request}, err
				}
				if action == "reopen" {
					return service.ReopenChangeRequest(command.Context(), args[0])
				}
				return service.CancelChangeRequest(command.Context(), args[0], reason)
			})
		},
	}
	if action == "cancel" {
		command.Flags().StringVar(&reason, "reason", "", "cancellation reason")
	}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate and display the action without mutating state")
	return command
}

func newService(ctx context.Context, configuration ConnectionConfig) (Service, func(), error) {
	connection, err := configuration.connect(ctx)
	if err != nil {
		return nil, nil, err
	}
	adapter, err := operator.NewHTTPControlPlane(configuration.Server, connection.client)
	if err != nil {
		connection.cleanup()
		return nil, nil, err
	}
	if err := adapter.SetSpaceSelection(configuration.SpaceID); err != nil {
		connection.cleanup()
		return nil, nil, err
	}
	service := operator.NewService(adapter)
	if err := adapter.SetPlanReadTransport(configuration.PlanReadTransport); err != nil {
		connection.cleanup()
		return nil, nil, err
	}
	if err := adapter.SetPackagingReadTransport(configuration.PackagingReadTransport); err != nil {
		connection.cleanup()
		return nil, nil, err
	}
	return service, connection.cleanup, nil
}

func (state *commandState) execute(command *cobra.Command, operation func(Service) (any, error)) error {
	if err := state.applySelectedProfile(command); err != nil {
		return err
	}
	if _, err := state.config.validate(); err != nil {
		return err
	}
	service, cleanup, err := state.runtime.ServiceFactory(command.Context(), state.config)
	if err != nil {
		return err
	}
	defer cleanup()
	result, err := operation(service)
	if err != nil {
		return err
	}
	return writeOutput(state.runtime.Stdout, state.output, result)
}

func versionCommand(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print Conduit CLI build identity",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return writeOutput(state.runtime.Stdout, state.output, buildinfo.Current())
		},
	}
}

func completionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(command *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(command.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(command.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(command.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(command.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}
}

func planCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "plan", Short: "Propose, inspect, approve, and cancel Plans"}
	var file string
	propose := &cobra.Command{
		Use:   "propose",
		Short: "Propose a Plan from a desired-state document",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			var desired controlplane.DesiredState
			if err := decodeDocument(file, &desired); err != nil {
				return err
			}
			return state.execute(command, func(service Service) (any, error) {
				return planView(service.ProposePlan(command.Context(), desired))
			})
		},
	}
	propose.Flags().StringVarP(&file, "file", "f", "", "desired-state YAML or JSON file; use - for stdin")
	_ = propose.MarkFlagRequired("file")
	command.AddCommand(propose)
	var environment, status, installationID, search string
	list := &cobra.Command{
		Use:   "list",
		Short: "List visible Plans",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return state.execute(command, func(service Service) (any, error) {
				listed, err := service.ListPlans(command.Context(), operator.PlanListQuery{
					Environment: environment, Status: status, InstallationID: installationID, Search: search,
				})
				if err != nil {
					return nil, err
				}
				return planList{PlanListResult: listed}, nil
			})
		},
	}
	list.Flags().StringVar(&state.config.PlanReadTransport, "read-transport", "rest", "Plan summary transport: rest or connect; failures are not replayed")
	list.Flags().StringVar(&environment, "environment", "", "Environment id")
	list.Flags().StringVar(&status, "status", "", "Plan status")
	list.Flags().StringVar(&installationID, "installation-id", "", "installation targeted by a Plan step")
	list.Flags().StringVar(&search, "search", "", "case-insensitive search applied by the Hub")
	command.AddCommand(list)
	command.AddCommand(&cobra.Command{
		Use:     "get PLAN_ID",
		Aliases: []string{"show"},
		Short:   "Get a Plan and its exact execution evidence",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return state.execute(command, func(service Service) (any, error) {
				return planView(service.GetPlan(command.Context(), args[0]))
			})
		},
	})
	var rationale, approvalTransport string
	approve := &cobra.Command{
		Use:   "approve PLAN_ID",
		Short: "Approve a reviewed Plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(rationale) == "" {
				return fmt.Errorf("--rationale is required")
			}
			return executePlanApproval(command, state, args[0], rationale, approvalTransport)
		},
	}
	approve.Flags().StringVar(&rationale, "rationale", "", "review rationale")
	approve.Flags().StringVar(&approvalTransport, "write-transport", "rest", "Approval transport: rest (full Plan) or connect (conduit.plan-approval/v1 receipt); failures are not replayed")
	command.AddCommand(approve)
	var reason string
	cancel := &cobra.Command{
		Use:   "cancel PLAN_ID",
		Short: "Cancel a Plan before or during execution",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required")
			}
			return state.execute(command, func(service Service) (any, error) {
				return planView(service.CancelPlan(command.Context(), args[0], reason))
			})
		},
	}
	cancel.Flags().StringVar(&reason, "reason", "", "cancellation reason")
	command.AddCommand(cancel)
	return command
}

func environmentCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "environment", Aliases: []string{"env"}, Short: "Inspect Environment state"}
	var contactTeam string
	create := &cobra.Command{
		Use:   "create ENVIRONMENT_NAME",
		Short: "Author a Git-owned Environment descriptor",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			descriptor, err := newEnvironmentDescriptor(args[0], contactTeam)
			if err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, descriptor)
		},
	}
	create.Flags().StringVar(&contactTeam, "contact-team", "",
		"Team responsible for the Environment. If omitted, the Environment is authored without an owning team.")
	command.AddCommand(create)
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List visible Environments",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return state.execute(command, func(service Service) (any, error) {
				records, err := service.ListEnvironments(command.Context())
				if err != nil {
					return nil, err
				}
				return environmentList{Environments: records}, nil
			})
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "get ENVIRONMENT_ID",
		Short: "Get a declared and reported Environment projection",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return state.execute(command, func(service Service) (any, error) {
				return service.GetEnvironment(command.Context(), args[0])
			})
		},
	})

	manifest := &cobra.Command{Use: "manifest", Short: "Generate Spoke deployment manifests"}
	var namespace, image, hubURL, agentID, clientTLSSecret, externalCAConfigMap, storageClass string
	generate := &cobra.Command{
		Use:   "generate ENVIRONMENT_ID",
		Short: "Generate a pipeable restricted Spoke manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			result, err := generateSpokeManifest(spokeManifestInput{
				Environment: args[0], Namespace: namespace, Image: image, HubURL: hubURL, AgentID: agentID,
				ClientTLSSecret: clientTLSSecret, ExternalCAConfigMap: externalCAConfigMap, StorageClass: storageClass,
			})
			if err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, result)
		},
	}
	generate.Flags().StringVar(&namespace, "namespace", "", "externally supplied Kubernetes namespace")
	generate.Flags().StringVar(&image, "image", "", "immutable conduit-agent image digest reference")
	generate.Flags().StringVar(&hubURL, "control-plane-url", "", "Conduit Hub URL reachable from the Spoke")
	generate.Flags().StringVar(&agentID, "agent-id", "", "Spoke agent ID; defaults to the Environment ID")
	generate.Flags().StringVar(&clientTLSSecret, "client-tls-secret", "", "existing mTLS Secret name")
	generate.Flags().StringVar(&externalCAConfigMap, "external-ca-configmap", "", "existing Hub CA ConfigMap name")
	generate.Flags().StringVar(&storageClass, "k8s-storage-class", "", "recorded for compatibility; the stateless Spoke does not claim storage")
	for _, flag := range []string{"namespace", "image", "control-plane-url", "client-tls-secret", "external-ca-configmap"} {
		_ = generate.MarkFlagRequired(flag)
	}
	manifest.AddCommand(generate)
	command.AddCommand(manifest)
	command.AddCommand(&cobra.Command{
		Use:   "reported-state ENVIRONMENT_ID",
		Short: "Get the latest exact state reported by a Spoke",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return state.execute(command, func(service Service) (any, error) {
				return service.GetReportedState(command.Context(), args[0])
			})
		},
	})
	addEnvironmentCoverageCommands(command, state)
	return command
}

func moduleCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "module", Short: "Manage versioned Modules"}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List visible Module Releases",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return state.execute(command, func(service Service) (any, error) {
				releases, err := service.ListModules(command.Context())
				return map[string]any{"releases": releases}, err
			})
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "get MODULE_PRODUCT_ID",
		Short: "Get all visible Releases for a Module",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return state.execute(command, func(service Service) (any, error) {
				releases, err := service.ListModules(command.Context())
				filtered := make([]controlplane.ModuleRelease, 0)
				for _, release := range releases {
					if release.Name == args[0] {
						filtered = append(filtered, release)
					}
				}
				return map[string]any{"releases": filtered}, err
			})
		},
	})
	var environment, releaseChannel, version string
	var variableFlags []string
	var ignoreRecalls, ignoreSecrets bool
	var overrideRationale string
	install := &cobra.Command{
		Use:   "install MODULE_PRODUCT_ID_OR_COORDINATE",
		Short: "Install an exact or channel-tracking Module into an Environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			variables, err := parseKeyValues(variableFlags)
			if err != nil {
				return err
			}
			module, coordinateVersion, err := splitOptionalVersion(args[0])
			if err != nil {
				return err
			}
			if version == "" {
				version = coordinateVersion
			} else if coordinateVersion != "" && version != coordinateVersion {
				return errors.New("--version conflicts with the Module coordinate version")
			}
			if (version == "") == (releaseChannel == "") {
				return errors.New("select exactly one of an exact Module version or --release-channel")
			}
			if ignoreRecalls || ignoreSecrets {
				return proposeModuleOverride(command, state, ports.ModuleDraft{Action: "INSTALL", Environment: environment, Module: module, Version: version, ReleaseChannel: releaseChannel, Variables: variables, Flags: override.Flags{IgnoreRecalls: ignoreRecalls, IgnoreSecretRequirements: ignoreSecrets}, Rationale: overrideRationale}, false)
			}
			return state.execute(command, func(service Service) (any, error) {
				return service.InstallModule(command.Context(), environment, module, version, releaseChannel, variables)
			})
		},
	}
	install.Flags().StringVarP(&environment, "environment", "e", "", "Environment ID")
	install.Flags().StringVarP(&releaseChannel, "release-channel", "r", "", "Release Channel to track")
	install.Flags().StringVar(&version, "version", "", "exact Module version")
	install.Flags().StringArrayVar(&variableFlags, "variable", nil, "Module variable as key=value; repeatable")
	install.Flags().BoolVar(&ignoreRecalls, "ignore-recalls", false, "request independent review for the exact recalled Module release")
	install.Flags().BoolVar(&ignoreSecrets, "ignore-secret-requirements", false, "record reviewed intent only; Module metadata has no runtime secret gate")
	install.Flags().StringVar(&overrideRationale, "rationale", "", "reason required when requesting a reviewed override")
	_ = install.MarkFlagRequired("environment")
	command.AddCommand(install)
	addModuleCoverageCommands(command, state)
	addModuleOverrideCommands(command, state)
	return command
}

func productReleaseCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "product-release", Short: "Author and publish immutable Product Releases"}
	command.AddCommand(productReleaseChannelCommand(state))
	helmChart := &cobra.Command{Use: "helm-chart", Short: "Author Helm chart Product manifests"}
	var options helmChartInitOptions
	initCommand := &cobra.Command{
		Use:   "init",
		Short: "Initialize a digest-pinned helm-chart.v1 Product manifest",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			result, err := initializeHelmChartManifest(options)
			if err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, result)
		},
	}
	initCommand.Flags().StringVar(&options.RepositoryURL, "repository-url", "", "Helm repository URL")
	initCommand.Flags().StringVar(&options.Coordinate, "maven-coordinate", "", "Product group:name:version coordinate")
	initCommand.Flags().StringVar(&options.ChartPath, "chart-path", "", "Helm chart directory containing Chart.yaml and values.yaml")
	initCommand.Flags().StringVar(&options.Name, "name", "", "Helm chart name")
	initCommand.Flags().StringVar(&options.Version, "version", "", "Helm chart version")
	initCommand.Flags().StringVar(&options.ValuesPath, "values", "", "values.yaml copied beside the manifest")
	initCommand.Flags().StringVar(&options.SchemaPath, "config-schema", "", "optional values.schema.json copied or packaged with the manifest")
	initCommand.Flags().StringVar(&options.OutputDir, "output-dir", "", "output directory")
	initCommand.Flags().StringArrayVar(&options.Artifacts, "artifact", nil, "exact OCI artifact reference with @sha256 digest; repeatable")
	initCommand.Flags().BoolVar(&options.AsTGZ, "as-tgz", false, "write one deterministic Product TGZ instead of loose files")
	for _, flag := range []string{"repository-url", "maven-coordinate", "output-dir"} {
		_ = initCommand.MarkFlagRequired(flag)
	}
	helmChart.AddCommand(initCommand)
	command.AddCommand(helmChart)
	var manifestPath, productTGZPath, defaultConfigPath, configSchemaPath string
	var valuesPath, valuesSchemaPath string
	create := &cobra.Command{
		Use:   "create",
		Short: "Publish an immutable Product Release from a validated manifest",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			var err error
			if defaultConfigPath, err = soleSpelling(command, "default-config", defaultConfigPath, "values", valuesPath); err != nil {
				return err
			}
			if configSchemaPath, err = soleSpelling(command, "config-schema", configSchemaPath, "values-schema", valuesSchemaPath); err != nil {
				return err
			}
			// Presence, not emptiness: `--values=` is a stated intent that this
			// path cannot honour, and treating it as absence would publish a
			// Terraform Release while ignoring a flag the operator passed.
			helmConfigGiven := command.Flags().Changed("default-config") || command.Flags().Changed("values") ||
				command.Flags().Changed("config-schema") || command.Flags().Changed("values-schema")
			if productTGZPath != "" {
				if manifestPath != "" || defaultConfigPath != "" || configSchemaPath != "" {
					return errors.New("--product-tgz cannot be combined with --manifest, --default-config, or --config-schema")
				}
				bundle, err := decodeHelmProductTGZ(productTGZPath)
				if err != nil {
					return err
				}
				return state.execute(command, func(service Service) (any, error) {
					return service.PublishProductRelease(command.Context(), bundle)
				})
			}
			if manifestPath == "" {
				return errors.New("select exactly one of --manifest or --product-tgz")
			}
			var kind struct {
				ProductType string `yaml:"product-type"`
			}
			contents, err := os.ReadFile(manifestPath)
			if err != nil {
				return err
			}
			if err := yaml.Unmarshal(contents, &kind); err != nil {
				return err
			}
			if kind.ProductType == "terraform.v1" {
				// Helm configuration against a Terraform Product was accepted
				// and then silently dropped: the bundle these flags fill is
				// never built on this path. Apollo errors, and so must this --
				// an operator who passed a values file has stated an intent
				// that is not going to happen, and publishing anyway makes the
				// Release quietly different from what they asked for.
				if helmConfigGiven {
					return errors.New("--values and --values-schema apply to helm-chart.v1 Products; this manifest is terraform.v1")
				}
				manifest, err := controlplane.DecodeTerraformProductManifest(bytes.NewReader(contents))
				if err != nil {
					return err
				}
				return state.execute(command, func(service Service) (any, error) {
					return service.PublishTerraformProductRelease(command.Context(), manifest)
				})
			}
			manifest, err := readProductManifest(manifestPath)
			if err != nil {
				return err
			}
			bundle := controlplane.HelmProductReleaseBundle{Manifest: manifest}
			if defaultConfigPath != "" {
				bundle.DefaultConfig, err = os.ReadFile(defaultConfigPath)
				if err != nil {
					return fmt.Errorf("read default Helm configuration: %w", err)
				}
			}
			if configSchemaPath != "" {
				bundle.ConfigSchema, err = os.ReadFile(configSchemaPath)
				if err != nil {
					return fmt.Errorf("read Helm configuration schema: %w", err)
				}
			}
			if err := controlplane.ValidateHelmProductReleaseBundle(bundle); err != nil {
				return err
			}
			return state.execute(command, func(service Service) (any, error) {
				return service.PublishProductRelease(command.Context(), bundle)
			})
		},
	}
	create.Flags().StringVar(&manifestPath, "manifest", "", "helm-chart.v1 manifest path")
	create.Flags().StringVar(&productTGZPath, "product-tgz", "", "canonical Helm Product TGZ")
	create.Flags().StringVar(&defaultConfigPath, "default-config", "", "optional default values.yaml bound to the Release")
	create.Flags().StringVar(&configSchemaPath, "config-schema", "", "optional values.schema.json bound to the Release")
	// Apollo's published spellings for the same two inputs, added to
	// product-release create in apollo-cli 0.811.0. They are the same flag, not
	// a second path: an operator following Apollo's documentation reaches the
	// bundle Conduit already builds, and helm-chart init already spells the
	// first of them --values.
	create.Flags().StringVar(&valuesPath, "values", "", "alias for --default-config, Apollo's published spelling")
	create.Flags().StringVar(&valuesSchemaPath, "values-schema", "", "alias for --config-schema, Apollo's published spelling")
	command.AddCommand(create)
	var listProduct, listChannel string
	var pageSize int
	list := &cobra.Command{
		Use:   "list",
		Short: "List immutable Product Releases",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if strings.TrimSpace(listProduct) == "" {
				return errors.New("--product-id is required by Conduit's bounded catalog projection")
			}
			return state.execute(command, func(service Service) (any, error) {
				releases, err := service.ListProductReleases(command.Context(), listProduct)
				if err != nil {
					return nil, err
				}
				publications, err := service.ListProductPublications(command.Context(), listProduct)
				filtered := make([]controlplane.Release, 0, len(releases))
				for _, release := range releases {
					if listChannel != "" && !containsString(release.Channels, listChannel) {
						continue
					}
					filtered = append(filtered, release)
					if pageSize > 0 && len(filtered) == pageSize {
						break
					}
				}
				return map[string]any{"releases": filtered, "publications": publications}, err
			})
		},
	}
	list.Flags().StringVarP(&listProduct, "product-id", "p", "", "Product group:name coordinate")
	list.Flags().StringVar(&listChannel, "release-channel", "", "filter by Release Channel")
	list.Flags().IntVar(&pageSize, "page-size", 0, "maximum number of Releases; zero returns all")
	command.AddCommand(list)
	command.AddCommand(&cobra.Command{
		Use:   "get MAVEN_COORDINATE [MAVEN_COORDINATE...]",
		Short: "Get one or more immutable Product Releases",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return state.execute(command, func(service Service) (any, error) {
				results := make([]controlplane.Release, 0, len(args))
				publications := make([]controlplane.ProductPublication, 0, len(args))
				for _, coordinate := range args {
					product, version, err := splitProductCoordinate(coordinate)
					if err != nil {
						return nil, err
					}
					published, err := service.ListProductPublications(command.Context(), product)
					if err != nil {
						return nil, err
					}
					found := false
					for _, publication := range published {
						if publication.Version == version {
							publications = append(publications, publication)
							found = true
							break
						}
					}
					if found {
						continue
					}
					release, err := service.GetProductRelease(command.Context(), product, version)
					if err != nil {
						return nil, err
					}
					results = append(results, release)
				}
				return map[string]any{"releases": results, "publications": publications}, nil
			})
		},
	})
	addProductReleaseCoverageCommands(command, state)
	return command
}

func entityCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "entity", Short: "Propose governed Entity operations"}
	var environment, file, productID string
	install := &cobra.Command{
		Use:   "install",
		Short: "Propose an exact desired-state installation for approval",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			var desired controlplane.DesiredState
			if err := decodeDocument(file, &desired); err != nil {
				return err
			}
			if desired.Environment.ID != environment {
				return errors.New("desired-state Environment does not match --environment")
			}
			found := false
			for _, entity := range desired.Entities {
				found = found || entity.Product == productID
			}
			if !found {
				return errors.New("desired state contains no Entity for --product-id")
			}
			return state.execute(command, func(service Service) (any, error) {
				return service.ProposePlan(command.Context(), desired)
			})
		},
	}
	install.Flags().StringVarP(&environment, "environment", "e", "", "Environment ID")
	install.Flags().StringVarP(&file, "file", "f", "", "exact desired-state YAML or JSON file")
	install.Flags().StringVarP(&productID, "product-id", "p", "", "Product group:name coordinate")
	for _, flag := range []string{"environment", "file", "product-id"} {
		_ = install.MarkFlagRequired(flag)
	}
	command.AddCommand(install)
	var listEnvironment string
	list := &cobra.Command{
		Use:   "list",
		Short: "List desired Entities for an Environment",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return state.execute(command, func(service Service) (any, error) {
				entities, err := service.ListEntities(command.Context(), listEnvironment)
				return map[string]any{"entities": entities}, err
			})
		},
	}
	list.Flags().StringVarP(&listEnvironment, "environment", "e", "", "Environment ID")
	_ = list.MarkFlagRequired("environment")
	command.AddCommand(list)
	addEntityCoverageCommands(command, state)
	return command
}

func productCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "product", Short: "Inspect Products and govern promotion pipelines"}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List visible Products",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return state.execute(command, func(service Service) (any, error) {
				products, err := service.ListProducts(command.Context())
				return map[string]any{"products": products}, err
			})
		},
	})
	command.AddCommand(productPromotionCommand(state))
	return command
}

func suppressionCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "suppression", Short: "Inspect and remove deployment suppressions"}
	var environment string
	list := &cobra.Command{
		Use:   "list",
		Short: "List suppressions for an Environment",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if strings.TrimSpace(environment) == "" {
				return fmt.Errorf("--environment is required")
			}
			return state.execute(command, func(service Service) (any, error) {
				return service.ListSuppressions(command.Context(), environment)
			})
		},
	}
	list.Flags().StringVarP(&environment, "environment", "e", "", "Environment ID")
	command.AddCommand(list)
	command.AddCommand(&cobra.Command{
		Use:   "remove SUPPRESSION_ID",
		Short: "Remove a suppression through the governed operator API",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return state.execute(command, func(service Service) (any, error) {
				return service.RemoveSuppression(command.Context(), args[0])
			})
		},
	})
	return command
}

func teamCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "team", Short: "Manage governance Teams"}
	var file string
	apply := &cobra.Command{
		Use:   "apply TEAM_NAME",
		Short: "Create or update a Team from a document",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			var team controlplane.Team
			if err := decodeDocument(file, &team); err != nil {
				return err
			}
			return state.execute(command, func(service Service) (any, error) {
				return service.PutTeam(command.Context(), args[0], team)
			})
		},
	}
	apply.Flags().StringVarP(&file, "file", "f", "", "Team YAML or JSON file; use - for stdin")
	_ = apply.MarkFlagRequired("file")
	command.AddCommand(apply)
	return command
}

func grantCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "grant", Short: "Manage resource role grants"}
	var file string
	create := &cobra.Command{
		Use:   "create",
		Short: "Create an immutable resource grant from a document",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			var grant controlplane.ResourceGrant
			if err := decodeDocument(file, &grant); err != nil {
				return err
			}
			return state.execute(command, func(service Service) (any, error) {
				return service.CreateResourceGrant(command.Context(), grant)
			})
		},
	}
	create.Flags().StringVarP(&file, "file", "f", "", "grant YAML or JSON file; use - for stdin")
	_ = create.MarkFlagRequired("file")
	command.AddCommand(create)
	return command
}

func approvalPolicyCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "approval-policy", Short: "Manage approval policies"}
	var file string
	apply := &cobra.Command{
		Use:   "apply POLICY_ID",
		Short: "Create or update an approval policy from a document",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			var policy controlplane.ApprovalPolicy
			if err := decodeDocument(file, &policy); err != nil {
				return err
			}
			return state.execute(command, func(service Service) (any, error) {
				return service.PutApprovalPolicy(command.Context(), args[0], policy)
			})
		},
	}
	apply.Flags().StringVarP(&file, "file", "f", "", "approval-policy YAML or JSON file; use - for stdin")
	_ = apply.MarkFlagRequired("file")
	command.AddCommand(apply)
	return command
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func splitProductCoordinate(coordinate string) (string, string, error) {
	parts := strings.Split(coordinate, ":")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", fmt.Errorf("Product Release coordinate %q must be group:name:version", coordinate)
	}
	return parts[0] + ":" + parts[1], parts[2], nil
}
