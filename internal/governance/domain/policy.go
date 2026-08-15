package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// policyVariablePattern is the published {{ policyVariable.name }} spelling,
// rewritten to the CEL field access the environment actually declares.
var policyVariablePattern = regexp.MustCompile(`\{\{\s*policyVariable\.([A-Za-z][A-Za-z0-9_]*)\s*\}\}`)

// PolicyResult is what one policy expression returned: either a decision
// (APPROVE, REJECT, NO_OP) or a requirement a reviewer must satisfy.
type PolicyResult struct {
	Kind, Reason, Team, ResourceRID, Role, Operation string
	Alternatives                                     []PolicyResult
}

// Requirement turns a result into the reviewer requirement recorded against a
// change. The identifier is a digest of the requirement itself, so the same
// demand raised twice is the same requirement rather than two.
func (result PolicyResult) Requirement(changeID, policyID string) ReviewerRequirement {
	requirement := ReviewerRequirement{
		ChangeID: changeID, PolicyID: policyID, Kind: result.Kind, Team: result.Team,
		ResourceRID: result.ResourceRID, Role: result.Role, Operation: result.Operation, Reason: result.Reason,
	}
	for _, alternative := range result.Alternatives {
		requirement.Alternatives = append(requirement.Alternatives, alternative.Requirement(changeID, policyID))
	}
	payload, _ := json.Marshal(requirement)
	digest := sha256.Sum256(payload)
	requirement.ID = "req_" + hex.EncodeToString(digest[:16])
	return requirement
}

// CompilePolicy prepares one policy expression for evaluation against the
// published predicate set and the operator's declared trust configuration.
func CompilePolicy(policy ApprovalPolicy, trust PolicyTrust) (cel.Program, error) {
	environment, err := policyEnvironment(trust)
	if err != nil {
		return nil, err
	}
	expression := policyVariablePattern.ReplaceAllString(policy.Expression, `policyVariable.$1`)
	ast, issues := environment.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return environment.Program(ast)
}

// EvaluatePolicy runs one policy against the facts of a change.
func EvaluatePolicy(policy ApprovalPolicy, input map[string]any, trust PolicyTrust) (PolicyResult, error) {
	program, err := CompilePolicy(policy, trust)
	if err != nil {
		return PolicyResult{}, err
	}
	variables := make(map[string]any, len(policy.Variables))
	for name, variable := range policy.Variables {
		switch variable.Type {
		case PolicyString:
			variables[name] = variable.String
		case PolicyStringList:
			variables[name] = append([]string(nil), variable.List...)
		default:
			return PolicyResult{}, fmt.Errorf("variable %s has unsupported type %s", name, variable.Type)
		}
	}
	input["policyVariable"] = variables
	value, _, err := program.Eval(input)
	if err != nil {
		return PolicyResult{}, err
	}
	native, err := value.ConvertToNative(reflect.TypeOf(""))
	if err != nil {
		return PolicyResult{}, fmt.Errorf("policy must return a result function: %w", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal([]byte(native.(string)), &normalized); err != nil {
		return PolicyResult{}, fmt.Errorf("normalize policy result: %w", err)
	}
	return decodePolicyResult(normalized)
}

func policyEnvironment(trust PolicyTrust) (*cel.Env, error) {
	result := func(kind string, args ...ref.Val) ref.Val {
		value := map[string]any{"kind": kind}
		if len(args) > 0 {
			value["reason"] = fmt.Sprint(args[len(args)-1].Value())
		}
		switch kind {
		case RequirementTeam:
			value["team"] = fmt.Sprint(args[0].Value())
		case RequirementRole:
			value["resourceRid"], value["role"] = fmt.Sprint(args[0].Value()), fmt.Sprint(args[1].Value())
		case RequirementOperation:
			value["resourceRid"], value["operation"] = fmt.Sprint(args[0].Value()), fmt.Sprint(args[1].Value())
		case RequirementAny:
			alternatives, err := args[0].ConvertToNative(reflect.TypeOf([]any{}))
			if err != nil {
				return types.NewErr("invalid require_any alternatives: %v", err)
			}
			decoded := make([]map[string]any, 0, len(alternatives.([]any)))
			for _, alternative := range alternatives.([]any) {
				var candidate map[string]any
				if err := json.Unmarshal([]byte(fmt.Sprint(alternative)), &candidate); err != nil {
					return types.NewErr("invalid require_any alternative: %v", err)
				}
				decoded = append(decoded, candidate)
			}
			value["alternatives"] = decoded
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return types.NewErr("encode policy result: %v", err)
		}
		return types.String(encoded)
	}
	options := []cel.EnvOption{
		cel.Variable("type", cel.StringType), cel.Variable("author", cel.DynType), cel.Variable("onBehalfOf", cel.DynType),
		cel.Variable("editors", cel.ListType(cel.StringType)), cel.Variable("metadata", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("request", cel.DynType), cel.Variable("diff", cel.DynType), cel.Variable("referencedResources", cel.DynType),
		cel.Variable("createdAt", cel.TimestampType), cel.Variable("now", cel.TimestampType), cel.Variable("policyVariable", cel.DynType),
		cel.Function("approve", cel.Overload("approve_string", []*cel.Type{cel.StringType}, cel.DynType,
			cel.UnaryBinding(func(arg ref.Val) ref.Val { return result("APPROVE", arg) }))),
		cel.Function("reject", cel.Overload("reject_string", []*cel.Type{cel.StringType}, cel.DynType,
			cel.UnaryBinding(func(arg ref.Val) ref.Val { return result("REJECT", arg) }))),
		cel.Function("no_op", cel.Overload("no_op", []*cel.Type{}, cel.DynType,
			cel.FunctionBinding(func(args ...ref.Val) ref.Val { return result("NO_OP", args...) }))),
		cel.Function("require_team", cel.Overload("require_team_strings", []*cel.Type{cel.StringType, cel.StringType}, cel.DynType,
			cel.BinaryBinding(func(left, right ref.Val) ref.Val { return result(RequirementTeam, left, right) }))),
		cel.Function("require_role", cel.Overload("require_role_strings", []*cel.Type{cel.StringType, cel.StringType, cel.StringType}, cel.DynType,
			cel.FunctionBinding(func(args ...ref.Val) ref.Val { return result(RequirementRole, args...) }))),
		cel.Function("require_operation", cel.Overload("require_operation_strings", []*cel.Type{cel.StringType, cel.StringType, cel.StringType}, cel.DynType,
			cel.FunctionBinding(func(args ...ref.Val) ref.Val { return result(RequirementOperation, args...) }))),
		cel.Function("require_any", cel.Overload("require_any_list_string", []*cel.Type{cel.ListType(cel.DynType), cel.StringType}, cel.DynType,
			cel.BinaryBinding(func(left, right ref.Val) ref.Val { return result(RequirementAny, left, right) }))),
	}
	return cel.NewEnv(append(options, policyPredicates(trust)...)...)
}

func decodePolicyResult(value map[string]any) (PolicyResult, error) {
	result := PolicyResult{Kind: stringValue(value, "kind"), Reason: stringValue(value, "reason")}
	result.Team, result.ResourceRID = stringValue(value, "team"), stringValue(value, "resourceRid")
	result.Role, result.Operation = stringValue(value, "role"), stringValue(value, "operation")
	if result.Kind == RequirementAny {
		alternatives, ok := value["alternatives"].([]any)
		if !ok {
			return PolicyResult{}, errors.New("require_any alternatives are invalid")
		}
		for _, raw := range alternatives {
			candidate, ok := raw.(map[string]any)
			if !ok {
				return PolicyResult{}, errors.New("require_any contains an invalid requirement")
			}
			decoded, err := decodePolicyResult(candidate)
			if err != nil {
				return PolicyResult{}, err
			}
			result.Alternatives = append(result.Alternatives, decoded)
		}
	}
	if !contains([]string{"APPROVE", "REJECT", "NO_OP", RequirementTeam, RequirementRole, RequirementOperation, RequirementAny}, result.Kind) {
		return PolicyResult{}, fmt.Errorf("unsupported policy result %q", result.Kind)
	}
	return result, nil
}

func stringValue(value map[string]any, key string) string {
	if value[key] == nil {
		return ""
	}
	return fmt.Sprint(value[key])
}

// PolicyInput is the CEL binding a policy expression is evaluated against. It
// is built here rather than by the caller so that every policy sees the same
// facts under the same names, whatever asked for the evaluation.
func PolicyInput(change ChangeFacts, author Principal, editors []string, now time.Time) map[string]any {
	resources := make(map[string]any, len(change.ReferencedResources))
	for name, resource := range change.ReferencedResources {
		resources[name] = map[string]any{
			"rid": resource.RID, "name": resource.Name, "labels": resource.Labels,
			"authorRoles": resource.AuthorRoles, "resource": resource.Resource,
		}
	}
	diff := make(map[string]any, len(change.Diff))
	for path, entry := range change.Diff {
		diff[path] = map[string]any{"before": entry.Before, "after": entry.After}
	}
	onBehalfOf := map[string]any{}
	if author.OnBehalfOf != nil {
		onBehalfOf = map[string]any{
			"username": author.OnBehalfOf.Username, "teams": author.OnBehalfOf.Teams,
			"serviceAccount": author.OnBehalfOf.ServiceAccount,
		}
	}
	metadata := change.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	return map[string]any{
		"type":       change.Type,
		"author":     map[string]any{"username": author.Username, "teams": author.Teams, "serviceAccount": author.ServiceAccount},
		"onBehalfOf": onBehalfOf, "editors": editors, "metadata": metadata, "request": change.Request,
		"diff": diff, "referencedResources": resources, "createdAt": now, "now": now,
	}
}
