package controlplane

import (
	"encoding/json"
	"time"

	override "github.com/jonathanagustin/conduit/internal/installoverride/domain"
)

type moduleOverrideIdentityProof struct {
	SchemaVersion int               `json:"schemaVersion"`
	Subject       string            `json:"subject"`
	Identity      override.Identity `json:"identity"`
	ReviewedAt    time.Time         `json:"reviewedAt"`
}

func recordModuleOverrideReviewer(request *ChangeRequest, principal Principal, eligible map[string]struct{}, reviewedAt time.Time) error {
	if request.TargetKind != ModuleOverrideTarget {
		return nil
	}
	if _, ok := eligible["module-override:"+request.TargetID]; !ok {
		return nil
	}
	if len(request.Changes) != 1 || request.Changes[0].Type != ModuleOverrideTarget {
		return ErrConflict
	}
	proof := moduleOverrideIdentityProof{SchemaVersion: 1, Subject: principal.Username, Identity: override.Identity{Actor: principal.Username, Effective: effectivePrincipal(principal).Username}, ReviewedAt: reviewedAt}
	if proof.Identity.Validate() != nil {
		return ErrConflict
	}
	encoded, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	if request.Changes[0].Metadata == nil {
		request.Changes[0].Metadata = map[string]any{}
	}
	key := "moduleOverrideReviewer:" + principal.Username
	if request.Changes[0].Metadata[key] != nil {
		return ErrConflict
	}
	request.Changes[0].Metadata[key] = string(encoded)
	return nil
}

func moduleOverrideReviewerProof(request ChangeRequest, subject string, reviewedAt time.Time) (moduleOverrideIdentityProof, error) {
	var proof moduleOverrideIdentityProof
	if len(request.Changes) != 1 {
		return proof, ErrConflict
	}
	document, ok := request.Changes[0].Metadata["moduleOverrideReviewer:"+subject].(string)
	if !ok || len(document) > 4096 || json.Unmarshal([]byte(document), &proof) != nil || proof.SchemaVersion != 1 || proof.Subject != subject || proof.Identity.Actor != subject || proof.Identity.Validate() != nil || !proof.ReviewedAt.Equal(reviewedAt) {
		return proof, ErrConflict
	}
	return proof, nil
}
