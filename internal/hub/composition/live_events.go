package composition

import (
	"context"
	"errors"
	"net/http"
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
	governanceService := controlplane.NewGovernanceService(repository)
	resolve := func(ctx context.Context) (nativeauth.State, error) {
		return liveOperatorState(ctx, governanceService, options)
	}
	auth, err := nativeauth.New(nativeauth.Config{Resolve: resolve, Visible: func(ctx context.Context, principal governance.Principal, subject live.Subject) (bool, error) {
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
		sources := map[string]liveports.CommittedSource{"native": native, "packaging": packagingSource}
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
		refresh := newLiveRefresh(root, service, []string{"native", "packaging"})
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
				return live.ErrUnavailable
			}
		}
		last = time.Now()
		return nil
	}
}
