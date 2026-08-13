package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

const maxInputBytes = 1 << 20

const usage = "usage: platform-operator {put-team NAME FILE|create-grant FILE|put-approval-policy ID FILE|propose-plan FILE|approve-plan ID RATIONALE|cancel-plan ID REASON|get-plan ID|get-reported-state ENVIRONMENT|list-suppressions ENVIRONMENT|remove-suppression ID}"

var operatorCommands = map[string]struct{}{
	"put-team": {}, "create-grant": {}, "put-approval-policy": {}, "propose-plan": {},
	"approve-plan": {}, "cancel-plan": {}, "get-plan": {}, "get-reported-state": {},
	"list-suppressions": {}, "remove-suppression": {},
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "platform-operator:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		_, err := fmt.Fprintln(output, usage)
		return err
	}
	if _, known := operatorCommands[args[0]]; !known {
		return usageError()
	}
	controlPlaneURL, err := requiredEnvironment("CONTROL_PLANE_URL")
	if err != nil {
		return err
	}
	if !strings.HasPrefix(controlPlaneURL, "https://") {
		return errors.New("CONTROL_PLANE_URL must use https with SPIFFE identity")
	}
	spiffeEndpoint, err := requiredEnvironment("SPIFFE_ENDPOINT_SOCKET")
	if err != nil {
		return err
	}
	source, err := workloadapi.NewX509Source(ctx, workloadapi.WithClientOptions(workloadapi.WithAddr(spiffeEndpoint)))
	if err != nil {
		return fmt.Errorf("connect to SPIFFE Workload API: %w", err)
	}
	defer source.Close()
	expectedHubID, err := requiredEnvironment("CONTROL_PLANE_SPIFFE_ID")
	if err != nil {
		return err
	}
	httpClient, err := operator.NewSPIFFEHTTPClient(source, expectedHubID)
	if err != nil {
		return fmt.Errorf("configure SPIFFE transport: %w", err)
	}
	adapter, err := operator.NewHTTPControlPlane(controlPlaneURL, httpClient)
	if err != nil {
		return err
	}
	service := operator.NewService(adapter)

	var result any
	switch args[0] {
	case "put-team":
		if len(args) != 3 {
			return usageError()
		}
		var value controlplane.Team
		if err := decodeFile(args[2], &value); err != nil {
			return err
		}
		result, err = service.PutTeam(ctx, args[1], value)
	case "create-grant":
		if len(args) != 2 {
			return usageError()
		}
		var value controlplane.ResourceGrant
		if err := decodeFile(args[1], &value); err != nil {
			return err
		}
		result, err = service.CreateResourceGrant(ctx, value)
	case "put-approval-policy":
		if len(args) != 3 {
			return usageError()
		}
		var value controlplane.ApprovalPolicy
		if err := decodeFile(args[2], &value); err != nil {
			return err
		}
		result, err = service.PutApprovalPolicy(ctx, args[1], value)
	case "propose-plan":
		if len(args) != 2 {
			return usageError()
		}
		var value controlplane.DesiredState
		if err := decodeFile(args[1], &value); err != nil {
			return err
		}
		result, err = service.ProposePlan(ctx, value)
	case "approve-plan":
		if len(args) != 3 {
			return usageError()
		}
		result, err = service.ApprovePlan(ctx, args[1], args[2])
	case "cancel-plan":
		if len(args) != 3 {
			return usageError()
		}
		result, err = service.CancelPlan(ctx, args[1], args[2])
	case "get-plan":
		if len(args) != 2 {
			return usageError()
		}
		result, err = service.GetPlan(ctx, args[1])
	case "get-reported-state":
		if len(args) != 2 {
			return usageError()
		}
		result, err = service.GetReportedState(ctx, args[1])
	case "list-suppressions":
		if len(args) != 2 {
			return usageError()
		}
		result, err = service.ListSuppressions(ctx, args[1])
	case "remove-suppression":
		if len(args) != 2 {
			return usageError()
		}
		result, err = service.RemoveSuppression(ctx, args[1])
	default:
		return usageError()
	}
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func decodeFile(path string, output any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open input file: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxInputBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode input file: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("input file must contain exactly one JSON value")
	}
	return nil
}

func requiredEnvironment(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("required environment variable %s is missing", name)
	}
	return value, nil
}

func usageError() error {
	return errors.New(usage)
}
