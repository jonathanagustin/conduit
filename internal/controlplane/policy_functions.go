package controlplane

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Change metadata sources. Apollo's trusted-request predicates recognize
// changes opened by its own Module deployment and Environment import
// workflows. Platform marks the equivalent origins explicitly so a policy can
// distinguish automation from an operator editing the same resource.
//
// Provenance (reviewed 2026-08-02):
// https://www.palantir.com/docs/apollo/cel-policies/cel-functions/
const (
	ChangeSourceModuleInstallation = "MODULE_INSTALLATION"
	ChangeSourceEnvironmentImport  = "ENVIRONMENT_IMPORT"

	MetadataChangeSource = "changeSource"
	MetadataModuleID     = "moduleId"
	MetadataBundleID     = "bundleId"
)

// TrustedServiceAccount names one automation identity and the Change Types it
// is allowed to auto-approve.
type TrustedServiceAccount struct {
	Username    string   `json:"username"`
	ChangeTypes []string `json:"changeTypes"`
}

// PolicyTrust is the deployment-owned trust configuration the CEL predicates
// read. Apollo hardcodes its own first-party service accounts; Platform has no
// standing to hardcode identities it does not own, so the operator declares
// them and an empty configuration denies every auto-approval.
type PolicyTrust struct {
	ServiceAccounts []TrustedServiceAccount `json:"serviceAccounts,omitempty"`
	ModuleService   string                  `json:"moduleService,omitempty"`
	ImportService   string                  `json:"importService,omitempty"`
}

func (trust PolicyTrust) accountFor(username string) (TrustedServiceAccount, bool) {
	for _, account := range trust.ServiceAccounts {
		if account.Username != "" && account.Username == username {
			return account, true
		}
	}
	return TrustedServiceAccount{}, false
}

// policyPrincipal is the subset of author/onBehalfOf a predicate needs.
type policyPrincipal struct {
	Username       string
	ServiceAccount bool
}

func decodePolicyPrincipal(value ref.Val) policyPrincipal {
	fields, ok := refToMap(value)
	if !ok {
		return policyPrincipal{}
	}
	principal := policyPrincipal{Username: fmt.Sprint(orEmpty(fields["username"]))}
	if serviceAccount, isBool := fields["serviceAccount"].(bool); isBool {
		principal.ServiceAccount = serviceAccount
	}
	return principal
}

func orEmpty(value any) any {
	if value == nil {
		return ""
	}
	return value
}

func refToMap(value ref.Val) (map[string]any, bool) {
	native, err := value.ConvertToNative(reflect.TypeOf(map[string]any{}))
	if err != nil {
		return nil, false
	}
	fields, ok := native.(map[string]any)
	return fields, ok
}

func refToStrings(value ref.Val) []string {
	native, err := value.ConvertToNative(reflect.TypeOf([]string{}))
	if err != nil {
		return nil
	}
	strs, ok := native.([]string)
	if !ok {
		return nil
	}
	return strs
}

// onlyEditedBy reports whether nobody other than the named user has edited the
// change request since it was opened. Every published trusted-request predicate
// requires this, so a human amending an automated proposal withdraws its
// auto-approval instead of inheriting it.
func onlyEditedBy(username string, editors []string) bool {
	if username == "" {
		return false
	}
	for _, editor := range editors {
		if strings.TrimSpace(editor) == "" {
			continue
		}
		if editor != username {
			return false
		}
	}
	return true
}

// trustedRequestAuthor resolves the acting identity: onBehalfOf when a
// delegated identity is present, otherwise the author.
func trustedRequestAuthor(author, onBehalfOf policyPrincipal) policyPrincipal {
	if onBehalfOf.Username != "" {
		return onBehalfOf
	}
	return author
}

// pathMatchesAnyPattern reports whether a changed diff path is permitted by at
// least one glob pattern. Paths use "/" as a separator, "*" matches exactly one
// key, and "**" matches one or more nested keys.
//
// "**" deliberately does not match zero segments: this function is an allowlist
// gate, so permitting "foo/**" must not silently permit replacing "foo"
// wholesale, which is a strictly broader change than editing something nested
// under it.
func pathMatchesAnyPattern(path string, patterns []string) bool {
	segments := strings.Split(path, "/")
	for _, pattern := range patterns {
		if matchPathSegments(segments, strings.Split(pattern, "/")) {
			return true
		}
	}
	return false
}

func matchPathSegments(path, pattern []string) bool {
	if len(pattern) == 0 {
		return len(path) == 0
	}
	if pattern[0] == "**" {
		for consumed := 1; consumed <= len(path); consumed++ {
			if matchPathSegments(path[consumed:], pattern[1:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 {
		return false
	}
	if pattern[0] != "*" && pattern[0] != path[0] {
		return false
	}
	return matchPathSegments(path[1:], pattern[1:])
}

// policyPredicates registers the published boolean CEL predicates. They are
// pure functions of the policy input plus the operator's declared trust
// configuration, so a policy authored against the public contract evaluates the
// same way here.
func policyPredicates(trust PolicyTrust) []cel.EnvOption {
	principalArgs := []*cel.Type{cel.DynType, cel.DynType, cel.ListType(cel.StringType)}
	return []cel.EnvOption{
		cel.Function("should_auto_approve_service_user",
			cel.Overload("should_auto_approve_service_user_impl",
				append([]*cel.Type{cel.StringType}, principalArgs...), cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					if len(args) != 4 {
						return types.Bool(false)
					}
					changeType := fmt.Sprint(args[0].Value())
					acting := trustedRequestAuthor(decodePolicyPrincipal(args[1]), decodePolicyPrincipal(args[2]))
					if !acting.ServiceAccount || !onlyEditedBy(acting.Username, refToStrings(args[3])) {
						return types.Bool(false)
					}
					account, trusted := trust.accountFor(acting.Username)
					return types.Bool(trusted && contains(account.ChangeTypes, changeType))
				}))),
		cel.Function("is_author_service_user",
			cel.Overload("is_author_service_user_impl",
				append([]*cel.Type{cel.StringType}, principalArgs...), cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					if len(args) != 4 {
						return types.Bool(false)
					}
					username := fmt.Sprint(args[0].Value())
					author, onBehalfOf := decodePolicyPrincipal(args[1]), decodePolicyPrincipal(args[2])
					editors := refToStrings(args[3])
					for _, candidate := range []policyPrincipal{author, onBehalfOf} {
						if candidate.Username == "" || candidate.Username != username {
							continue
						}
						if candidate.ServiceAccount && onlyEditedBy(candidate.Username, editors) {
							return types.Bool(true)
						}
					}
					return types.Bool(false)
				}))),
		cel.Function("is_trusted_module_request",
			cel.Overload("is_trusted_module_request_impl", principalArgs, cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return types.Bool(trustedAutomationRequest(
						trust.ModuleService, ChangeSourceModuleInstallation, MetadataModuleID, args...))
				}))),
		cel.Function("is_trusted_import_request",
			cel.Overload("is_trusted_import_request_impl", principalArgs, cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return types.Bool(trustedAutomationRequest(
						trust.ImportService, ChangeSourceEnvironmentImport, "", args...))
				}))),
		cel.Function("diff_only_modifies_paths",
			cel.Overload("diff_only_modifies_paths_impl",
				[]*cel.Type{cel.DynType, cel.ListType(cel.StringType)}, cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					if len(args) != 2 {
						return types.Bool(false)
					}
					diff, ok := refToMap(args[0])
					if !ok {
						return types.Bool(false)
					}
					patterns := refToStrings(args[1])
					if len(patterns) == 0 {
						return types.Bool(len(diff) == 0)
					}
					for path := range diff {
						if !pathMatchesAnyPattern(path, patterns) {
							return types.Bool(false)
						}
					}
					return types.Bool(true)
				}))),
	}
}

// trustedAutomationRequest is the shared shape of is_trusted_module_request and
// is_trusted_import_request: the declared automation identity opened the change,
// the change carries the matching origin metadata, and nobody else edited it.
func trustedAutomationRequest(
	serviceUsername, changeSource, requiredMetadataKey string, args ...ref.Val,
) bool {
	if serviceUsername == "" || len(args) != 3 {
		return false
	}
	acting := trustedRequestAuthor(decodePolicyPrincipal(args[0]), policyPrincipal{})
	if acting.Username != serviceUsername || !acting.ServiceAccount ||
		!onlyEditedBy(acting.Username, refToStrings(args[2])) {
		return false
	}
	metadata, ok := refToMap(args[1])
	if !ok {
		return false
	}
	if fmt.Sprint(orEmpty(metadata[MetadataChangeSource])) != changeSource {
		return false
	}
	if requiredMetadataKey == "" {
		return true
	}
	return strings.TrimSpace(fmt.Sprint(orEmpty(metadata[requiredMetadataKey]))) != ""
}
