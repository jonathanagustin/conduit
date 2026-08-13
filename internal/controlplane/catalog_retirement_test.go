package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogRetirementPreconditionsAndImmutableHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	active := catalogPlan("pln_active", "prod", now, "app.example:api", "1.2.3", true)
	if _, err := repository.CreatePlan(ctx, active); err != nil {
		t.Fatal(err)
	}
	service := NewCatalogRetirementService(repository)
	service.now = func() time.Time { return now.Add(time.Hour) }
	if _, err := service.Prepare(ctx, CatalogRetirementRelease, "app.example:api", "1.2.3", "creator", "obsolete release"); !errors.Is(err, ErrConflict) {
		t.Fatalf("active Entity retirement error = %v", err)
	}
	if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
		Environment: "prod", AgentID: "agent", ObservedAt: now,
		Entities: []ReportedEntity{{InstallationID: "api", Product: "app.example:api", Version: "1.2.3", Lifecycle: LifecycleInstalled}},
	}); err != nil {
		t.Fatal(err)
	}
	inactive := catalogPlan("pln_inactive", "prod", now.Add(time.Minute), "app.example:other", "2.0.0", false)
	if _, err := repository.CreatePlan(ctx, inactive); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Prepare(ctx, CatalogRetirementRelease, "app.example:api", "1.2.3", "creator", "obsolete release"); !errors.Is(err, ErrConflict) {
		t.Fatalf("reported Entity retirement error = %v", err)
	}
	if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
		Environment: "prod", AgentID: "agent", ObservedAt: now.Add(2 * time.Minute),
		Entities: []ReportedEntity{{InstallationID: "api", Product: "app.example:api", Version: "1.2.3", Lifecycle: LifecycleUninstalled}},
	}); err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(ctx, CatalogRetirementRelease, "app.example:api", "1.2.3", "creator", "obsolete release")
	if err != nil {
		t.Fatal(err)
	}
	prepared.ChangeRequestID = "crq_retire"
	stored, err := repository.CreateCatalogRetirement(ctx, prepared)
	if err != nil || stored.Revision != 1 {
		t.Fatalf("retirement = %#v, %v", stored, err)
	}
	if _, err := repository.GetPlan(ctx, active.ID); err != nil {
		t.Fatalf("historical Plan was erased: %v", err)
	}
	if report, err := repository.GetEnvironmentReport(ctx, "prod"); err != nil || len(report.Entities) != 1 {
		t.Fatalf("reported history = %#v, %v", report, err)
	}
	if _, err := repository.CreatePlan(ctx, catalogPlan("pln_reuse", "other", now.Add(2*time.Hour), "app.example:api", "1.2.3", true)); !errors.Is(err, ErrConflict) {
		t.Fatalf("retired coordinate reuse error = %v", err)
	}
	dependent := catalogPlan("pln_dependency_reuse", "other", now.Add(3*time.Hour), "app.example:consumer", "2.0.0", false)
	dependent.DesiredState.Releases[0].Dependencies = []Dependency{{Product: "app.example:api", MinimumVersion: "1.0.0", MaximumVersion: "2.x.x"}}
	if _, err := repository.CreatePlan(ctx, dependent); !errors.Is(err, ErrConflict) {
		t.Fatalf("retired dependency reuse error = %v", err)
	}
}

func TestCatalogRetirementRejectsLiveDependency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	history := catalogPlan("pln_history", "old", now, "app.example:library", "1.4.0", false)
	if _, err := repository.CreatePlan(ctx, history); err != nil {
		t.Fatal(err)
	}
	dependent := catalogPlan("pln_dependent", "prod", now.Add(time.Minute), "app.example:consumer", "2.0.0", false)
	dependent.DesiredState.Releases[0].Dependencies = []Dependency{{Product: "app.example:library", MinimumVersion: "1.0.0", MaximumVersion: "2.x.x"}}
	if _, err := repository.CreatePlan(ctx, dependent); err != nil {
		t.Fatal(err)
	}
	service := NewCatalogRetirementService(repository)
	if _, err := service.Prepare(ctx, CatalogRetirementRelease, "app.example:library", "1.4.0", "creator", "remove old library"); !errors.Is(err, ErrConflict) {
		t.Fatalf("dependency retirement error = %v", err)
	}
}

func TestCatalogRetirementPersistsAndBlocksPublicRepublish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if _, err := repository.CreatePlan(ctx, catalogPlan("pln_history", "prod", now, "app.example:api", "1.2.3", false)); err != nil {
		t.Fatal(err)
	}
	retirement := CatalogRetirement{
		Scope: CatalogRetirementRelease, Product: "app.example:api", Version: "1.2.3", Rationale: "obsolete",
		RetiredBy: "creator", ChangeRequestID: "crq_retire", RetiredAt: now.Add(time.Hour), Revision: 1,
	}
	if _, err := repository.CreateCatalogRetirement(ctx, retirement); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if stored, err := reopened.GetCatalogRetirement(ctx, retirement.Product, retirement.Version); err != nil || stored.ChangeRequestID != "crq_retire" {
		t.Fatalf("reopened retirement = %#v, %v", stored, err)
	}
	api := newApolloPublicAPI(reopened, NewGovernanceService(reopened), HandlerOptions{})
	response := httptest.NewRecorder()
	api.publish(ctx, response, "app.example:api:1.2.3", nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("retired public republish = %d %s", response.Code, response.Body.String())
	}
}

func TestProductRetirementBlocksModulePublicationAndInstallation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if _, err := repository.CreatePlan(ctx, catalogPlan("pln_history", "prod", now, "app.example:api", "1.2.3", false)); err != nil {
		t.Fatal(err)
	}
	retirement := CatalogRetirement{
		Scope: CatalogRetirementProduct, Product: "app.example:api", Rationale: "retire product",
		RetiredBy: "creator", ChangeRequestID: "crq_product", RetiredAt: now.Add(time.Hour), Revision: 1,
	}
	if _, err := repository.CreateCatalogRetirement(ctx, retirement); err != nil {
		t.Fatal(err)
	}
	locked := LockedModuleEntity{ProductID: retirement.Product}
	if _, err := repository.CreateModuleRelease(ctx, ModuleRelease{
		Name: "foundation", Version: "1.0.0", Digest: "sha256:test", Lock: ModuleLock{Entities: []LockedModuleEntity{locked}},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Module publication against retired Product = %v", err)
	}
	if _, err := repository.CreateModuleInstallation(ctx, ModuleInstallation{
		ID: "modi_foundation", Module: "foundation", Environment: "prod",
		Entities: []InstalledModuleEntity{{ProductID: retirement.Product}},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Module installation against retired Product = %v", err)
	}
}

func TestCatalogRetirementHTTPUsesProductCreatorAndChangeRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if _, err := repository.CreatePlan(ctx, catalogPlan("pln_history", "prod", now, "app.example:api", "1.2.3", false)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreatePlan(ctx, catalogPlan("pln_product_history", "stage", now, "app.example:worker", "4.5.6", false)); err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "creators", DisplayName: "Creators", AdminUsers: []string{"creator"}, MemberUsers: []string{"creator"}},
		{Name: "product-creators", DisplayName: "Product creators", AdminUsers: []string{"product-creator"}, MemberUsers: []string{"product-creator"}},
		{Name: "viewers", DisplayName: "Viewers", AdminUsers: []string{"viewer"}, MemberUsers: []string{"viewer"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []ResourceGrant{
		{ResourceRID: productRID("app.example:api"), Team: "creators", Roles: []string{"product:release-creator"}},
		{ResourceRID: productRID("app.example:api"), Team: "viewers", Roles: []string{"product:viewer"}},
		{ResourceRID: productRID("app.example:worker"), Team: "product-creators", Roles: []string{"product:creator"}},
	} {
		if _, err := governance.PutGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{
		ID: "retirement", DisplayName: "Retirement", Enabled: true,
		Expression: `(type == "RETIRE_PRODUCT_RELEASE" || type == "RETIRE_PRODUCT") ? approve("creator may retire unused coordinate") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	path := "/v1/products/app.example:api/releases/1.2.3/retire"
	denied := governanceRequest(t, handler, http.MethodPost, path, "viewer", "", map[string]string{"rationale": "obsolete"})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer retirement = %d %s", denied.Code, denied.Body.String())
	}
	response := governanceRequest(t, handler, http.MethodPost, path, "creator", "", map[string]string{"rationale": "obsolete"})
	if response.Code != http.StatusCreated {
		t.Fatalf("creator retirement = %d %s", response.Code, response.Body.String())
	}
	var result struct {
		ChangeRequest ChangeRequest     `json:"changeRequest"`
		Retirement    CatalogRetirement `json:"retirement"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.ChangeRequest.Execution == nil ||
		result.ChangeRequest.Execution.Status != ExecutionSucceeded || result.Retirement.ChangeRequestID != result.ChangeRequest.ID {
		t.Fatalf("retirement response = %#v, %v", result, err)
	}
	productResponse := governanceRequest(t, handler, http.MethodPost, "/v1/products/app.example:worker/retire", "product-creator", "", map[string]string{"rationale": "retire obsolete Product"})
	if productResponse.Code != http.StatusCreated {
		t.Fatalf("Product creator retirement = %d %s", productResponse.Code, productResponse.Body.String())
	}
	if stored, err := repository.GetCatalogRetirement(ctx, "app.example:worker", ""); err != nil || stored.Scope != CatalogRetirementProduct {
		t.Fatalf("governed Product retirement = %#v, %v", stored, err)
	}
}

func TestCatalogRetirementAppliesAfterRequiredReview(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if _, err := repository.CreatePlan(ctx, catalogPlan("pln_history", "prod", now, "app.example:api", "1.2.3", false)); err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "creators", DisplayName: "Creators", AdminUsers: []string{"creator"}, MemberUsers: []string{"creator"}},
		{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: productRID("app.example:api"), Team: "creators", Roles: []string{"product:release-creator"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{
		ID: "retirement-review", DisplayName: "Retirement review", Enabled: true,
		Expression: `type == "RETIRE_PRODUCT_RELEASE" ? require_team("reviewers", "review permanent retirement") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := governanceRequest(t, handler, http.MethodPost, "/v1/products/app.example:api/releases/1.2.3/retire", "creator", "", map[string]string{"rationale": "obsolete"})
	var proposed struct {
		ChangeRequest ChangeRequest `json:"changeRequest"`
	}
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &proposed) != nil || proposed.ChangeRequest.Status != ChangePendingApprovals {
		t.Fatalf("pending retirement = %d %#v %s", response.Code, proposed, response.Body.String())
	}
	if _, err := repository.GetCatalogRetirement(ctx, "app.example:api", "1.2.3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pending retirement was applied: %v", err)
	}
	approved := governanceRequest(t, handler, http.MethodPost, "/v1/change-requests/"+proposed.ChangeRequest.ID+"/approve", "reviewer", "", map[string]string{"rationale": "approved"})
	if approved.Code != http.StatusOK {
		t.Fatalf("retirement approval = %d %s", approved.Code, approved.Body.String())
	}
	stored, err := repository.GetCatalogRetirement(ctx, "app.example:api", "1.2.3")
	if err != nil || stored.ChangeRequestID != proposed.ChangeRequest.ID {
		t.Fatalf("approved retirement = %#v, %v", stored, err)
	}
	request, err := repository.GetChangeRequest(ctx, proposed.ChangeRequest.ID)
	if err != nil || request.Execution == nil || request.Execution.Status != ExecutionSucceeded {
		t.Fatalf("retirement execution = %#v, %v", request.Execution, err)
	}

	// Applying an approved retirement is idempotent, and it has to be: an
	// approval can be replayed by a retry or by a second reader of the same
	// queue. A retirement is permanent and its record is immutable, so a second
	// apply that re-ran the write would fail rather than be harmless -- and a
	// second apply that restamped the execution would erase when it first
	// happened. Both paths are exercised here: the caller that can see the
	// execution, and the stale caller that cannot.
	service := NewCatalogRetirementService(repository)
	service.now = func() time.Time { return time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) }
	again, replayed, err := service.ApplyApprovedChangeRequest(ctx, request)
	if err != nil {
		t.Fatalf("re-applying an approved retirement: %v", err)
	}
	if again.RetiredAt != stored.RetiredAt || again.ChangeRequestID != stored.ChangeRequestID {
		t.Errorf("re-apply returned a different retirement: %#v, was %#v", again, stored)
	}
	if replayed.Execution == nil || !replayed.Execution.CompletedAt.Equal(request.Execution.CompletedAt) {
		t.Errorf("re-apply restamped the execution: %#v, was %#v", replayed.Execution, request.Execution)
	}

	stale := request
	stale.Execution = nil
	if _, staleReplayed, err := service.ApplyApprovedChangeRequest(ctx, stale); err != nil {
		t.Errorf("a stale caller's re-apply failed: %v", err)
	} else if staleReplayed.Execution == nil ||
		!staleReplayed.Execution.CompletedAt.Equal(request.Execution.CompletedAt) {
		t.Errorf("a stale caller restamped the execution: %#v", staleReplayed.Execution)
	}
	persisted, err := repository.GetChangeRequest(ctx, request.ID)
	if err != nil || persisted.Execution == nil ||
		!persisted.Execution.CompletedAt.Equal(request.Execution.CompletedAt) {
		t.Errorf("the persisted retirement evidence was restamped: %#v, %v", persisted.Execution, err)
	}
}

func catalogPlan(id, environment string, createdAt time.Time, product, version string, active bool) PlanRecord {
	state := &DesiredState{Environment: Environment{ID: environment}, Releases: []Release{{Product: product, Version: version}}}
	steps := []PlanStep{}
	if active {
		steps = append(steps, PlanStep{InstallationID: "entity", Product: product, Version: version, Action: planActionReconcile})
	}
	return PlanRecord{
		ID: id, DesiredStateDigest: "sha256:test", DesiredState: state,
		Plan: Plan{Environment: environment, Generation: id, Steps: steps}, CreatedAt: createdAt, UpdatedAt: createdAt, Revision: 1,
	}
}

// A retirement is scoped either to a whole Product or to one Release of it, and
// every precondition below has to read that scope. The existing tests use one
// scope each, so inverting the scope test changed nothing they measured: a
// Product retirement that only blocked its own version, or a Release retirement
// that blocked every version, both passed.
func TestCatalogRetirementPreconditionsHonourBothScopes(t *testing.T) {
	t.Parallel()
	const product = "app.example:library"
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	// Each case seeds the state that must block, at version 1.4.0.
	for _, blocker := range []struct {
		name string
		seed func(*testing.T, context.Context, *MemoryRepository)
		// productScopeOnly marks the check that exists only for Product
		// retirements, where a Release retirement is expected not to block.
		productScopeOnly bool
	}{
		{name: "a Plan step still targets it", seed: func(t *testing.T, ctx context.Context, repository *MemoryRepository) {
			if _, err := repository.CreatePlan(ctx, catalogPlan("pln_active", "prod", now, product, "1.4.0", true)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a Release still depends on it", seed: func(t *testing.T, ctx context.Context, repository *MemoryRepository) {
			dependent := catalogPlan("pln_dependent", "prod", now, "app.example:consumer", "2.0.0", false)
			// The range starts at 1.4.0 so the older coordinate below is genuinely
			// outside it -- which is what makes versionWithinDependency observable.
			dependent.DesiredState.Releases[0].Dependencies = []Dependency{
				{Product: product, MinimumVersion: "1.4.0", MaximumVersion: "2.x.x"},
			}
			if _, err := repository.CreatePlan(ctx, dependent); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a reported Entity still runs it", seed: func(t *testing.T, ctx context.Context, repository *MemoryRepository) {
			if _, err := repository.CreatePlan(ctx, catalogPlan("pln_history", "prod", now, product, "1.4.0", false)); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
				Environment: "prod", Entities: []ReportedEntity{{
					InstallationID: "library", Product: product, Version: "1.4.0", Lifecycle: LifecycleInstalled,
				}},
			}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a Module installation still manages it", productScopeOnly: true, seed: func(t *testing.T, ctx context.Context, repository *MemoryRepository) {
			if _, err := repository.CreatePlan(ctx, catalogPlan("pln_history", "prod", now, product, "1.4.0", false)); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.CreateModuleInstallation(ctx, ModuleInstallation{
				ID: "modi_foundation", Module: "foundation", Environment: "prod",
				Entities: []InstalledModuleEntity{{ProductID: product}},
			}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(blocker.name, func(t *testing.T) {
			t.Parallel()
			prepare := func(t *testing.T, scope, version string) error {
				t.Helper()
				ctx := context.Background()
				repository := NewMemoryRepository()
				// An older coordinate nothing is using. It has to be a real
				// published version or the retirement is refused as unknown before
				// any precondition is reached.
				if _, err := repository.CreatePlan(ctx, catalogPlan("pln_old", "retired-env", now.Add(-time.Hour), product, "1.0.0", false)); err != nil {
					t.Fatal(err)
				}
				blocker.seed(t, ctx, repository)
				_, err := NewCatalogRetirementService(repository).Prepare(ctx, scope, product, version, "creator", "retire")
				return err
			}

			// A Product retirement covers every version, so it is blocked whatever
			// version the live state happens to be at.
			if err := prepare(t, CatalogRetirementProduct, ""); !errors.Is(err, ErrConflict) {
				t.Errorf("Product retirement was allowed while %s: %v", blocker.name, err)
			}

			// A Release retirement covers exactly one version. The live one blocks;
			// a different one does not, and that is the half nothing asserted.
			releaseErr := prepare(t, CatalogRetirementRelease, "1.4.0")
			otherErr := prepare(t, CatalogRetirementRelease, "1.0.0")
			if blocker.productScopeOnly {
				if releaseErr != nil {
					t.Errorf("Release retirement was blocked by a Product-scoped check: %v", releaseErr)
				}
				if otherErr != nil {
					t.Errorf("an unrelated Release retirement was blocked: %v", otherErr)
				}
				return
			}
			if !errors.Is(releaseErr, ErrConflict) {
				t.Errorf("Release retirement of the live version was allowed while %s: %v", blocker.name, releaseErr)
			}
			if otherErr != nil {
				t.Errorf("Release retirement of an unaffected version was blocked while %s: %v", blocker.name, otherErr)
			}
		})
	}
}

// The RID a retirement is governed and audited under. A Product retirement and a
// Release retirement of the same Product are different things to approve, and
// they have to be nameable apart -- a grant on one must not carry the other.
func TestCatalogRetirementRIDDistinguishesTheTwoScopes(t *testing.T) {
	t.Parallel()
	product := catalogRetirementRID(CatalogRetirement{Scope: CatalogRetirementProduct, Product: "app.example:api"})
	release := catalogRetirementRID(CatalogRetirement{Scope: CatalogRetirementRelease, Product: "app.example:api", Version: "1.2.3"})
	if product != productRID("app.example:api") {
		t.Errorf("a Product retirement is governed under %q, not the Product's own RID", product)
	}
	if release != "release/app.example:api/1.2.3" {
		t.Errorf("a Release retirement is governed under %q", release)
	}
	if product == release {
		t.Errorf("both scopes are governed under the same RID %q", product)
	}
}

// A retired coordinate must still be removable. Blocking every Plan that mentions
// it would strand the Entities already running it, which is the opposite of what
// retiring something is for.
func TestRetiredCatalogStillAllowsAPlanThatUninstallsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if _, err := repository.CreatePlan(ctx, catalogPlan("pln_history", "prod", now, "app.example:api", "1.2.3", false)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateCatalogRetirement(ctx, CatalogRetirement{
		Scope: CatalogRetirementProduct, Product: "app.example:api", Rationale: "retire product",
		RetiredBy: "creator", ChangeRequestID: "crq_product", RetiredAt: now.Add(time.Hour), Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}

	// The Plan's desired state no longer lists the retired Release -- that is what
	// removing it means -- while a step still names it in order to take it away.
	// Any Plan that still DESIRED the retired coordinate is refused earlier, so
	// this is the only shape in which the step-level exemption is reached at all.
	removal := func(id string, action string, at time.Time) PlanRecord {
		return PlanRecord{
			ID: id, DesiredStateDigest: "sha256:test",
			DesiredState: &DesiredState{Environment: Environment{ID: "prod"}},
			Plan: Plan{Environment: "prod", Generation: id, Steps: []PlanStep{{
				InstallationID: "entity", Product: "app.example:api", Version: "1.2.3", Action: action,
			}}},
			CreatedAt: at, UpdatedAt: at, Revision: 1,
		}
	}
	if _, err := repository.CreatePlan(ctx, removal("pln_uninstall", planActionUninstall, now.Add(2*time.Hour))); err != nil {
		t.Errorf("a Plan that uninstalls the retired Product was refused: %v", err)
	}
	if _, err := repository.CreatePlan(ctx, removal("pln_reconcile", planActionReconcile, now.Add(3*time.Hour))); !errors.Is(err, ErrConflict) {
		t.Errorf("a Plan that keeps running the retired Product = %v", err)
	}
}

// Two Plans for one Environment can share a creation timestamp -- they are stamped
// from the same clock, and nothing forces them apart. Which one counts as latest
// then has to be decided, and decided the same way every time: a precondition that
// depends on which Plan happened to be read first is not a precondition.
func TestLatestPlanIsDecidedDeterministicallyWhenTimestampsTie(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	tied := []PlanRecord{
		{ID: "pln_aaa", Plan: Plan{Environment: "prod"}, CreatedAt: at},
		{ID: "pln_ccc", Plan: Plan{Environment: "prod"}, CreatedAt: at},
		{ID: "pln_bbb", Plan: Plan{Environment: "prod"}, CreatedAt: at},
	}
	if got := latestPlansByEnvironment(tied)["prod"].ID; got != "pln_ccc" {
		t.Errorf("tied Plans resolved to %q, wanted the highest ID", got)
	}
	// Reversed input, same answer: the tiebreak is on the identifier, not on
	// arrival order.
	reversed := []PlanRecord{tied[2], tied[1], tied[0]}
	if got := latestPlansByEnvironment(reversed)["prod"].ID; got != "pln_ccc" {
		t.Errorf("reversed input resolved to %q, wanted the same Plan", got)
	}
	// And a later timestamp still beats a higher identifier.
	later := append([]PlanRecord{}, tied...)
	later = append(later, PlanRecord{ID: "pln_aab", Plan: Plan{Environment: "prod"}, CreatedAt: at.Add(time.Second)})
	if got := latestPlansByEnvironment(later)["prod"].ID; got != "pln_aab" {
		t.Errorf("a later Plan lost to a higher ID: %q", got)
	}
}

// The file repository builds a path out of the coordinate it is asked for, so the
// coordinate is validated before it becomes one. A Product retirement carries no
// version, and refusing that empty version would make whole-Product retirements
// unreadable from disk.
func TestFileRepositoryValidatesRetirementCoordinatesBeforeReadingThem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, err := NewFileRepository(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	for _, seed := range []PlanRecord{
		catalogPlan("pln_api", "old-api", now, "app.example:api", "1.2.3", false),
		catalogPlan("pln_worker", "old-worker", now, "app.example:worker", "1.2.3", false),
	} {
		if _, err := repository.CreatePlan(ctx, seed); err != nil {
			t.Fatal(err)
		}
	}
	for _, retirement := range []CatalogRetirement{
		{Scope: CatalogRetirementProduct, Product: "app.example:api", Rationale: "retire product",
			RetiredBy: "creator", ChangeRequestID: "crq_product", RetiredAt: now, Revision: 1},
		{Scope: CatalogRetirementRelease, Product: "app.example:worker", Version: "1.2.3", Rationale: "retire release",
			RetiredBy: "creator", ChangeRequestID: "crq_release", RetiredAt: now, Revision: 1},
	} {
		if _, err := repository.CreateCatalogRetirement(ctx, retirement); err != nil {
			t.Fatal(err)
		}
	}
	if stored, err := repository.GetCatalogRetirement(ctx, "app.example:api", ""); err != nil ||
		stored.Scope != CatalogRetirementProduct {
		t.Errorf("a whole-Product retirement was unreadable: %#v, %v", stored, err)
	}
	if stored, err := repository.GetCatalogRetirement(ctx, "app.example:worker", "1.2.3"); err != nil ||
		stored.Version != "1.2.3" {
		t.Errorf("a Release retirement was unreadable: %#v, %v", stored, err)
	}
	// A coordinate that could escape the directory never becomes a path.
	for _, bad := range [][2]string{
		{"../../etc", ""},
		{"app.example:api", "../../etc"},
		{"app.example:api", "not a version"},
	} {
		if _, err := repository.GetCatalogRetirement(ctx, bad[0], bad[1]); !errors.Is(err, ErrNotFound) {
			t.Errorf("coordinate %q/%q was turned into a path: %v", bad[0], bad[1], err)
		}
	}
}
