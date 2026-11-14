package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

const (
	ListRoute   = "GET /v1/packaging-runs"
	EventsRoute = "GET /v1/packaging-runs/{runID}/events"

	tierNative         = "CONDUIT_NATIVE"
	authorityPackaging = "packaging-run"
	authorityPlan      = "conduit-plan"
	authorityApproval  = "conduit-governance"
)

var errMalformedCursor = errors.New("malformed opaque cursor")

// Visible reports whether this caller may see a run aimed at environmentID.
// A nil function hides every row. The check runs after authentication and
// before search, counting, and pagination.
type Visible func(*http.Request, string) bool

func mountCollections(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(ListRoute, func(writer http.ResponseWriter, request *http.Request) {
		serveList(cfg, writer, request)
	})
	mux.HandleFunc(EventsRoute, func(writer http.ResponseWriter, request *http.Request) {
		serveEvents(cfg, writer, request)
	})
}

type runPage struct {
	Tier              string             `json:"tier"`
	Configured        bool               `json:"configured"`
	Authority         string             `json:"authority"`
	PlanAuthority     string             `json:"planAuthority"`
	ApprovalAuthority string             `json:"approvalAuthority"`
	Items             []domain.RunStatus `json:"items"`
	PageSize          int                `json:"pageSize"`
	Limit             int                `json:"limit"`
	HasPrevious       bool               `json:"hasPrevious"`
	NextCursor        string             `json:"nextCursor,omitempty"`
	PreviousCursor    string             `json:"previousCursor,omitempty"`
	Total             int                `json:"total"`
}

type eventPage struct {
	Tier              string             `json:"tier"`
	Configured        bool               `json:"configured"`
	Authority         string             `json:"authority"`
	PlanAuthority     string             `json:"planAuthority"`
	ApprovalAuthority string             `json:"approvalAuthority"`
	RunID             string             `json:"runId,omitempty"`
	Items             []domain.StepEvent `json:"items"`
	PageSize          int                `json:"pageSize"`
	Limit             int                `json:"limit"`
	HasPrevious       bool               `json:"hasPrevious"`
	NextCursor        string             `json:"nextCursor,omitempty"`
	PreviousCursor    string             `json:"previousCursor,omitempty"`
	Total             int                `json:"total"`
}

func serveList(cfg Config, writer http.ResponseWriter, request *http.Request) {
	if !authorized(cfg, writer, request) {
		return
	}
	query, err := parsePage(request.URL.Query())
	if writeCoded(writer, err) || writeCoded(writer, validateCursor(query)) {
		return
	}
	page := blankRunPage(cfg.Reader != nil, query.limit)
	if cfg.Reader == nil {
		writeJSON(writer, http.StatusOK, page)
		return
	}
	runs, err := cfg.Reader.List(request.Context())
	if err != nil {
		writeError(writer, http.StatusBadGateway, "packaging run index is unavailable")
		return
	}
	visible := make([]domain.RunStatus, 0, len(runs))
	for _, run := range runs {
		if run.RunID == "" || !cfg.allows(request, run.TargetPlanRef) {
			continue
		}
		if !matches(run, query) {
			continue
		}
		visible = append(visible, redactRun(run))
	}
	sort.Slice(visible, func(i, j int) bool { return visible[i].RunID < visible[j].RunID })
	items, meta, err := paginate(visible, func(run domain.RunStatus) string { return run.RunID }, query.fingerprint(), query.cursor, query.limit)
	if writeCoded(writer, err) {
		return
	}
	if items == nil {
		items = []domain.RunStatus{}
	}
	page.Items = items
	page.PageSize = meta.PageSize
	page.HasPrevious = meta.HasPrevious
	page.NextCursor = meta.NextCursor
	page.PreviousCursor = meta.PreviousCursor
	page.Total = len(visible)
	writeJSON(writer, http.StatusOK, page)
}

func serveEvents(cfg Config, writer http.ResponseWriter, request *http.Request) {
	if !authorized(cfg, writer, request) {
		return
	}
	query, err := parsePage(request.URL.Query())
	if writeCoded(writer, err) || writeCoded(writer, validateCursor(query)) {
		return
	}
	runID := request.PathValue("runID")
	page := blankEventPage(cfg.Reader != nil, query.limit)
	if cfg.Reader == nil {
		writeJSON(writer, http.StatusOK, page)
		return
	}
	status, err := cfg.Reader.Read(request.Context(), runID)
	if errors.Is(err, ErrNotFound) || (err == nil && !cfg.allows(request, status.TargetPlanRef)) {
		writeError(writer, http.StatusNotFound, ErrNotFound.Error())
		return
	}
	if err != nil {
		writeError(writer, http.StatusBadGateway, "packaging run status is unavailable")
		return
	}
	events, err := cfg.Reader.Events(request.Context(), runID)
	if errors.Is(err, ErrNotFound) {
		writeError(writer, http.StatusNotFound, ErrNotFound.Error())
		return
	}
	if err != nil {
		writeError(writer, http.StatusBadGateway, "packaging run history is unavailable")
		return
	}
	redacted := make([]domain.StepEvent, 0, len(events))
	for _, event := range events {
		redacted = append(redacted, redactEvent(event))
	}
	sort.Slice(redacted, func(i, j int) bool { return redacted[i].Sequence < redacted[j].Sequence })
	items, meta, err := paginate(redacted, eventKey, query.fingerprint(), query.cursor, query.limit)
	if writeCoded(writer, err) {
		return
	}
	if items == nil {
		items = []domain.StepEvent{}
	}
	page.RunID = runID
	page.Items = items
	page.PageSize = meta.PageSize
	page.HasPrevious = meta.HasPrevious
	page.NextCursor = meta.NextCursor
	page.PreviousCursor = meta.PreviousCursor
	page.Total = len(redacted)
	writeJSON(writer, http.StatusOK, page)
}

func (cfg Config) allows(request *http.Request, targetPlanRef string) bool {
	if cfg.Visible == nil {
		return false
	}
	return cfg.Visible(request, domain.EnvironmentOf(targetPlanRef))
}

func matches(run domain.RunStatus, query pageQuery) bool {
	if query.phase != "" && run.Phase != query.phase {
		return false
	}
	if query.outcome != "" && run.Outcome != query.outcome {
		return false
	}
	if query.planID != "" && run.PlanID != query.planID {
		return false
	}
	if query.search == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		run.RunID, run.Phase, run.Outcome, run.Reason, run.SourceDigest,
		run.ArtifactDigest, run.ProposalRevision, run.TargetPlanRef, run.PlanID,
	}, "\n"))
	return strings.Contains(haystack, query.search)
}

func validateCursor(query pageQuery) error {
	if query.cursor == "" {
		return nil
	}
	cursor, err := decodeCursor(query.cursor)
	if err != nil || cursor.Version != 1 || cursor.Key == "" {
		return StatusError{Status: http.StatusUnprocessableEntity, Message: "malformed opaque cursor"}
	}
	if cursor.Filter != query.fingerprint() {
		return StatusError{Status: http.StatusUnprocessableEntity, Message: "cursor does not match the active filters"}
	}
	return nil
}

func eventKey(event domain.StepEvent) string {
	return fmt.Sprintf("%06d", event.Sequence)
}

func blankRunPage(configured bool, limit int) runPage {
	return runPage{
		Tier:              tierNative,
		Configured:        configured,
		Authority:         authorityPackaging,
		PlanAuthority:     authorityPlan,
		ApprovalAuthority: authorityApproval,
		Items:             []domain.RunStatus{},
		Limit:             limit,
	}
}

func blankEventPage(configured bool, limit int) eventPage {
	return eventPage{
		Tier:              tierNative,
		Configured:        configured,
		Authority:         authorityPackaging,
		PlanAuthority:     authorityPlan,
		ApprovalAuthority: authorityApproval,
		Items:             []domain.StepEvent{},
		Limit:             limit,
	}
}

func stamp(status domain.RunStatus, configured bool) domain.RunStatus {
	status.Tier = tierNative
	status.Configured = configured
	status.Authority = authorityPackaging
	status.PlanAuthority = authorityPlan
	status.ApprovalAuthority = authorityApproval
	return status
}

func redactRun(run domain.RunStatus) domain.RunStatus {
	if secretField(run.Reason) || secretField(run.RunID) || secretField(run.Phase) || secretField(run.Outcome) ||
		secretField(run.SourceDigest) || secretField(run.ArtifactDigest) || secretField(run.ProposalRevision) ||
		secretField(run.TargetPlanRef) || secretField(run.PlanID) ||
		secretField(run.TemplateVersion) || secretField(run.ToolVersions) {
		run.Reason = "redacted"
		if secretField(run.RunID) {
			run.RunID = ""
		}
		if secretField(run.SourceDigest) {
			run.SourceDigest = ""
		}
		if secretField(run.ArtifactDigest) {
			run.ArtifactDigest = ""
		}
		if secretField(run.ProposalRevision) {
			run.ProposalRevision = ""
		}
		if secretField(run.TargetPlanRef) {
			run.TargetPlanRef = ""
		}
		if secretField(run.PlanID) {
			run.PlanID = ""
		}
		if secretField(run.TemplateVersion) {
			run.TemplateVersion = ""
		}
		if secretField(run.ToolVersions) {
			run.ToolVersions = ""
		}
	}
	return stamp(run, true)
}

func redactEvent(event domain.StepEvent) domain.StepEvent {
	if secretField(event.Step) || secretField(event.Outcome) || secretField(event.Reason) ||
		secretField(event.SourceDigest) || secretField(event.ArtifactDigest) || secretField(event.ProposalRevision) ||
		secretField(event.ApprovalID) || secretField(event.PlanID) || secretField(event.TargetPlanRef) {
		event.Reason = "redacted"
		if secretField(event.Step) {
			event.Step = "redacted"
		}
		if secretField(event.Outcome) {
			event.Outcome = ""
		}
		if secretField(event.SourceDigest) {
			event.SourceDigest = ""
		}
		if secretField(event.ArtifactDigest) {
			event.ArtifactDigest = ""
		}
		if secretField(event.ProposalRevision) {
			event.ProposalRevision = ""
		}
		if secretField(event.ApprovalID) {
			event.ApprovalID = ""
		}
		if secretField(event.PlanID) {
			event.PlanID = ""
		}
		if secretField(event.TargetPlanRef) {
			event.TargetPlanRef = ""
		}
	}
	event.Reason = domain.TrimReason(event.Reason)
	return event
}

func secretField(value string) bool { return domain.RejectSensitive(value) != nil }

func authorized(cfg Config, writer http.ResponseWriter, request *http.Request) bool {
	if cfg.Authorize == nil {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return false
	}
	if err := cfg.Authorize(request); err != nil {
		status := http.StatusUnauthorized
		var coded StatusError
		if errors.As(err, &coded) && coded.Status != 0 {
			status = coded.Status
		}
		message := "unauthorized"
		if err.Error() != "" {
			message = err.Error()
		}
		writeError(writer, status, message)
		return false
	}
	return true
}

func writeCoded(writer http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var coded StatusError
	if errors.As(err, &coded) && coded.Status != 0 {
		message := coded.Message
		if message == "" {
			message = coded.Error()
		}
		writeError(writer, coded.Status, message)
		return true
	}
	writeError(writer, http.StatusBadGateway, "packaging run status is unavailable")
	return true
}
