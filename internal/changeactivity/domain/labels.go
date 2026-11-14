package domain

// LabelMutation is the native result of governed add/remove or its preview.
// CONDUIT-INVENTED: changeactivity.LabelMutations
// Displaced-by: a published or sanitized Change Request label mutation API establishing atomic update, preview, authorization and result semantics.
type LabelMutation struct {
	ChangeRequestID    string   `json:"changeRequestId"`
	Labels             []string `json:"labels"`
	ResultingStatus    string   `json:"resultingStatus"`
	PlanID             string   `json:"planId,omitempty"`
	PlanStatus         string   `json:"planStatus,omitempty"`
	ApprovalTransition bool     `json:"approvalTransition"`
	Mode               string   `json:"mode"`
	ValueTier          string   `json:"valueTier"`
}
