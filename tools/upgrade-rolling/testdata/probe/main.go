// The same fixture is linked against the old and current production repository.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 || (os.Args[1] != "seed" && os.Args[1] != "read") {
		return fmt.Errorf("expected seed or read")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	repo, err := controlplane.NewPostgresRepository(ctx, os.Getenv("CONDUIT_UPGRADE_DSN"))
	if err != nil {
		return fmt.Errorf("open fixture repository failed")
	}
	defer repo.Close()
	if os.Args[1] == "seed" {
		foundation, err := os.ReadFile(os.Getenv("CONDUIT_UPGRADE_FOUNDATION"))
		if err != nil {
			return fmt.Errorf("read foundation fixture failed")
		}
		epoch := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
		err = repo.Transaction(ctx, func(tx controlplane.Repository) error {
			for i := 0; i < 500; i++ {
				_, err := tx.PutTeam(ctx, controlplane.Team{Name: fmt.Sprintf("seed-team-%04d", i), DisplayName: fmt.Sprintf("Seed team %d", i), MemberUsers: []string{fmt.Sprintf("seed-user-%04d", i)}, CreatedAt: epoch, UpdatedAt: epoch, Revision: 1})
				if err != nil {
					return fmt.Errorf("seed team %d failed", i)
				}
			}
			for i := 0; i < 1000; i++ {
				event := governance.NewAuthorizationCheckEvent(epoch.Add(time.Duration(i)*time.Second), fmt.Sprintf("seed-user-%04d", i), "read", "rolling-baseline-audit", governance.DecisionAllow, fmt.Sprintf("seed %04d", i), nil)
				if _, err := tx.AppendAuthorizationAudit(ctx, event); err != nil {
					return fmt.Errorf("seed audit %d failed", i)
				}
			}
			for i := 0; i < 100; i++ {
				var state controlplane.DesiredState
				if err := json.Unmarshal(foundation, &state); err != nil {
					return fmt.Errorf("decode foundation failed")
				}
				state.Environment.ID = fmt.Sprintf("seed-%04d", i)
				if _, err := controlplane.NewLifecycle(tx).CreatePlan(ctx, state); err != nil {
					return fmt.Errorf("seed valid plan %d failed", i)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	snapshot := map[string]any{}
	teams, err := repo.ListTeams(ctx)
	if err != nil {
		return fmt.Errorf("read teams failed")
	}
	teamCount := 0
	for _, team := range teams {
		if strings.HasPrefix(team.Name, "seed-team-") {
			snapshot["team/"+team.Name] = team
			teamCount++
		}
	}
	if teamCount != 500 {
		return fmt.Errorf("seed team count changed")
	}
	audits, err := repo.ListAuthorizationAudit(ctx, "rolling-baseline-audit")
	if err != nil || len(audits) != 1000 {
		return fmt.Errorf("seed audit count changed")
	}
	for _, audit := range audits {
		snapshot["audit/"+audit.ID] = audit
	}
	for i := 0; i < 100; i++ {
		plans, err := repo.ListPlans(ctx, fmt.Sprintf("seed-%04d", i))
		if err != nil || len(plans) != 1 {
			return fmt.Errorf("seed plan count changed for %d", i)
		}
		snapshot["plan/"+plans[0].ID] = plans[0]
	}
	return json.NewEncoder(os.Stdout).Encode(snapshot)
}
