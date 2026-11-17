package composition

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	livehttp "github.com/jonathanagustin/conduit/internal/liveevents/adapters/httpapi"
	"github.com/jonathanagustin/conduit/internal/liveevents/adapters/nativeauth"
	"github.com/jonathanagustin/conduit/internal/liveevents/adapters/schema"
	liveapp "github.com/jonathanagustin/conduit/internal/liveevents/application"
	live "github.com/jonathanagustin/conduit/internal/liveevents/domain"
	liveports "github.com/jonathanagustin/conduit/internal/liveevents/ports"
	packaginglive "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/liveevents"
	packaging "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
)

type liveRequestKey struct{}

// A stream never reuses the once-per-request operator cache: credentials,
// membership, delegation and grants must remain current after response headers.
func liveOperatorState(ctx context.Context, service *controlplane.GovernanceService, options controlplane.HandlerOptions) (nativeauth.State, error) {
	request, ok := ctx.Value(liveRequestKey{}).(*http.Request)
	if !ok || request == nil {
		return nativeauth.State{}, live.ErrUnauthorized
	}
	if options.OperatorIdentityResolver == nil && options.AllowInsecureOperatorIdentityHeaders && strings.TrimSpace(request.Header.Get("X-Platform-Subject")) == "" {
		return nativeauth.State{}, live.ErrUnauthorized
	}
	// The TLS handshake verified chains once. Recheck validity as the connection
	// ages, including when the configured OIDC resolver accepts certificate auth.
	if request.TLS != nil && len(request.TLS.VerifiedChains) > 0 && request.Header.Get("Authorization") == "" {
		valid := false
		now := time.Now()
		for _, chain := range request.TLS.VerifiedChains {
			current := len(chain) > 0
			for _, cert := range chain {
				if cert == nil || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
					current = false
					break
				}
			}
			valid = valid || current
		}
		if !valid {
			return nativeauth.State{}, live.ErrUnauthorized
		}
	}
	claims, err := operatorClaims(request.WithContext(ctx), options)
	if err != nil || strings.TrimSpace(claims.Username) == "" {
		return nativeauth.State{}, live.ErrUnauthorized
	}
	principal, err := service.ResolvePrincipal(ctx, claims)
	if err != nil {
		return nativeauth.State{}, liveIdentityError(err)
	}
	access, err := service.EventSubscriberAccess(ctx, principal, options)
	if err != nil {
		return nativeauth.State{}, liveIdentityError(err)
	}
	return nativeauth.State{Principal: principal, Grants: access.Grants, Administrator: access.Administrator, Allowed: access.Allowed}, nil
}
func liveIdentityError(err error) error {
	switch {
	case errors.Is(err, controlplane.ErrUnauthorized):
		return live.ErrUnauthorized
	case errors.Is(err, controlplane.ErrForbidden):
		return live.ErrForbidden
	default:
		return live.ErrUnavailable
	}
}

// CONDUIT-INVENTED: liveevents.GovernedSSE
// Displaced-by: A published or owner-approved sanitized event-stream contract establishing transport, cursor binding, stream eligibility, source coverage and resource authorization semantics.
func mountLiveEvents(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, hub Options) (http.Handler, error) {
	governanceService := newGovernanceService(repository, options)
	resolve := func(ctx context.Context) (nativeauth.State, error) {
		return liveOperatorState(ctx, governanceService, options)
	}
	auth, err := nativeauth.New(nativeauth.Config{Resolve: resolve, Visible: func(ctx context.Context, principal governance.Principal, subject live.Subject) (bool, error) {
		if subject.Type == "assistant-run" {
			return assistantRunVisible(ctx, repository, principal, subject)
		}
		if subject.Type != "packaging-run" {
			return controlplane.EventResourceVisible(ctx, repository, governanceService, principal, options, subject)
		}
		if hub.PackagingStatus == nil {
			return false, live.ErrUnavailable
		}
		status, err := hub.PackagingStatus.Read(ctx, subject.ID)
		if errors.Is(err, packaginghttp.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, live.ErrUnavailable
		}
		environment := packaging.EnvironmentOf(status.TargetPlanRef)
		if status.RunID != subject.ID {
			return false, live.ErrUnavailable
		}
		if environment == "" {
			return false, nil
		}
		access, err := governanceService.EventSubscriberAccess(ctx, principal, options)
		if err != nil {
			return false, live.ErrUnavailable
		}
		return access.Administrator || governanceService.Authorize(ctx, governance.EffectivePrincipal(principal), "environment/"+environment, controlplane.OperationReadResource), nil
	}})
	if err != nil {
		return nil, err
	}
	authorize := func(request *http.Request) (context.Context, error) {
		ctx := context.WithValue(request.Context(), liveRequestKey{}, request.Clone(request.Context()))
		_, err := auth.Snapshot(ctx)
		return ctx, err
	}
	var stream http.Handler
	if hub.LiveEventJournal == nil {
		stream = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := authorize(r)
			status, message := http.StatusServiceUnavailable, "event delivery not configured"
			if errors.Is(err, live.ErrUnauthorized) {
				status, message = http.StatusUnauthorized, "operator identity required"
			} else if errors.Is(err, live.ErrForbidden) {
				status, message = http.StatusForbidden, "event stream permission required"
			}
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, message, status)
		})
	} else {
		native, ok := repository.(liveports.CommittedSource)
		if !ok {
			return nil, live.ErrInvalid
		}
		var packagingSource liveports.CommittedSource = unavailableLiveSource{}
		if reader, ok := hub.PackagingStatus.(packaginglive.Reader); ok {
			packagingSource, err = packaginglive.New(reader)
			if err != nil {
				return nil, err
			}
		}
		assistantSource := assistantLiveSource{repository: repository}
		sources := map[string]liveports.CommittedSource{"native": native, "packaging": packagingSource, "assistant": assistantSource}
		validator, err := schema.New()
		if err != nil {
			return nil, err
		}
		service, err := liveapp.New(liveapp.Config{Journal: hub.LiveEventJournal, Authorizer: auth, Validator: validator, Readiness: liveContextReadiness{}, Sources: sources})
		if err != nil {
			return nil, err
		}
		root := hub.LiveEventContext
		if root == nil {
			root = context.Background()
		}
		refresh := newLiveRefresh(root, service, []string{"native", "packaging", "assistant"})
		stream, err = livehttp.New(livehttp.Config{Service: service, Authorize: authorize, Refresh: refresh})
		if err != nil {
			return nil, err
		}
	}
	mux := http.NewServeMux()
	mux.Handle("GET /v1/events", stream)
	mux.Handle("/", legacy)
	return mux, nil
}

var assistantRunIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,199}$`)

func parseAssistantRunID(id string) (string, string, bool) {
	if !assistantRunIDPattern.MatchString(id) {
		return "", "", false
	}
	idx := strings.IndexByte(id, ':')
	if idx <= 0 || idx >= len(id)-1 {
		return "", "", false
	}
	return id[:idx], id[idx+1:], true
}

func assistantRunVisible(ctx context.Context, repository controlplane.Repository, principal governance.Principal, subject live.Subject) (bool, error) {
	threadID, runID, ok := parseAssistantRunID(subject.ID)
	if !ok {
		return false, nil
	}
	if strings.TrimSpace(principal.Username) == "" || governance.EffectivePrincipal(principal).Username != principal.Username {
		return false, nil
	}
	events, err := repository.ListAuthorizationAudit(ctx, "planning-session/"+threadID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		return false, live.ErrUnavailable
	}
	for _, event := range events {
		if event.Context == nil {
			continue
		}
		tID, _ := event.Context["threadId"].(string)
		rID, _ := event.Context["runId"].(string)
		if tID == threadID && rID == runID && event.Subject != "" {
			return event.Subject == principal.Username, nil
		}
	}
	return false, nil
}

type assistantLiveSource struct {
	repository controlplane.Repository
}

type assistantActivityRow struct {
	sequence uint64
	activity string
	at       time.Time
}

func (s assistantLiveSource) ReadCommittedEvents(ctx context.Context, positions map[string]uint64, limit int) ([]live.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > live.MaxQueueEvents {
		return nil, live.ErrInvalid
	}
	if s.repository == nil {
		return nil, live.ErrUnavailable
	}
	events, err := s.repository.ListAuthorizationAudit(ctx, "")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, live.ErrUnavailable
	}
	byRun := map[string][]assistantActivityRow{}
	for _, event := range events {
		if event.Context == nil || !strings.HasPrefix(event.Action, "agent.event.") {
			continue
		}
		threadID, _ := event.Context["threadId"].(string)
		runID, _ := event.Context["runId"].(string)
		activity, _ := event.Context["activity"].(string)
		seq, ok := parseActivitySequence(event.Context["activitySequence"])
		if threadID == "" || runID == "" || !knownAssistantActivity(activity) || !ok || seq == 0 {
			continue
		}
		runKey := threadID + ":" + runID
		if !assistantRunIDPattern.MatchString(runKey) {
			return nil, live.ErrInvalid
		}
		byRun[runKey] = append(byRun[runKey], assistantActivityRow{
			sequence: seq,
			activity: activity,
			at:       event.CreatedAt.UTC(),
		})
	}
	keys := make([]string, 0, len(byRun))
	for k := range byRun {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	records := make([]live.Record, 0)
	for _, runKey := range keys {
		rows := byRun[runKey]
		sort.Slice(rows, func(i, j int) bool { return rows[i].sequence < rows[j].sequence })
		for i, row := range rows {
			if row.sequence != uint64(i+1) || row.at.IsZero() {
				return nil, live.ErrInvalid
			}
		}
		last := rows[len(rows)-1].sequence
		subject := live.Subject{Type: "assistant-run", ID: runKey}
		producer := live.ProducerID(subject)
		previous := positions[producer]
		if previous > last {
			return nil, live.ErrReset
		}
		for _, row := range rows {
			if row.sequence <= previous {
				continue
			}
			payload, _ := json.Marshal(struct {
				Activity string `json:"activity"`
			}{Activity: row.activity})
			records = append(records, live.Record{
				Producer: producer,
				Position: row.sequence,
				ID:       live.RecordID(producer, row.sequence),
				Subject:  subject,
				Content: live.Content{
					OccurredAt: row.at,
					Kind:       "assistant.activity",
					Resource:   live.Resource{Type: "assistant-run", ID: runKey, Revision: row.sequence},
					Data:       payload,
				},
			})
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Producer == records[j].Producer {
			return records[i].Position < records[j].Position
		}
		return records[i].Producer < records[j].Producer
	})
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func parseActivitySequence(raw any) (uint64, bool) {
	switch v := raw.(type) {
	case uint64:
		return v, true
	case int:
		if v > 0 {
			return uint64(v), true
		}
	case float64:
		if v > 0 && float64(uint64(v)) == v {
			return uint64(v), true
		}
	}
	return 0, false
}

func knownAssistantActivity(activity string) bool {
	switch activity {
	case "STARTED", "WAITING_INPUT", "PLANNING", "FINISHED", "FAILED":
		return true
	}
	return false
}

type liveContextReadiness struct{}

func (liveContextReadiness) Check(ctx context.Context) error { return ctx.Err() }

type unavailableLiveSource struct{}

func (unavailableLiveSource) ReadCommittedEvents(context.Context, map[string]uint64, int) ([]live.Record, error) {
	return nil, live.ErrUnavailable
}

// Refresh is demand-driven, bounded and shared by all subscribers. A departing
// operator cannot cancel a source read for everybody else. No goroutine or
// periodic source polling runs in the background; native outboxes retain idle writes.
type liveDrainer interface {
	DrainCommitted(context.Context, string, int) (liveapp.DrainResult, error)
}

func newLiveRefresh(root context.Context, service liveDrainer, names []string) func(context.Context) error {
	gate := make(chan struct{}, 1)
	last := time.Time{}
	return func(ctx context.Context) error {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		defer func() { <-gate }()
		if err := ctx.Err(); err != nil {
			return err
		}
		if root.Err() != nil {
			return live.ErrUnavailable
		}
		if !last.IsZero() && time.Since(last) < time.Second {
			return nil
		}
		poll, cancel := context.WithTimeout(root, 10*time.Second)
		defer cancel()
		for _, name := range names {
			if _, err := service.DrainCommitted(poll, name, 4); err != nil {
				last = time.Time{}
				if errors.Is(err, live.ErrReset) {
					return live.ErrReset
				}
				return live.ErrUnavailable
			}
		}
		last = time.Now()
		return nil
	}
}
