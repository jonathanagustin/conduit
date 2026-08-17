package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// promotionRepository is a repository that has actually reviewed the
// Environment the promotion evidence names.
//
// Every fixture here used a bare NewMemoryRepository() and then supplied health
// evidence for dev-use2, an Environment the Hub had never seen. That worked
// because an unreviewed Environment resolved to the defaulted topology --
// AGENTS_CONNECTED -- and vouched. ParticipatesInPromotion now requires the Hub
// to hold a reviewed generation, so the fixtures have to establish the
// Environment they attest with, which is what a real Hub would hold anyway.
func promotionRepository(t *testing.T) Repository {
	t.Helper()
	repository := NewMemoryRepository()
	state := foundation()
	if _, err := repository.CreatePlan(context.Background(), PlanRecord{
		ID:           "pln_promotion_fixture",
		Plan:         Plan{Environment: state.Environment.ID, Generation: state.Environment.Generation},
		DesiredState: &state, Status: PlanAwaitingApproval,
		CreatedAt: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seed the Environment the promotion evidence names: %v", err)
	}
	return repository
}

func promotionRequest(now time.Time) PromotionEvaluationRequest {
	release := foundation().Releases[0]
	release.Version = "0.2.0-1-gaaaaaaa"
	release.Chart.Tag = release.Version
	release.Images[0].Tag = release.Version
	release.Channels = []string{"DEV"}
	release.Labels = map[string]string{"security": "approved"}
	return PromotionEvaluationRequest{
		Release: release, FromChannel: "DEV", ToChannel: "RELEASE_CANDIDATE",
		Policy: PromotionPolicy{
			RequiredLabels:         map[string]string{"security": "approved"},
			MinimumHealthyEntities: 1, MaximumUnhealthyEntities: 0,
			MinimumHealthyDurationSeconds: 24 * 60 * 60, ExcludeWeekends: true,
		},
		Evidence: []PromotionHealthEvidence{{
			Environment: "dev-use2", InstallationID: "example-api", Health: "HEALTHY",
			HealthySince: now.Add(-72 * time.Hour), ObservedAt: now,
		}},
	}
}

func TestPromotionEvaluationEnforcesReachabilityFreshnessAndOverallTimeout(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	repository := promotionRepository(t)
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	request := promotionRequest(now)
	request.EvaluationStartedAt = now.Add(-2 * time.Hour)
	request.Policy.MaximumEvaluationDurationSeconds = 60 * 60
	request.Policy.MaximumEvidenceAgeSeconds = 60 * 60
	request.Evidence[0].ObservedAt = now.Add(-2 * time.Hour)
	record, err := lifecycle.EvaluatePromotion(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(record.Reasons, ";")
	if record.Status != PromotionBlocked || !strings.Contains(joined, "overall timeout") || !strings.Contains(joined, "stale health evidence") {
		t.Fatalf("timed promotion = %#v", record)
	}

	firstRequest := promotionRequest(now)
	first, err := lifecycle.EvaluatePromotion(context.Background(), firstRequest)
	if err != nil || first.Status != PromotionPassed {
		t.Fatalf("first stage = %#v, %v", first, err)
	}
	cycle := promotionRequest(now)
	cycle.FromChannel, cycle.ToChannel = "RELEASE_CANDIDATE", "DEV"
	if _, err := lifecycle.EvaluatePromotion(context.Background(), cycle); err == nil || !strings.Contains(err.Error(), "channel cycle") {
		t.Fatalf("cycle error = %v", err)
	}
	unreachable := promotionRequest(now)
	unreachable.FromChannel, unreachable.ToChannel = "UNREACHABLE", "STABLE"
	if _, err := lifecycle.EvaluatePromotion(context.Background(), unreachable); err == nil || !strings.Contains(err.Error(), "not reachable") {
		t.Fatalf("unreachable source error = %v", err)
	}
}

func TestPromotionFailureSuppressesCanaryAndRecoveryRemovesSuppression(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	repository := promotionRepository(t)
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	blocked := promotionRequest(now)
	blocked.Evidence[0].Health = "UNHEALTHY"
	record, err := lifecycle.EvaluatePromotion(context.Background(), blocked)
	if err != nil || record.Status != PromotionBlocked {
		t.Fatalf("blocked promotion = %#v, %v", record, err)
	}
	suppressions, err := repository.ListSuppressions(context.Background(), "dev-use2")
	if err != nil || len(suppressions) != 1 || suppressions[0].Source != SuppressionPromotion ||
		suppressions[0].PromotionID != record.ID || !suppressions[0].Active(now) {
		t.Fatalf("promotion suppressions = %#v, %v", suppressions, err)
	}
	now = now.Add(time.Minute)
	passed := promotionRequest(now)
	passed.Evidence[0].HealthySince = now.Add(-72 * time.Hour)
	recovered, err := lifecycle.EvaluatePromotion(context.Background(), passed)
	if err != nil || recovered.Status != PromotionPassed {
		t.Fatalf("recovered promotion = %#v, %v", recovered, err)
	}
	suppressions, err = repository.ListSuppressions(context.Background(), "dev-use2")
	if err != nil || len(suppressions) != 1 || suppressions[0].Active(now) || suppressions[0].RemovedBy != "platform-delivery" {
		t.Fatalf("recovered suppressions = %#v, %v", suppressions, err)
	}
}

func TestRampedRolloutConstraintAppliesOnlyToUpgrades(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	repository := promotionRepository(t)
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	request := promotionRequest(now)
	request.Policy.RolloutWindowSeconds = 3 * 60 * 60
	promotion, err := lifecycle.EvaluatePromotion(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	state := foundation()
	state.Releases = []Release{request.Release}
	state.Promotions = []PromotionRecord{promotion}
	state.Entities = []Entity{{
		Name: "example-api", Product: request.Release.Product, ReleaseChannel: request.ToChannel, Namespace: "example",
	}}
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].RampedRollout == nil || plan.Steps[0].RampedRollout.PromotionID != promotion.ID ||
		plan.Steps[0].RampedRollout.NotBefore.Before(promotion.EvaluatedAt) ||
		plan.Steps[0].RampedRollout.NotBefore.After(promotion.EvaluatedAt.Add(3*time.Hour)) {
		t.Fatalf("ramped rollout = %#v", plan.Steps)
	}
	before := plan.Steps[0].RampedRollout.NotBefore.Add(-time.Nanosecond)
	older := EnvironmentReport{Entities: []ReportedEntity{{InstallationID: "example-api", Version: "0.1.0"}}}
	if rampedRolloutReady(plan, older, before) {
		t.Fatal("upgrade was eligible before its ramp slot")
	}
	if !rampedRolloutReady(plan, EnvironmentReport{}, before) {
		t.Fatal("new install was incorrectly ramped")
	}
	same := EnvironmentReport{Entities: []ReportedEntity{{InstallationID: "example-api", Version: request.Release.Version}}}
	if !rampedRolloutReady(plan, same, before) {
		t.Fatal("config-only reconcile was incorrectly ramped")
	}
	newer := EnvironmentReport{Entities: []ReportedEntity{{InstallationID: "example-api", Version: "0.3.0"}}}
	if !rampedRolloutReady(plan, newer, before) {
		t.Fatal("downgrade or recall roll-off was incorrectly ramped")
	}
	if !rampedRolloutReady(plan, older, plan.Steps[0].RampedRollout.NotBefore) {
		t.Fatal("upgrade remained blocked at its ramp slot")
	}
}

func TestPromotionEvaluationIsImmutableAndEnablesChannelSelection(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	repository := promotionRepository(t)
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	record, err := lifecycle.EvaluatePromotion(context.Background(), promotionRequest(now))
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PromotionPassed || len(record.Reasons) != 0 || record.ID == "" {
		t.Fatalf("promotion = %#v", record)
	}
	second, err := lifecycle.EvaluatePromotion(context.Background(), promotionRequest(now))
	if err != nil || second.ID != record.ID {
		t.Fatalf("idempotent promotion = %#v, %v", second, err)
	}

	state := foundation()
	state.Releases = []Release{promotionRequest(now).Release}
	state.Promotions = []PromotionRecord{record}
	state.Entities = []Entity{{
		Name: "example-api", Product: record.Product, ReleaseChannel: record.ToChannel, Namespace: "example",
	}}
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Steps[0].Version; got != record.Version {
		t.Fatalf("selected version = %s", got)
	}
}

func TestPromotionEvaluationPersistsBlockedReasons(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	repository := promotionRepository(t)
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	request := promotionRequest(now)
	request.Release.Labels = nil
	request.Evidence[0].Health = "UNHEALTHY"
	record, err := lifecycle.EvaluatePromotion(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PromotionBlocked || len(record.Reasons) != 3 {
		t.Fatalf("blocked promotion = %#v", record)
	}
	if releaseOnChannel(request.Release, []PromotionRecord{record}, request.ToChannel) {
		t.Fatal("blocked promotion changed channel membership")
	}
}

func TestApolloSecurityPromotionLabelsAreDerivedFromExactScannerEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	request := promotionRequest(now)
	request.Release.VulnerabilityAssessment = passingScanResult(request.Release).VulnerabilityAssessment
	request.Policy.RequiredLabels = map[string]string{
		SecurityScannedLabel:     "true",
		SecurityScanOutcomeLabel: "pass",
	}
	lifecycle := NewLifecycle(promotionRepository(t))
	lifecycle.now = func() time.Time { return now }
	record, err := lifecycle.EvaluatePromotion(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PromotionPassed || record.Labels[SecurityScannedLabel] != "true" ||
		record.Labels[SecurityScanOutcomeLabel] != "pass" {
		t.Fatalf("derived security labels = %#v", record)
	}

	request = promotionRequest(now)
	request.Policy.RequiredLabels = map[string]string{SecurityScannedLabel: "true"}
	lifecycle = NewLifecycle(NewMemoryRepository())
	lifecycle.now = func() time.Time { return now }
	record, err = lifecycle.EvaluatePromotion(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PromotionBlocked || record.Labels[SecurityScannedLabel] != "" {
		t.Fatalf("unscanned release promotion = %#v", record)
	}

	request = promotionRequest(now)
	assessment := passingScanResult(request.Release).VulnerabilityAssessment
	assessment.Outcome = "FAIL"
	assessment.ActiveVulnerabilities = 1
	request.Release.VulnerabilityAssessment = assessment
	request.Policy.RequiredLabels = map[string]string{SecurityScanOutcomeLabel: "pass"}
	lifecycle = NewLifecycle(NewMemoryRepository())
	lifecycle.now = func() time.Time { return now }
	record, err = lifecycle.EvaluatePromotion(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PromotionBlocked || record.Labels[SecurityScannedLabel] != "true" ||
		record.Labels[SecurityScanOutcomeLabel] != "fail" {
		t.Fatalf("failing scan promotion = %#v", record)
	}

	request = promotionRequest(now)
	assessment = passingScanResult(request.Release).VulnerabilityAssessment
	assessment.ArtifactDigests = []string{"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	request.Release.VulnerabilityAssessment = assessment
	request.Policy.RequiredLabels = map[string]string{SecurityScannedLabel: "true"}
	lifecycle = NewLifecycle(NewMemoryRepository())
	lifecycle.now = func() time.Time { return now }
	record, err = lifecycle.EvaluatePromotion(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PromotionBlocked || record.Labels[SecurityScannedLabel] != "" {
		t.Fatalf("digest-mismatched scan promotion = %#v", record)
	}
}

func TestApolloSecurityPromotionLabelsCannotBeAuthored(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	request := promotionRequest(now)
	request.Release.Labels[SecurityScanOutcomeLabel] = "pass"
	if _, err := NewLifecycle(NewMemoryRepository()).EvaluatePromotion(context.Background(), request); err == nil ||
		!strings.Contains(err.Error(), "scanner-owned") {
		t.Fatalf("spoofed promotion label error = %v", err)
	}

	state := foundation()
	state.Releases[0].Labels = map[string]string{SecurityScannedLabel: "true"}
	errs := Validate(state)
	found := false
	for _, err := range errs {
		found = found || strings.Contains(err.Message, "scanner-owned")
	}
	if !found {
		t.Fatalf("spoofed desired-state label errors = %#v", errs)
	}
}

func TestFileRepositoryPersistsPromotion(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	// Same seeding as promotionRepository, against the file repository this
	// case exists to exercise.
	state := foundation()
	if _, err := repository.CreatePlan(context.Background(), PlanRecord{
		ID:           "pln_promotion_fixture",
		Plan:         Plan{Environment: state.Environment.ID, Generation: state.Environment.Generation},
		DesiredState: &state, Status: PlanAwaitingApproval,
		CreatedAt: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seed the Environment the promotion evidence names: %v", err)
	}
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	record, err := lifecycle.EvaluatePromotion(context.Background(), promotionRequest(now))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reopened.GetPromotion(context.Background(), record.ID)
	if err != nil || stored.Status != PromotionPassed {
		t.Fatalf("stored promotion = %#v, %v", stored, err)
	}
}

func TestPromotionHTTPAPI(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	repository := NewMemoryRepository()
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureAgentIdentityHeaders: true, AllowInsecureOperatorIdentityHeaders: true,
	})
	body, err := json.Marshal(promotionRequest(now))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/promotions/evaluations", bytes.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var created PromotionRecord
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/v1/promotions/"+created.ID, nil))
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getResponse.Code, getResponse.Body.String())
	}
}

func TestPromotionHTTPAPIProductFilterDoesNotLeakAnotherProduct(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 2, 30, 0, 0, time.UTC)
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }

	payments := promotionRequest(now)
	payments.Release.Product = "app.example:payments"
	search := promotionRequest(now)
	search.Release.Product = "app.example:search"
	if _, err := lifecycle.EvaluatePromotion(context.Background(), payments); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.EvaluatePromotion(context.Background(), search); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"operator"},
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/promotions?product=app.example%3Apayments", nil)
	request.Header.Set("X-Platform-Subject", "operator")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var page PromotionPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Promotions) != 1 || page.Promotions[0].Product != payments.Release.Product {
		t.Fatalf("filtered promotions = %#v", page)
	}
}

func TestPromotionRecordTamperingIsRejected(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	record, err := lifecycle.EvaluatePromotion(context.Background(), promotionRequest(now))
	if err != nil {
		t.Fatal(err)
	}
	record.Policy.MinimumHealthyEntities = 999
	state := foundation()
	state.Promotions = []PromotionRecord{record}
	errs := Validate(state)
	found := false
	for _, validationError := range errs {
		if validationError.Path == "promotions[0].id" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tampered record accepted: %#v", errs)
	}
}

func TestPromotionChainIsOrderIndependent(t *testing.T) {
	t.Parallel()
	release := foundation().Releases[0]
	release.Channels = []string{"DEV"}
	promotions := []PromotionRecord{
		{Product: release.Product, Version: release.Version, FromChannel: "RELEASE_CANDIDATE", ToChannel: "RELEASE", Status: PromotionPassed},
		{Product: release.Product, Version: release.Version, FromChannel: "DEV", ToChannel: "RELEASE_CANDIDATE", Status: PromotionPassed},
	}
	if !releaseOnChannel(release, promotions, "RELEASE") {
		t.Fatal("reversed multi-stage promotion did not reach RELEASE")
	}
}

func TestPromotionStagePauseIsIdempotentAndExcludesEvaluation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	repository := promotionRepository(t)
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	request := promotionRequest(now)

	paused, err := lifecycle.SetPromotionStagePaused(
		ctx, request.Release.Product, request.FromChannel, request.ToChannel, "release-operator", "investigating canary", true,
	)
	if err != nil || paused.Status != PromotionStagePaused || paused.ChangedBy != "release-operator" ||
		paused.Rationale != "investigating canary" || paused.Revision != 1 || !paused.ChangedAt.Equal(now) {
		t.Fatalf("paused control = %#v, %v", paused, err)
	}
	idempotent, err := lifecycle.SetPromotionStagePaused(
		ctx, request.Release.Product, request.FromChannel, request.ToChannel, "another-operator", "duplicate request", true,
	)
	if err != nil || idempotent != paused {
		t.Fatalf("idempotent pause = %#v, %v", idempotent, err)
	}
	if _, err := lifecycle.EvaluatePromotion(ctx, request); err == nil || !strings.Contains(err.Error(), "is paused by release-operator") {
		t.Fatalf("paused evaluation error = %v", err)
	}
	records, err := repository.ListPromotions(ctx, request.Release.Product)
	if err != nil || len(records) != 0 {
		t.Fatalf("paused stage created evaluation evidence: %#v, %v", records, err)
	}

	now = now.Add(time.Minute)
	resumed, err := lifecycle.SetPromotionStagePaused(
		ctx, request.Release.Product, request.FromChannel, request.ToChannel, "release-operator", "canary recovered", false,
	)
	if err != nil || resumed.Status != PromotionStageActive || resumed.ChangedBy != "release-operator" ||
		resumed.Rationale != "canary recovered" || resumed.Revision != 2 || !resumed.ChangedAt.Equal(now) {
		t.Fatalf("resumed control = %#v, %v", resumed, err)
	}
	idempotent, err = lifecycle.SetPromotionStagePaused(
		ctx, request.Release.Product, request.FromChannel, request.ToChannel, "another-operator", "duplicate resume", false,
	)
	if err != nil || idempotent != resumed {
		t.Fatalf("idempotent resume = %#v, %v", idempotent, err)
	}
	record, err := lifecycle.EvaluatePromotion(ctx, request)
	if err != nil || record.Status != PromotionPassed {
		t.Fatalf("resumed evaluation = %#v, %v", record, err)
	}
}

func TestFileRepositoryPersistsPromotionStagePauseAndResume(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	request := promotionRequest(now)
	if _, err := lifecycle.SetPromotionStagePaused(
		ctx, request.Release.Product, request.FromChannel, request.ToChannel, "release-operator", "manual hold", true,
	); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	control, err := reopened.GetPromotionStageControl(ctx, request.Release.Product, request.FromChannel, request.ToChannel)
	if err != nil || control.Status != PromotionStagePaused || control.Revision != 1 {
		t.Fatalf("reopened paused control = %#v, %v", control, err)
	}
	restartedLifecycle := NewLifecycle(reopened)
	restartedLifecycle.now = func() time.Time { return now }
	if _, err := restartedLifecycle.EvaluatePromotion(ctx, request); err == nil || !strings.Contains(err.Error(), "manual hold") {
		t.Fatalf("restarted paused evaluation error = %v", err)
	}
	now = now.Add(time.Minute)
	if _, err := restartedLifecycle.SetPromotionStagePaused(
		ctx, request.Release.Product, request.FromChannel, request.ToChannel, "release-operator", "hold cleared", false,
	); err != nil {
		t.Fatal(err)
	}

	reopenedAgain, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	control, err = reopenedAgain.GetPromotionStageControl(ctx, request.Release.Product, request.FromChannel, request.ToChannel)
	if err != nil || control.Status != PromotionStageActive || control.Revision != 2 || control.Rationale != "hold cleared" {
		t.Fatalf("reopened active control = %#v, %v", control, err)
	}
}

func TestPromotionStageControlHTTPIsGoverned(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"platform-admin"},
	})
	path := "/v1/products/app.example:example-api/promotion-stages/DEV/RELEASE_CANDIDATE"

	denied := httptest.NewRequest(http.MethodPost, path+"/pause", strings.NewReader(`{"rationale":"manual hold"}`))
	denied.Header.Set("X-Platform-Subject", "unprivileged-user")
	deniedResponse := httptest.NewRecorder()
	handler.ServeHTTP(deniedResponse, denied)
	if deniedResponse.Code != http.StatusForbidden {
		t.Fatalf("unprivileged pause = %d %s", deniedResponse.Code, deniedResponse.Body.String())
	}

	pause := httptest.NewRequest(http.MethodPost, path+"/pause", strings.NewReader(`{"rationale":"manual hold"}`))
	pause.Header.Set("X-Platform-Subject", "platform-admin")
	pauseResponse := httptest.NewRecorder()
	handler.ServeHTTP(pauseResponse, pause)
	if pauseResponse.Code != http.StatusOK {
		t.Fatalf("pause = %d %s", pauseResponse.Code, pauseResponse.Body.String())
	}
	var paused PromotionStageControl
	if err := json.Unmarshal(pauseResponse.Body.Bytes(), &paused); err != nil || paused.Status != PromotionStagePaused || paused.ChangedBy != "platform-admin" {
		t.Fatalf("paused response = %#v, %v", paused, err)
	}

	get := httptest.NewRequest(http.MethodGet, path, nil)
	get.Header.Set("X-Platform-Subject", "platform-admin")
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"status":"PAUSED"`) {
		t.Fatalf("get paused control = %d %s", getResponse.Code, getResponse.Body.String())
	}

	resume := httptest.NewRequest(http.MethodPost, path+"/resume", strings.NewReader(`{"rationale":"hold cleared"}`))
	resume.Header.Set("X-Platform-Subject", "platform-admin")
	resumeResponse := httptest.NewRecorder()
	handler.ServeHTTP(resumeResponse, resume)
	if resumeResponse.Code != http.StatusOK || !strings.Contains(resumeResponse.Body.String(), `"status":"ACTIVE"`) {
		t.Fatalf("resume = %d %s", resumeResponse.Code, resumeResponse.Body.String())
	}
}

// Reasons is prose. Every consumer that wants to do more than print it -- group
// blocked promotions by cause, count how often a label gate fires, point an
// operator at the Environment to go fix -- has to parse the sentence back
// apart, and the sentences were never designed to survive that. Blockers carry
// the same decision as a closed set of kinds with the subject named.
func TestPromotionBlockersCarryTheDecisionThatReasonsOnlyDescribes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	evaluate := func(t *testing.T, mutate func(*PromotionEvaluationRequest)) PromotionRecord {
		t.Helper()
		lifecycle := NewLifecycle(promotionRepository(t))
		lifecycle.now = func() time.Time { return now }
		request := promotionRequest(now)
		mutate(&request)
		record, err := lifecycle.EvaluatePromotion(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		return record
	}

	t.Run("Reasons is exactly the Blockers' messages, in the same order", func(t *testing.T) {
		// Reasons is what the record has always published, and a peer Hub
		// still reads it. The moment the two can disagree there are two
		// sources of truth for one decision, so Reasons is derived and never
		// assembled separately.
		record := evaluate(t, func(request *PromotionEvaluationRequest) {
			request.Release.Labels = nil
			request.Policy.MaximumEvidenceAgeSeconds = 60
			request.Evidence[0].ObservedAt = now.Add(-2 * time.Hour)
		})
		if len(record.Blockers) < 2 {
			t.Fatalf("blockers = %#v, want several to compare against", record.Blockers)
		}
		if len(record.Reasons) != len(record.Blockers) {
			t.Fatalf("reasons = %v, blockers = %#v", record.Reasons, record.Blockers)
		}
		for index, blocker := range record.Blockers {
			if record.Reasons[index] != blocker.Message {
				t.Errorf("reason %d = %q, blocker message = %q", index, record.Reasons[index], blocker.Message)
			}
		}
	})

	t.Run("a passed promotion carries neither", func(t *testing.T) {
		record := evaluate(t, func(*PromotionEvaluationRequest) {})
		if record.Status != PromotionPassed || len(record.Blockers) != 0 || len(record.Reasons) != 0 {
			t.Fatalf("passed promotion = %#v", record)
		}
	})

	t.Run("names the kind and the subject for each cause", func(t *testing.T) {
		for _, test := range []struct {
			name    string
			mutate  func(*PromotionEvaluationRequest)
			kind    string
			subject string
		}{
			{
				// The label name, not the whole requirement: it is what the
				// operator sets on the Release to clear the blocker.
				"a missing required label", func(request *PromotionEvaluationRequest) { request.Release.Labels = nil },
				PromotionBlockerRequiredLabelMissing, "security",
			},
			{
				"a label value outside the allowed set", func(request *PromotionEvaluationRequest) {
					request.Policy.RequiredLabels = nil
					request.Policy.LabelRequirements = []PromotionLabelRequirement{{Label: "tier", Values: []string{"gold"}}}
				},
				PromotionBlockerRequiredLabelNotAllowed, "tier",
			},
			{
				"evidence older than the policy allows", func(request *PromotionEvaluationRequest) {
					request.Policy.MaximumEvidenceAgeSeconds = 60
					request.Evidence[0].ObservedAt = now.Add(-2 * time.Hour)
				},
				PromotionBlockerEvidenceStale, "dev-use2/example-api",
			},
			{
				"the same Installation reporting twice", func(request *PromotionEvaluationRequest) {
					request.Evidence = append(request.Evidence, request.Evidence[0])
				},
				PromotionBlockerEvidenceDuplicate, "dev-use2/example-api",
			},
			{
				"a health state the Hub does not know", func(request *PromotionEvaluationRequest) {
					request.Evidence[0].Health = "MOSTLY_FINE"
				},
				PromotionBlockerEvidenceHealthUnknown, "dev-use2/example-api",
			},
			{
				"an evaluation that ran past its overall timeout", func(request *PromotionEvaluationRequest) {
					request.Policy.MaximumEvaluationDurationSeconds = 60
					request.EvaluationStartedAt = now.Add(-2 * time.Hour)
				},
				PromotionBlockerEvaluationTimedOut, "",
			},
			{
				"too few healthy Entities", func(request *PromotionEvaluationRequest) {
					request.Policy.MinimumHealthyEntities = 5
				},
				PromotionBlockerHealthyBelowMinimum, "",
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				record := evaluate(t, test.mutate)
				var found *PromotionBlocker
				for index, blocker := range record.Blockers {
					if blocker.Kind == test.kind {
						found = &record.Blockers[index]
					}
				}
				if found == nil {
					t.Fatalf("no %s blocker in %#v", test.kind, record.Blockers)
				}
				if test.subject == "" {
					// Not every cause has a subject. A threshold the whole
					// stage failed is about the stage, and inventing a subject
					// for it would make Subjects unreliable where it matters.
					if len(found.Subjects) != 0 {
						t.Errorf("%s carried subjects %v, but nothing in particular is at fault", test.kind, found.Subjects)
					}
					return
				}
				if len(found.Subjects) != 1 || found.Subjects[0] != test.subject {
					t.Errorf("%s subjects = %v, want [%s]", test.kind, found.Subjects, test.subject)
				}
			})
		}
	})
}

// A PromotionRecord arriving from another Hub is re-derived from the policy and
// evidence it carries, so a peer cannot assert a decision its own inputs do not
// support. Blockers have to be re-derived too, and the record ID does not cover
// it: the ID is a digest of the record, so a peer that edits a blocker and
// recomputes the ID has a self-consistent record that still lies about why.
func TestPromotionRecordsFromAPeerReproduceTheirBlockers(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	lifecycle := NewLifecycle(promotionRepository(t))
	lifecycle.now = func() time.Time { return now }
	request := promotionRequest(now)
	// Two blockers, from two producers, whose messages arrive in the opposite
	// order to the one they are published in: the label blocker is built
	// first, and "healthy entity count" sorts before "required label".
	request.Release.Labels = nil
	request.Policy.MinimumHealthyEntities = 5
	blocked, err := lifecycle.EvaluatePromotion(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.Blockers) == 0 {
		t.Fatal("the fixture no longer blocks")
	}

	reseal := func(t *testing.T, record PromotionRecord) PromotionRecord {
		t.Helper()
		id, err := promotionIdentity(record)
		if err != nil {
			t.Fatal(err)
		}
		record.ID = id
		return record
	}
	validate := func(record PromotionRecord) []string {
		var errs []ValidationError
		validatePromotions([]PromotionRecord{record}, []Release{request.Release}, &errs)
		paths := make([]string, 0, len(errs))
		for _, item := range errs {
			paths = append(paths, item.Path)
		}
		return paths
	}

	t.Run("accepts the record it produced", func(t *testing.T) {
		if paths := validate(blocked); len(paths) != 0 {
			t.Fatalf("a freshly evaluated record did not validate: %v", paths)
		}
	})

	t.Run("rejects a resealed record whose blocker kind was swapped", func(t *testing.T) {
		tampered := cloneJSON(blocked)
		tampered.Blockers[0].Kind = PromotionBlockerEvidenceStale
		tampered = reseal(t, tampered)
		paths := validate(tampered)
		if !containsSubstring(paths, ".blockers") {
			// Reasons is untouched and still reproduces, and the ID matches the
			// contents, so nothing else in the validator can see this.
			t.Fatalf("a swapped blocker kind was accepted: %v", paths)
		}
	})

	t.Run("rejects a resealed record whose blocker subject was swapped", func(t *testing.T) {
		tampered := cloneJSON(blocked)
		tampered.Blockers[0].Subjects = []string{"some-other-thing"}
		tampered = reseal(t, tampered)
		if paths := validate(tampered); !containsSubstring(paths, ".blockers") {
			t.Fatalf("a swapped blocker subject was accepted: %v", paths)
		}
	})

	t.Run("rejects a resealed record that dropped a blocker", func(t *testing.T) {
		// Reasons still reproduces in full, and the ID matches the contents,
		// so a comparison that stopped at the shorter list would accept a
		// record that quietly understates why the promotion was blocked.
		if len(blocked.Blockers) < 2 {
			t.Fatalf("fixture has %d blockers, so this proves nothing", len(blocked.Blockers))
		}
		tampered := cloneJSON(blocked)
		tampered.Blockers = tampered.Blockers[:len(tampered.Blockers)-1]
		tampered = reseal(t, tampered)
		if paths := validate(tampered); !containsSubstring(paths, ".blockers") {
			t.Fatalf("a dropped blocker was accepted: %v", paths)
		}
	})

	t.Run("accepts a canary record it produced", func(t *testing.T) {
		// A canary stage assembles its blockers down a different path, and the
		// record is only accepted if that path reproduces the order the record
		// was published in.
		canaryLifecycle := NewLifecycle(NewMemoryRepository())
		canaryLifecycle.now = func() time.Time { return now }
		canaryRequest := promotionRequest(now)
		canaryRequest.Release.Labels = nil
		canaryRequest.Policy.Type = PromotionCanary
		canaryRequest.Policy.MinimumHealthyEntities = 0
		canaryRequest.Policy.MinimumHealthyDurationSeconds = 0
		canaryRequest.Policy.Conditions = []PromotionCondition{
			{Action: PromoteWhenHealthy, ThresholdPercent: 100, MinimumEntityCount: 9},
		}
		canaryRecord, err := canaryLifecycle.EvaluatePromotion(context.Background(), canaryRequest)
		if err != nil {
			t.Fatal(err)
		}
		if len(canaryRecord.Blockers) < 2 {
			t.Fatalf("canary fixture has %d blockers", len(canaryRecord.Blockers))
		}
		var errs []ValidationError
		validatePromotions([]PromotionRecord{canaryRecord}, []Release{canaryRequest.Release}, &errs)
		if len(errs) != 0 {
			t.Fatalf("a freshly evaluated canary record did not validate: %#v", errs)
		}
	})

	t.Run("accepts a record from a Hub that predates Blockers", func(t *testing.T) {
		// Reasons is what such a Hub published, and it still reproduces. The
		// record is evidence of a real decision; refusing it would partition
		// promotion history at the version this field was added.
		older := cloneJSON(blocked)
		older.Blockers = nil
		older = reseal(t, older)
		if paths := validate(older); len(paths) != 0 {
			t.Fatalf("a record without Blockers was rejected: %v", paths)
		}
	})

	t.Run("still rejects that record if its Reasons do not reproduce", func(t *testing.T) {
		// Dropping Blockers must not become a way to escape verification.
		older := cloneJSON(blocked)
		older.Blockers = nil
		older.Reasons = []string{"everything is fine"}
		older = reseal(t, older)
		if paths := validate(older); !containsSubstring(paths, ".status") {
			t.Fatalf("unreproducible Reasons were accepted: %v", paths)
		}
	})
}

// Blocker order is not cosmetic. A peer's record is accepted only if its
// Reasons reproduce element by element, and several blockers are produced by
// ranging over maps -- PromotionPolicy.RequiredLabels among them. Left in
// whatever order Go's map iteration gave that run, the same inputs would
// reproduce on one evaluation and fail on the next.
func TestPromotionBlockerOrderIsStableAcrossEvaluations(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	request := promotionRequest(now)
	request.Release.Labels = map[string]string{}
	request.Policy.RequiredLabels = map[string]string{
		"security": "approved", "tier": "gold", "region": "use2", "owner": "payments", "audit": "clean",
	}

	var first PromotionRecord
	for attempt := 0; attempt < 25; attempt++ {
		lifecycle := NewLifecycle(promotionRepository(t))
		lifecycle.now = func() time.Time { return now }
		record, err := lifecycle.EvaluatePromotion(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if len(record.Blockers) != len(request.Policy.RequiredLabels) {
			t.Fatalf("blockers = %#v, want one per unmet label", record.Blockers)
		}
		if attempt == 0 {
			first = record
			if !sort.StringsAreSorted(record.Reasons) {
				t.Fatalf("reasons = %v, not ordered", record.Reasons)
			}
			continue
		}
		if !stringSlicesEqual(record.Reasons, first.Reasons) {
			t.Fatalf("attempt %d ordered differently:\n%v\n%v", attempt, record.Reasons, first.Reasons)
		}
		// The record ID is a digest of the whole record, so an unstable order
		// would also give the same decision two different identities.
		if record.ID != first.ID {
			t.Fatalf("attempt %d produced a different identity for the same decision", attempt)
		}
	}
}

// A canary stage that promoted on nothing failed as a stage. Attaching one of
// its conditions as the subject would point an operator at a condition that is
// not individually at fault, and would make Subjects unreliable exactly where
// a caller wants to trust it.
func TestCanaryStageBlamesNoSingleConditionWhenNonePromoted(t *testing.T) {
	t.Parallel()
	request := PromotionEvaluationRequest{
		Policy: PromotionPolicy{Conditions: []PromotionCondition{
			{Action: PromoteWhenHealthy, ThresholdPercent: 90, MinimumEntityCount: 1},
		}},
	}
	blockers := catalog.EvaluateCanaryStage(request, promotionFixtureTime())
	var found bool
	for _, blocker := range blockers {
		if blocker.Kind != PromotionBlockerCanaryNoConditionMet {
			continue
		}
		found = true
		if len(blocker.Subjects) != 0 {
			t.Errorf("subjects = %v, but no one condition is at fault", blocker.Subjects)
		}
	}
	if !found {
		t.Fatalf("blockers = %#v, want the stage-level blocker", blockers)
	}
}

// Reasons has always been sorted, and stays sorted now that it is a projection.
// Each producer sorts what it makes, so the only way the whole list comes out
// unsorted is by concatenating two sorted runs -- which is exactly what
// happens, twice: the federation blockers are prepended to the stage's, and a
// canary stage's are appended to the label blockers.
func TestPromotionReasonsStaySortedAcrossConcatenatedProducers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)

	t.Run("federation blockers merged with a timed stage's", func(t *testing.T) {
		repository := NewMemoryRepository()
		lifecycle := NewLifecycle(repository)
		lifecycle.now = func() time.Time { return now }
		disconnected := foundation()
		disconnected.Environment.ID = "air-gapped"
		disconnected.Environment.ConnectionSetting = ConnectionNoConnectionExpected
		if _, err := repository.CreatePlan(ctx, PlanRecord{
			ID: "pln_disconnected", Plan: Plan{Environment: "air-gapped", Generation: disconnected.Environment.Generation},
			DesiredState: &disconnected, Status: PlanAwaitingApproval, CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		record, err := lifecycle.EvaluatePromotion(ctx, PromotionEvaluationRequest{
			Release:     Release{Product: "app.example:example-api", Version: "0.1.0"},
			FromChannel: "RELEASE", ToChannel: "STABLE",
			// The federation blockers are prepended, so the merge only shows
			// up against a stage blocker whose message sorts before
			// "environment ...". A canary stage produces "condition 1 ...",
			// which does; every timed-stage message begins with a later
			// letter, which is why this stage is a canary.
			Policy: PromotionPolicy{
				Type:       PromotionCanary,
				Conditions: []PromotionCondition{{Action: PromoteWhenHealthy, ThresholdPercent: 100, MinimumEntityCount: 9}},
			},
			Evidence: []PromotionHealthEvidence{{
				Environment: "air-gapped", InstallationID: "example-api",
				Health: HealthHealthy, HealthySince: now.Add(-2 * time.Hour), ObservedAt: now.Add(-2 * time.Hour),
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !blockedBy(record.Blockers, PromotionBlockerEnvironmentCannotVouch, "air-gapped") {
			t.Fatalf("fixture no longer produces a federation blocker: %#v", record.Blockers)
		}
		if !sort.StringsAreSorted(record.Reasons) {
			t.Errorf("reasons = %v, not ordered", record.Reasons)
		}
	})

	t.Run("canary blockers merged with label blockers", func(t *testing.T) {
		lifecycle := NewLifecycle(NewMemoryRepository())
		lifecycle.now = func() time.Time { return now }
		request := promotionRequest(now)
		request.Release.Labels = map[string]string{}
		request.Policy.RequiredLabels = map[string]string{"audit": "clean", "zone": "use2"}
		request.Policy.Type = PromotionCanary
		request.Policy.MinimumHealthyDurationSeconds = 0
		request.Policy.MinimumHealthyEntities = 0
		request.Policy.Conditions = []PromotionCondition{
			{Action: PromoteWhenHealthy, ThresholdPercent: 100, MinimumEntityCount: 9},
		}
		record, err := lifecycle.EvaluatePromotion(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if !blockedBy(record.Blockers, PromotionBlockerCanaryTooFewEntities, "canary Entities") {
			t.Fatalf("fixture no longer produces a canary blocker: %#v", record.Blockers)
		}
		if !sort.StringsAreSorted(record.Reasons) {
			t.Errorf("reasons = %v, not ordered", record.Reasons)
		}
	})
}
