package conduitcli

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	requests "github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/spf13/cobra"
)

func debugRequestCommand(state *commandState) *cobra.Command {
	group := &cobra.Command{Use: "request", Short: "Manage native non-executing debug request metadata; this does not open a terminal"}
	ready := func() error {
		format := strings.ToLower(strings.TrimSpace(state.output))
		if format != "json" && format != "yaml" {
			return errors.New("debug request metadata requires json or yaml output")
		}
		if state.config.SpaceID != "" {
			return errors.New("debug request metadata uses explicit Environment/namespace scope; Space selection is not supported")
		}
		return nil
	}
	run := func(cmd *cobra.Command, fn func(operator.DebugRequestControlPlane) (any, error)) error {
		if err := ready(); err != nil {
			return err
		}
		return state.execute(cmd, func(service Service) (any, error) {
			if err := ready(); err != nil {
				return nil, err
			}
			port, ok := service.(operator.DebugRequestControlPlane)
			if !ok {
				return nil, errors.New("debug request metadata is unavailable from this service")
			}
			return fn(port)
		})
	}
	var environment, namespace, profile, requestID string
	var lifetime time.Duration
	create := &cobra.Command{Use: "create", Short: "Record an audited metadata request; profile and broker preflight remain unverified", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		target := requests.Target{Environment: environment, Namespace: namespace, Profile: profile}
		if target.Validate() != nil || lifetime <= 0 || lifetime > requests.MaximumLifetime || lifetime%time.Second != 0 {
			return errors.New("expected Environment, namespace, profile and a positive whole-second lifetime up to 4h")
		}
		id := requestID
		if id == "" {
			var entropy [16]byte
			if _, err := rand.Read(entropy[:]); err != nil {
				return err
			}
			id = "dsr_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(entropy[:]))
		}
		if !requests.ValidID(id) {
			return requests.ErrInvalid
		}
		return run(cmd, func(port operator.DebugRequestControlPlane) (any, error) {
			record, err := port.CreateDebugRequest(cmd.Context(), operator.DebugRequestDraft{SessionID: id, Target: target, LifetimeSeconds: int64(lifetime / time.Second)})
			if err != nil {
				return nil, fmt.Errorf("debug request %s (retain this ID when resolving a retry): %w", id, err)
			}
			return record, nil
		})
	}}
	create.Flags().StringVarP(&environment, "environment", "e", "", "Environment ID (required)")
	create.Flags().StringVar(&namespace, "namespace", "", "declared Kubernetes namespace (required)")
	create.Flags().StringVar(&profile, "terminal-profile", "", "requested profile reference; not an execution preflight (required)")
	create.Flags().StringVar(&requestID, "request-id", "", "retained native request ID for an idempotent retry")
	create.Flags().DurationVar(&lifetime, "lifetime", requests.MaximumLifetime, "metadata lifetime, positive whole seconds up to 4h")
	for _, flag := range []string{"environment", "namespace", "terminal-profile"} {
		_ = create.MarkFlagRequired(flag)
	}
	group.AddCommand(create)
	var listEnvironment, listNamespace, after string
	var limit int
	list := &cobra.Command{Use: "list", Short: "List an owner-visible page of native metadata", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		scope := requests.Scope{Environment: listEnvironment, Namespace: listNamespace}
		if scope.Validate() != nil || limit < 1 || limit > 100 || (after != "" && !requests.ValidID(after)) {
			return requests.ErrInvalid
		}
		return run(cmd, func(port operator.DebugRequestControlPlane) (any, error) {
			return port.ListDebugRequests(cmd.Context(), scope, after, limit)
		})
	}}
	list.Flags().StringVarP(&listEnvironment, "environment", "e", "", "Environment ID (required)")
	list.Flags().StringVar(&listNamespace, "namespace", "", "namespace (required)")
	list.Flags().StringVar(&after, "after", "", "visible request ID cursor")
	list.Flags().IntVar(&limit, "limit", 50, "page size, 1..100")
	_ = list.MarkFlagRequired("environment")
	_ = list.MarkFlagRequired("namespace")
	group.AddCommand(list)
	for _, action := range []string{"get", "history", "revoke", "delete"} {
		command := &cobra.Command{Use: action + " REQUEST_ID", Short: action + " native debug request metadata", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if !requests.ValidID(args[0]) {
				return requests.ErrInvalid
			}
			return run(cmd, func(port operator.DebugRequestControlPlane) (any, error) {
				switch action {
				case "get":
					return port.GetDebugRequest(cmd.Context(), args[0])
				case "history":
					return port.DebugRequestHistory(cmd.Context(), args[0])
				case "revoke":
					return port.MutateDebugRequest(cmd.Context(), args[0], requests.Revoked)
				default:
					return port.MutateDebugRequest(cmd.Context(), args[0], requests.Deleted)
				}
			})
		}}
		group.AddCommand(command)
	}
	return group
}
