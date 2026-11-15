package controlplane

import "testing"

func TestPostgresReadOnlyStatusVerificationDetectsTampering(t *testing.T) {
	repository, _ := auditRepositoryForTest(t)
	if _, err := repository.AppendAuthorizationAudit(t.Context(), repositoryAuditEvent("status-event")); err != nil {
		t.Fatal(err)
	}
	first, err := repository.VerifyAuthorizationAudit(t.Context())
	if err != nil || first.Sequence != 1 {
		t.Fatalf("stored-chain verification: %+v %v", first, err)
	}
	second, err := repository.VerifyAuthorizationAudit(t.Context())
	if err != nil || second != first {
		t.Fatalf("read-only verification changed the checkpoint: %+v %v", second, err)
	}
	if _, err = repository.database.ExecContext(t.Context(), "UPDATE conduit_authorization_audit_events SET resource_rid='tampered' WHERE sequence=1"); err == nil {
		t.Fatal("ordinary DML bypassed append-only storage protection")
	}
	// Privileged storage-owner corruption is a separate threat from ordinary
	// application DML. Disable triggers only in this isolated fixture database.
	for _, statement := range []string{
		"ALTER TABLE conduit_authorization_audit_events DISABLE TRIGGER USER",
		"UPDATE conduit_authorization_audit_events SET resource_rid='tampered' WHERE sequence=1",
		"ALTER TABLE conduit_authorization_audit_events ENABLE TRIGGER USER",
	} {
		if _, err = repository.database.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = repository.VerifyAuthorizationAudit(t.Context()); err == nil {
		t.Fatal("status verifier accepted inconsistent stored chain bytes")
	}
}
