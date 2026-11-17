package controlplane

import (
	"context"
	"errors"
	"testing"

	governanceports "github.com/jonathanagustin/conduit/internal/governance/ports"
)

type persistedOpeningProbe struct {
	t        *testing.T
	reader   Repository
	openings []governanceports.ChangeRequestOpening
}

func (probe *persistedOpeningProbe) AwaitingApproval(ctx context.Context, opening governanceports.ChangeRequestOpening) error {
	stored, err := probe.reader.GetChangeRequest(ctx, opening.ID)
	if err != nil || stored.Status != ChangePendingApprovals || stored.Revision != opening.Revision {
		probe.t.Errorf("announcement must follow the commit: opening=%#v stored=%#v error=%v", opening, stored, err)
	}
	probe.openings = append(probe.openings, opening)
	return errors.New("an unavailable optional notification destination must not undo this commit")
}

func TestSerializedOverrideProposalsAnnounceAfterCommitAndSkipPreviews(t *testing.T) {
	for _, kind := range []string{"memory", "file"} {
		for _, surface := range []string{"module", "maintenance"} {
			t.Run(kind+"/"+surface, func(t *testing.T) {
				var repository Repository = NewMemoryRepository()
				if kind == "file" {
					var err error
					repository, err = NewFileRepository(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
				}
				probe := &persistedOpeningProbe{t: t, reader: repository}
				switch surface {
				case "module":
					service, _, _, draft, _ := moduleOverrideFixture(t, repository)
					service.options.Announcements.ChangeRequestOpenings = probe
					if _, err := service.ProposeModuleOverride(t.Context(), draft, true); err != nil {
						t.Fatal(err)
					}
					if len(probe.openings) != 0 {
						t.Fatal("preview announced a proposal", probe.openings)
					}
					result, err := service.ProposeModuleOverride(t.Context(), draft, false)
					if err != nil || len(probe.openings) != 1 || probe.openings[0].ID != result.ReviewID {
						t.Fatalf("committed module review: result=%#v openings=%#v error=%v", result, probe.openings, err)
					}
				case "maintenance":
					service, _, _, _, _ := temporaryMaintenanceFixture(t, repository, "prod", false)
					service.options.Announcements.ChangeRequestOpenings = probe
					draft := temporaryDraft("prod")
					draft.DryRun = true
					if _, err := service.CreateMaintenanceOverrides(t.Context(), draft); err != nil {
						t.Fatal(err)
					}
					if len(probe.openings) != 0 {
						t.Fatal("preview announced a proposal", probe.openings)
					}
					draft.DryRun = false
					result, err := service.CreateMaintenanceOverrides(t.Context(), draft)
					if err != nil || len(result.Requests) != 1 || len(probe.openings) != 1 || probe.openings[0].ID != result.Requests[0].ChangeRequestID {
						t.Fatalf("committed maintenance review: result=%#v openings=%#v error=%v", result, probe.openings, err)
					}
				}
			})
		}
	}
}

func TestRolledBackOverrideProposalsAnnounceNothing(t *testing.T) {
	for _, surface := range []string{"module", "maintenance"} {
		t.Run(surface, func(t *testing.T) {
			base := NewMemoryRepository()
			failure := errors.New("audit cannot commit")
			failing := &failingModuleAuditRepository{Repository: base, transaction: base, action: "MODULE_OVERRIDE_PROPOSED", failure: failure}
			probe := &persistedOpeningProbe{t: t, reader: base}
			var err error
			if surface == "module" {
				service, _, _, draft, _ := moduleOverrideFixture(t, failing)
				service.options.Announcements.ChangeRequestOpenings = probe
				_, err = service.ProposeModuleOverride(t.Context(), draft, false)
			} else {
				failing.action = "TEMPORARY_ENTITY_MAINTENANCE_PROPOSED"
				service, _, _, _, _ := temporaryMaintenanceFixture(t, failing, "prod", false)
				service.options.Announcements.ChangeRequestOpenings = probe
				_, err = service.CreateMaintenanceOverrides(t.Context(), temporaryDraft("prod"))
			}
			if !errors.Is(err, failure) {
				t.Fatalf("expected failed transaction, got %v", err)
			}
			stored, err := base.ListChangeRequests(t.Context(), "prod")
			if err != nil || len(stored) != 0 || len(probe.openings) != 0 {
				t.Fatalf("rollback escaped as a notification: requests=%#v openings=%#v error=%v", stored, probe.openings, err)
			}
		})
	}
}
