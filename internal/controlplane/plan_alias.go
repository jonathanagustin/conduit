package controlplane

import "github.com/jonathanagustin/conduit/internal/delivery/plan"

// The Plan — what a Hub hands a Spoke, and everything it carries — now lives in
// internal/delivery/plan. These are type aliases rather than conversions
// because a Plan is serialized: it is persisted and served over HTTP, so its
// JSON is a contract. An alias is the same type, so the bytes are unchanged by
// construction rather than by a conversion somebody has to keep correct.
type (
	Plan                  = plan.Plan
	PlanStep              = plan.Step
	SecretMutation        = plan.Mutation
	SecretAccess          = plan.Access
	MaintenanceConstraint = plan.MaintenanceConstraint
	MaintenanceOverride   = plan.Override
	UpgradeRationale      = plan.UpgradeRationale
	IgnoredNamespace      = plan.IgnoredNamespace
)

const (
	SecretCreate  = plan.SecretCreate
	SecretReplace = plan.SecretReplace
	SecretVerify  = plan.SecretVerify
	SecretDelete  = plan.SecretDelete
)
