package controlplane

import (
	"context"
	"errors"
	reportadapter "github.com/jonathanagustin/conduit/internal/apollo/adapters/reporting"
	apolloapp "github.com/jonathanagustin/conduit/internal/apollo/application"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/apollo"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

func TestApolloRawAgentReportsSurviveAnotherReplicaAndNativeReport(t *testing.T) {
	for _, mode := range []string{"memory", "file"} {
		t.Run(mode, func(t *testing.T) {
			var repository Repository = NewMemoryRepository()
			reopen := func() Repository { return repository }
			if mode == "file" {
				root := t.TempDir()
				open := func() Repository {
					store, err := NewFileRepository(root)
					if err != nil {
						t.Fatal(err)
					}
					return store
				}
				repository, reopen = open(), open
			}
			assertApolloSharedAgentReports(t, repository, reopen)
		})
	}
}

func assertApolloSharedAgentReports(t *testing.T, repository Repository, reopen func() Repository) {
	t.Helper()
	ctx := context.Background()
	state := environmentInventoryFixture(t, repository)
	plan, err := (environmentReportingProjectionPort{repository: repository}).LatestPlan(ctx, state.Environment.ID)
	if err != nil || len(plan.Steps) == 0 {
		t.Fatalf("plan = %#v, error = %v", plan, err)
	}
	step := plan.Steps[0]
	group, name, found := strings.Cut(step.Product, ":")
	if !found {
		t.Fatalf("invalid fixture product %q", step.Product)
	}
	quorum := apollo.Quorum("ALL")
	namespace := "reported-namespace"
	nodes := []apollo.NodeInfo{{CreationTimestamp: time.Now().UTC(), HostId: "host", NodeId: "current", ProductVersion: step.Version}}
	info := []apollo.EntityInfo{{EntityType: "SERVICE", Id: step.Entity, ProductGroup: group, ProductName: name, Quorum: &quorum, NodesInfo: &nodes, K8sNamespace: &namespace}}
	writer := newApolloPublicAPI(repository, NewGovernanceService(repository), HandlerOptions{})
	if writer.initializationError != nil {
		t.Fatal(writer.initializationError)
	}
	if err := writer.application.PutEntityInfo(ctx, state.Environment.ID, info); err != nil {
		t.Fatal(err)
	}

	// A native Spoke report may replace the normalized view, but must not erase
	// the independently retained Apollo inputs needed by a later replica.
	report, err := repository.GetEnvironmentReport(ctx, state.Environment.ID)
	if err != nil {
		t.Fatal(err)
	}
	for index := range report.Entities {
		report.Entities[index].Namespace = step.Namespace
		report.Entities[index].Lifecycle = environments.LifecycleUnknown
		report.Entities[index].HealthDetails = nil
	}
	if _, err := repository.PutEnvironmentReport(ctx, report); err != nil {
		t.Fatal(err)
	}
	readerRepository := reopen()
	reader := newApolloPublicAPI(readerRepository, NewGovernanceService(readerRepository), HandlerOptions{})
	if reader.initializationError != nil {
		t.Fatal(reader.initializationError)
	}
	healthy := []apollo.ContainerStatus{{ContainerId: "container", Liveness: true, Readiness: true}}
	unhealthy := []apollo.ContainerStatus{{ContainerId: "container", Liveness: false, Readiness: false}}
	health := []apollo.EntityHealth{
		{ProductGroup: group, ProductName: name, ProductVersion: step.Version, NodeId: "current", ContainerStatuses: &healthy},
		{ProductGroup: group, ProductName: name, ProductVersion: step.Version, NodeId: "removed", ContainerStatuses: &unhealthy},
	}
	if err := reader.application.PutEntityHealth(ctx, state.Environment.ID, health); err != nil {
		t.Fatal(err)
	}
	stored, err := readerRepository.GetEnvironmentReport(ctx, state.Environment.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entity := range stored.Entities {
		if entity.InstallationID != step.InstallationID {
			continue
		}
		if entity.Health != environments.HealthHealthy || entity.Lifecycle != environments.LifecycleInstalled || entity.Namespace != namespace {
			t.Fatalf("second replica did not use the durable info: %#v", entity)
		}
		if entity.HealthDetails == nil || len(entity.HealthDetails.Nodes) != 1 || entity.HealthDetails.Nodes[0].NodeID != "current" {
			t.Fatalf("removed node affected the merged report: %#v", entity.HealthDetails)
		}
		return
	}
	t.Fatal("merged installation is missing")
}

func TestApolloAgentReportStorageAtomicUpdates(t *testing.T) {
	for _, mode := range []string{"memory", "file", "postgres"} {
		t.Run(mode, func(t *testing.T) {
			repository, reopen := agentReportRepository(t, mode)
			ctx := context.Background()
			port := environmentReportingProjectionPort{repository: repository}
			info := []apollo.EntityInfo{{Id: "entity", ProductGroup: "group", ProductName: "product", EntityType: "SERVICE"}}
			health := []apollo.EntityHealth{{NodeId: "node", ProductGroup: "group", ProductName: "product", ProductVersion: "1"}}
			// No Plan yet: save inputs without inventing a native report.
			var wait sync.WaitGroup
			errs := make(chan error, 2)
			ports := []environmentReportingProjectionPort{port, port}
			if mode == "postgres" {
				ports[1] = environmentReportingProjectionPort{repository: reopen()}
			}
			for index, patch := range []apolloapp.AgentReportPatch{{Info: &info}, {Health: &health}} {
				wait.Add(1)
				go func(port environmentReportingProjectionPort, patch apolloapp.AgentReportPatch) {
					defer wait.Done()
					errs <- reportadapter.New(port).UpdateAgentReports(ctx, "pending", patch, func(snapshot apolloapp.AgentReportSnapshot) (*environments.Report, error) { return nil, nil })
				}(ports[index], patch)
			}
			wait.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := repository.GetEnvironmentReport(ctx, "pending"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("raw inputs fabricated report: %v", err)
			}
			reports, err := repository.ListEnvironmentReports(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, report := range reports {
				if report.Environment == "pending" {
					t.Fatal("pending raw report leaked into native inventory")
				}
			}
			repository = reopen()
			port = environmentReportingProjectionPort{repository: repository}
			verify := func(snapshot apolloapp.AgentReportSnapshot) (*environments.Report, error) {
				if !reflect.DeepEqual(snapshot.Inputs.Info, info) || !reflect.DeepEqual(snapshot.Inputs.Health, health) {
					t.Fatalf("lost concurrent/restarted inputs: %#v", snapshot.Inputs)
				}
				return nil, nil
			}
			if err := reportadapter.New(port).UpdateAgentReports(ctx, "pending", apolloapp.AgentReportPatch{}, verify); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("projection failed")
			empty := []apollo.EntityInfo{}
			err = reportadapter.New(port).UpdateAgentReports(ctx, "pending", apolloapp.AgentReportPatch{Info: &empty}, func(snapshot apolloapp.AgentReportSnapshot) (*environments.Report, error) { return nil, failure })
			if !errors.Is(err, failure) {
				t.Fatalf("failure not propagated: %v", err)
			}
			if err := reportadapter.New(port).UpdateAgentReports(ctx, "pending", apolloapp.AgentReportPatch{}, verify); err != nil {
				t.Fatal(err)
			}
			if err := reportadapter.New(port).UpdateAgentReports(ctx, "pending", apolloapp.AgentReportPatch{Info: &empty}, func(snapshot apolloapp.AgentReportSnapshot) (*environments.Report, error) {
				if len(snapshot.Inputs.Info) != 0 || len(snapshot.Inputs.Health) != 1 {
					t.Fatalf("empty replacement was not retained: %#v", snapshot.Inputs)
				}
				return &environments.Report{Environment: "pending", AgentID: "test"}, nil
			}); err != nil {
				t.Fatal(err)
			}
			report, err := repository.GetEnvironmentReport(ctx, "pending")
			if err != nil || report.AgentID != "test" {
				t.Fatalf("projection not committed: %#v, %v", report, err)
			}
			for _, invalid := range []string{"../escape", "", "a/b"} {
				if err := reportadapter.New(port).UpdateAgentReports(ctx, invalid, apolloapp.AgentReportPatch{}, verify); !errors.Is(err, ErrNotFound) {
					t.Fatalf("invalid environment %q accepted: %v", invalid, err)
				}
			}
		})
	}
}

func TestPostgresApolloAgentReportsAcrossPools(t *testing.T) {
	repository, reopen := agentReportRepository(t, "postgres")
	assertApolloSharedAgentReports(t, repository, reopen)
}

func agentReportRepository(t *testing.T, mode string) (Repository, func() Repository) {
	t.Helper()
	switch mode {
	case "memory":
		repository := NewMemoryRepository()
		return repository, func() Repository { return repository }
	case "file":
		root := t.TempDir()
		open := func() Repository {
			repository, err := NewFileRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			return repository
		}
		return open(), open
	case "postgres":
		dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("CONDUIT_TEST_POSTGRES_DSN is required")
		}
		open := func() Repository {
			repository, err := NewPostgresRepository(context.Background(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = repository.Close() })
			return repository
		}
		repository := open().(*PostgresRepository)
		if _, err := repository.database.Exec(`TRUNCATE conduit_apollo_agent_reports; UPDATE conduit_repository_state SET generation = 0, state = '{}'::jsonb WHERE singleton = TRUE`); err != nil {
			t.Fatal(err)
		}
		return repository, open
	default:
		t.Fatal("unsupported repository mode")
		return nil, nil
	}
}

func TestPostgresApolloProjectionRollsBackWhenRawWriteFails(t *testing.T) {
	repository, _ := agentReportRepository(t, "postgres")
	postgres := repository.(*PostgresRepository)
	ctx := context.Background()
	if _, err := postgres.database.Exec(`CREATE FUNCTION reject_apollo_report() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected raw write failure'; END $$;
 CREATE TRIGGER reject_apollo_report BEFORE INSERT OR UPDATE ON conduit_apollo_agent_reports FOR EACH ROW EXECUTE FUNCTION reject_apollo_report()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = postgres.database.Exec(`DROP TRIGGER IF EXISTS reject_apollo_report ON conduit_apollo_agent_reports; DROP FUNCTION IF EXISTS reject_apollo_report()`)
	})
	port := environmentReportingProjectionPort{repository: repository}
	err := reportadapter.New(port).UpdateAgentReports(ctx, "rollback", apolloapp.AgentReportPatch{}, func(apolloapp.AgentReportSnapshot) (*environments.Report, error) {
		return &environments.Report{Environment: "rollback", AgentID: "must-not-commit"}, nil
	})
	if err == nil {
		t.Fatal("raw storage failure was swallowed")
	}
	if _, err := repository.GetEnvironmentReport(ctx, "rollback"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("normalized report escaped rollback: %v", err)
	}
	var count int
	if err := postgres.database.QueryRow(`SELECT count(*) FROM conduit_apollo_agent_reports WHERE environment = 'rollback'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial raw input committed: %d, %v", count, err)
	}
}
