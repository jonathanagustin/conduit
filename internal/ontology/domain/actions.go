package domain

import "strings"

// ActionRule is one server-enforced ontology action. The console does not
// decide it. A missing role or a blocking fact rejects the action.
type ActionRule struct {
	ID         string
	Roles      []string
	AuditEvent string
}

// ActionFacts are the observations the server has. Zero values fail closed
// where a rule needs a positive fact, so an unknown plan or run is not treated
// as clear.
type ActionFacts struct {
	Roles               []string
	WindowOpen          bool
	HasOverride         bool
	ApprovalsRecorded   int
	ApprovalsRequired   int
	AllArtifactsSigned  bool
	ConflictingRollout  bool
	ChangeStatus        string
	PlanStatus          string
	RunPhase            string
	RunKnown            bool
	ProfileStatus       string
	ProfileKnown        bool
	ArtifactStatus      string
	ArtifactKnown       bool
	SBOMCritical        int
	ChannelLabelsMet    bool
	CallerAlreadySigned bool
	RequirementPending  bool
	Confirmation        string
	ProfileID           string
}

// ActionDecision is the server's verdict and the audit event name to record.
type ActionDecision struct {
	Known      bool
	Allowed    bool
	Reason     string
	AuditEvent string
}

// LookupAction returns a rule from the server catalog.
func LookupAction(id string) (ActionRule, bool) {
	rule, ok := actionRules[id]
	return rule, ok
}

// Decide applies the role list and the blocking rule for one action.
func Decide(id string, facts ActionFacts) ActionDecision {
	rule, ok := actionRules[id]
	if !ok {
		return ActionDecision{Reason: "unknown action"}
	}
	decision := ActionDecision{Known: true, AuditEvent: rule.AuditEvent}
	if !roleAllowed(facts.Roles, rule.Roles) {
		decision.Reason = "caller does not have a role for this action"
		return decision
	}
	if reason := blockers[id](facts); reason != "" {
		decision.Reason = reason
		return decision
	}
	decision.Allowed = true
	decision.Reason = "action allowed"
	return decision
}

func roleAllowed(have, allowed []string) bool {
	for _, role := range have {
		for _, candidate := range allowed {
			if strings.EqualFold(strings.TrimSpace(role), candidate) {
				return true
			}
		}
	}
	return false
}

func quorumMet(facts ActionFacts) bool {
	return facts.ApprovalsRequired > 0 && facts.ApprovalsRecorded >= facts.ApprovalsRequired
}

func runCancellable(facts ActionFacts) bool {
	if !facts.RunKnown {
		return false
	}
	switch strings.ToUpper(facts.RunPhase) {
	case "QUEUED", "RUNNING", "STAGED":
		return true
	default:
		return false
	}
}

var actionRules = map[string]ActionRule{
	"execute_plan":           {ID: "execute_plan", Roles: []string{"Operator", "Release Approver"}, AuditEvent: "plan.execution_dispatched"},
	"promote_release_manual": {ID: "promote_release_manual", Roles: []string{"Release Approver", "Profile Owner"}, AuditEvent: "release_channel.manual_promoted"},
	"include_change_in_plan": {ID: "include_change_in_plan", Roles: []string{"Developer", "Operator", "Profile Owner"}, AuditEvent: "change_request.included_in_plan"},
	"download_bundle":        {ID: "download_bundle", Roles: []string{"Operator", "Profile Owner"}, AuditEvent: "artifact.bundle_downloaded"},
	"rerun_packaging_run":    {ID: "rerun_packaging_run", Roles: []string{"Operator", "Developer"}, AuditEvent: "packaging_run.rerun_requested"},
	"cancel_packaging_run":   {ID: "cancel_packaging_run", Roles: []string{"Operator", "Developer"}, AuditEvent: "packaging_run.canceled"},
	"fork":                   {ID: "fork", Roles: []string{"Developer", "Operator", "Profile Owner"}, AuditEvent: "work_profile.forked"},
	"upgrade":                {ID: "upgrade", Roles: []string{"Operator", "Profile Owner"}, AuditEvent: "work_profile.upgrade_staged"},
	"archive":                {ID: "archive", Roles: []string{"Profile Owner"}, AuditEvent: "work_profile.archived"},
	"delete":                 {ID: "delete", Roles: []string{"Profile Owner"}, AuditEvent: "work_profile.deleted"},
	"approve_plan":           {ID: "approve_plan", Roles: []string{"Release Approver"}, AuditEvent: "plan.approved"},
	"reject_plan":            {ID: "reject_plan", Roles: []string{"Release Approver", "Security Admin"}, AuditEvent: "plan.rejected"},
	"rerun_from_step":        {ID: "rerun_from_step", Roles: []string{"Operator", "Developer"}, AuditEvent: "packaging_run.rerun_from_step"},
	"cancel_run":             {ID: "cancel_run", Roles: []string{"Operator", "Developer"}, AuditEvent: "packaging_run.canceled"},
}

var blockers = map[string]func(ActionFacts) string{
	"execute_plan": func(facts ActionFacts) string {
		if !facts.WindowOpen && !facts.HasOverride {
			return "maintenance window is closed"
		}
		if !quorumMet(facts) {
			return "approval quorum is missing"
		}
		if !facts.AllArtifactsSigned {
			return "an artifact is unsigned"
		}
		if facts.ConflictingRollout {
			return "another plan is rolling out"
		}
		return ""
	},
	"promote_release_manual": func(facts ActionFacts) string {
		if !facts.AllArtifactsSigned {
			return "an artifact is unsigned"
		}
		if !facts.ChannelLabelsMet {
			return "channel label requirements are unmet"
		}
		if !quorumMet(facts) {
			return "approval quorum is missing"
		}
		if facts.ConflictingRollout {
			return "another plan is rolling out"
		}
		return ""
	},
	"include_change_in_plan": func(facts ActionFacts) string {
		if !strings.EqualFold(facts.ChangeStatus, "APPROVED") {
			return "change request is not approved"
		}
		if strings.EqualFold(facts.PlanStatus, "ROLLING_OUT") || facts.ConflictingRollout {
			return "target plan is rolling out"
		}
		return ""
	},
	"download_bundle": func(facts ActionFacts) string {
		if !facts.ArtifactKnown || (facts.ArtifactStatus != "SIGNED" && facts.ArtifactStatus != "PUBLISHED") {
			return "artifact is unsigned"
		}
		if facts.SBOMCritical > 0 {
			return "SBOM has an unresolved critical finding"
		}
		return ""
	},
	"rerun_packaging_run": func(facts ActionFacts) string {
		if !facts.RunKnown || strings.EqualFold(facts.RunPhase, "RUNNING") || strings.EqualFold(facts.PlanStatus, "ROLLING_OUT") {
			return "run is running or a linked plan is rolling out"
		}
		return ""
	},
	"cancel_packaging_run": func(facts ActionFacts) string {
		if !runCancellable(facts) {
			return "run is not queued, running, or staged"
		}
		return ""
	},
	"fork": func(facts ActionFacts) string {
		if !facts.ProfileKnown || strings.EqualFold(facts.ProfileStatus, "DELETING") {
			return "work profile is deleting or unknown"
		}
		return ""
	},
	"upgrade": func(facts ActionFacts) string {
		if !facts.ProfileKnown || !strings.EqualFold(facts.ProfileStatus, "UPGRADE_AVAILABLE") || !facts.AllArtifactsSigned {
			return "work profile is not ready for upgrade or the artifact is unsigned"
		}
		return ""
	},
	"archive": func(facts ActionFacts) string {
		if !facts.ProfileKnown || facts.ConflictingRollout {
			return "a linked plan is rolling out or the profile is unknown"
		}
		return ""
	},
	"delete": func(facts ActionFacts) string {
		if !facts.ProfileKnown || facts.ConflictingRollout || facts.Confirmation != facts.ProfileID || facts.ProfileID == "" {
			return "profile is rolling out or the confirmation does not match"
		}
		return ""
	},
	"approve_plan": func(facts ActionFacts) string {
		if facts.RequirementPending || facts.CallerAlreadySigned {
			return "a requirement is open or the caller already signed"
		}
		return ""
	},
	"reject_plan": func(facts ActionFacts) string {
		if strings.EqualFold(facts.PlanStatus, "SUCCEEDED") || strings.EqualFold(facts.PlanStatus, "REJECTED") {
			return "plan is already succeeded or rejected"
		}
		if facts.PlanStatus == "" {
			return "plan status is unknown"
		}
		return ""
	},
	"rerun_from_step": func(facts ActionFacts) string {
		if !facts.RunKnown || strings.EqualFold(facts.RunPhase, "RUNNING") || strings.EqualFold(facts.PlanStatus, "ROLLING_OUT") {
			return "run is running or a linked plan is rolling out"
		}
		return ""
	},
	"cancel_run": func(facts ActionFacts) string {
		if !runCancellable(facts) {
			return "run is not queued, running, or staged"
		}
		return ""
	},
}
