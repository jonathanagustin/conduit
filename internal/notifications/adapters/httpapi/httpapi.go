// Package httpapi is the inbound HTTP adapter for notifications: it turns a
// request into a question the application layer already answers, and refuses
// the caller who is not entitled to ask it.
//
// Nothing here decides what a notification is, who a fan-out reaches, or how
// many rows a read may return -- the domain and the application layer own all
// three, and this package would be the wrong place to restate any of them. What
// it owns is the one thing a transport is uniquely able to get wrong: an inbox
// is per-principal, and a request naming somebody else's inbox arrives looking
// exactly like a request naming your own.
//
// The routes are a Conduit extension. The pinned Apollo OpenAPI under
// docs/apollo/snapshots/ publishes no notification operation at all -- the word
// does not appear in it -- so there is no shape here to be faithful to, and
// pretending otherwise by modelling one on a neighbouring Apollo response would
// manufacture a resemblance nothing supports.
//
// CONDUIT-INVENTED: notifications.HTTPSurface
// Displaced-by: a capture of Apollo's notification Hub API -- its route paths,
// its request and response shapes, and how it scopes an inbox read to a caller
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

// OperationReadResource is the operation every route here asks Governance for
// by name, spelled rather than imported so that notifications depends on no
// other bounded context's package.
//
// The spelling is held to Governance's by a test in the composition root, which
// is where the two meet. Drifting apart would fail closed -- an operation no
// grant conveys authorizes nobody -- but "closed" here means every subscriber
// silently loses their settings page, so it is worth a test rather than a
// comfortable argument.
const OperationReadResource = "resource:read"

// Principal is who is asking, reduced to the two facts a notification decision
// needs: the name an inbox is keyed on, and the teams this person is inside.
//
// Reduced deliberately. Handing the whole Governance principal to this adapter
// would let a later edit here reach delegation, service-account status or group
// membership and reinvent a rule that has one home.
type Principal struct {
	Username string
	Teams    []string
}

// ResolvePrincipal establishes who is asking, or says why nobody is.
//
// It is supplied rather than implemented here because the credential is the
// deployment's business -- OIDC, a verified client certificate, or a
// development header behind an explicit opt-in -- and an adapter that read one
// itself would be a second place identity is decided.
type ResolvePrincipal func(*http.Request) (Principal, error)

// Authorize reports whether this principal may perform this operation on this
// resource. Governance answers it; this package only asks.
type Authorize func(ctx context.Context, principal Principal, resourceRID, operation string) bool

// StatusError lets whoever resolves identity say what a failure means, since
// only they can tell an absent credential from a rejected delegation.
type StatusError struct {
	Status  int
	Message string
}

func (err StatusError) Error() string { return err.Message }

// Config is everything these routes need and nothing they do not.
type Config struct {
	Reader      *application.Reader
	Preferences *application.Preferences
	Resolve     ResolvePrincipal
	Authorize   Authorize
}

// Register mounts the notification routes on a mux.
//
// It returns an error rather than mounting a partial surface: a Hub that came
// up serving three of four routes, or serving them with no authorizer, is worse
// than one that refused to start.
func Register(mux *http.ServeMux, config Config) error {
	switch {
	case mux == nil:
		return errors.New("a mux is required to mount the notification routes on")
	case config.Reader == nil:
		return errors.New("a notification reader is required to serve an inbox")
	case config.Preferences == nil:
		return errors.New("a notification preference service is required to serve settings")
	case config.Resolve == nil:
		return errors.New("an identity resolver is required; routes that cannot say who is asking cannot refuse anybody")
	case config.Authorize == nil:
		return errors.New("an authorizer is required; routes that cannot ask Governance would answer for it")
	}
	api := &api{config: config}
	mux.HandleFunc("GET /v1/notifications/inbox", api.readInbox)
	mux.HandleFunc("GET /v1/notifications/events", api.readEvents)
	mux.HandleFunc("GET /v1/notifications/preferences", api.readPreference)
	mux.HandleFunc("PUT /v1/notifications/preferences", api.writePreference)
	// Everything else under the prefix answers here rather than falling through
	// to whatever the mux has at "/". A Hub that composes these routes in front
	// of a legacy facade would otherwise answer 404 to DELETE on a route it
	// serves, which reads as "no such route" to the one caller who most needs
	// to be told they used the wrong verb.
	mux.HandleFunc("/v1/notifications/{path...}", api.unsupported)
	return nil
}

// served maps each route to the methods it answers, which is both the 405
// decision and the Allow header the answer has to carry.
var served = map[string]string{
	"inbox":       http.MethodGet,
	"events":      http.MethodGet,
	"preferences": http.MethodGet + ", " + http.MethodPut,
}

func (api *api) unsupported(writer http.ResponseWriter, request *http.Request) {
	route := request.PathValue("path")
	allowed, known := served[route]
	if !known {
		writeError(writer, http.StatusNotFound,
			fmt.Errorf("/v1/notifications/%s is not a notification route", route))
		return
	}
	writer.Header().Set("Allow", allowed)
	writeError(writer, http.StatusMethodNotAllowed, fmt.Errorf(
		"/v1/notifications/%s answers %s, not %s", route, allowed, request.Method))
}

type api struct{ config Config }

// readInbox answers what reached one recipient.
//
// The owner defaults to the caller, so the common request carries no owner at
// all and cannot name the wrong one. Naming an owner explicitly is what makes a
// team inbox reachable, and it is exactly the parameter that has to be refused
// when it names somebody else.
func (api *api) readInbox(writer http.ResponseWriter, request *http.Request) {
	principal, ok := api.principal(writer, request)
	if !ok {
		return
	}
	owner, err := requestedOwner(request.URL.Query(), principal)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if !ownedBy(principal, owner) {
		refuseOwner(writer, owner)
		return
	}
	channel, err := requestedChannel(request.URL.Query().Get("channel"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	limit, err := requestedLimit(request.URL.Query().Get("limit"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	entries, err := api.config.Reader.Inbox(
		request.Context(), domain.Recipient{Owner: owner, Channel: channel}, limit)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	// The applied limit is reported rather than the requested one. A caller
	// that asked for 500 and received 100 has to be able to tell a bounded page
	// from the end of the inbox, and the count alone cannot say which it was.
	writeJSON(writer, http.StatusOK, map[string]any{
		"owner": owner, "channel": channel, "limit": application.Bound(limit), "entries": entries,
	})
}

// readEvents answers what the Hub raised about a resource, subscribed to or
// not. This is the read that makes "nobody was told" answerable, and it is
// about a resource rather than a person, so it is authorized as a resource read
// and carries no owner.
func (api *api) readEvents(writer http.ResponseWriter, request *http.Request) {
	principal, ok := api.principal(writer, request)
	if !ok {
		return
	}
	resourceRID := strings.TrimSpace(request.URL.Query().Get("resourceRid"))
	if resourceRID == "" {
		writeError(writer, http.StatusBadRequest,
			errors.New("reading notification events requires the resource they are about"))
		return
	}
	if !api.authorized(writer, request, principal, resourceRID) {
		return
	}
	limit, err := requestedLimit(request.URL.Query().Get("limit"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	events, err := api.config.Reader.EventsForResource(request.Context(), resourceRID, limit)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"resourceRid": resourceRID, "limit": application.Bound(limit), "events": events,
	})
}

func (api *api) readPreference(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	principal, ok := api.principal(writer, request)
	if !ok {
		return
	}
	owner, err := requestedOwner(query, principal)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if !ownedBy(principal, owner) {
		refuseOwner(writer, owner)
		return
	}
	channel, err := requestedChannel(query.Get("channel"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	resourceRID := strings.TrimSpace(query.Get("resourceRid"))
	if resourceRID == "" {
		writeError(writer, http.StatusBadRequest,
			errors.New("a notification preference is about one resource, which was not named"))
		return
	}
	if !api.authorized(writer, request, principal, resourceRID) {
		return
	}
	preference, err := api.config.Preferences.Get(request.Context(), resourceRID, owner, channel)
	switch {
	case errors.Is(err, application.ErrNoPreference):
		writeError(writer, http.StatusNotFound, err)
		return
	case err != nil:
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	writeJSON(writer, http.StatusOK, preference)
}

// preferenceRequest is what a caller may set. The identifier, the timestamps
// and the revision are absent because the application layer mints them; a body
// that could carry them would let a client backdate its own subscription.
type preferenceRequest struct {
	Owner          domain.Owner       `json:"owner"`
	ResourceRID    string             `json:"resourceRid"`
	Channel        domain.Channel     `json:"channel"`
	Types          []domain.EventType `json:"types,omitempty"`
	AllFutureTypes bool               `json:"allFutureTypes,omitempty"`
	ContactID      string             `json:"contactId,omitempty"`
}

func (api *api) writePreference(writer http.ResponseWriter, request *http.Request) {
	principal, ok := api.principal(writer, request)
	if !ok {
		return
	}
	var body preferenceRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maximumPreferenceBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(writer, http.StatusBadRequest, fmt.Errorf("decode notification preference: %w", err))
		return
	}
	owner, err := defaultedOwner(body.Owner, principal)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if !ownedBy(principal, owner) {
		refuseOwner(writer, owner)
		return
	}
	resourceRID := strings.TrimSpace(body.ResourceRID)
	if resourceRID == "" {
		writeError(writer, http.StatusBadRequest,
			errors.New("a notification preference is about one resource, which was not named"))
		return
	}
	if !api.authorized(writer, request, principal, resourceRID) {
		return
	}
	stored, err := api.config.Preferences.Save(request.Context(), domain.Preference{
		Owner: owner, ResourceRID: resourceRID, Channel: body.Channel,
		Types: body.Types, AllFutureTypes: body.AllFutureTypes, ContactID: body.ContactID,
	})
	if err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(writer, http.StatusOK, stored)
}

// maximumPreferenceBytes bounds a body whose largest legitimate form is a list
// of event-type names.
const maximumPreferenceBytes = 64 << 10

// principal resolves the caller or writes the refusal and reports false.
func (api *api) principal(writer http.ResponseWriter, request *http.Request) (Principal, bool) {
	principal, err := api.config.Resolve(request)
	if err != nil {
		status := StatusError{}
		if errors.As(err, &status) && status.Status != 0 {
			writeError(writer, status.Status, err)
			return Principal{}, false
		}
		writeError(writer, http.StatusUnauthorized, err)
		return Principal{}, false
	}
	if strings.TrimSpace(principal.Username) == "" {
		writeError(writer, http.StatusUnauthorized,
			errors.New("an authenticated username is required to read notifications"))
		return Principal{}, false
	}
	return principal, true
}

// authorized asks Governance whether this caller may read this resource, and
// writes the refusal itself so no call site can forget to.
//
// Every route asks for the same operation, including the write. That is
// deliberate and is the divergence worth stating: the authority to change a
// personal subscription comes from owning it, which ownedBy has already
// established, and what the resource check adds is that nobody can subscribe to
// a resource they may not read -- which would otherwise be a way to have its
// events copied into an inbox one row at a time. Demanding resource:operate to
// tick a checkbox on your own settings page would instead deny it to exactly
// the read-only operators Apollo's own role prose says may manage their own
// subscriptions.
func (api *api) authorized(
	writer http.ResponseWriter, request *http.Request, principal Principal, resourceRID string,
) bool {
	if api.config.Authorize(request.Context(), principal, resourceRID, OperationReadResource) {
		return true
	}
	writeError(writer, http.StatusForbidden, fmt.Errorf(
		"forbidden: %s is not entitled to %s on %s", principal.Username, OperationReadResource, resourceRID))
	return false
}

// ownedBy is the whole reason this package is authorization-shaped.
//
// A user's inbox is theirs: not their team's to read, not an administrator's,
// not a service account's. A team's inbox is readable by its members, which is
// the For User / For Team split on Apollo's settings page and the closest thing
// to a published rule there is. Everything else is refused, including the cases
// that would be convenient -- there is no administrator override here, because
// "an operator with enough grants can read your mail" is a property somebody
// would have to discover by being surprised.
func ownedBy(principal Principal, owner domain.Owner) bool {
	switch owner.Kind {
	case domain.PrincipalUser:
		return owner.ID == principal.Username
	case domain.PrincipalTeam:
		for _, team := range principal.Teams {
			if team == owner.ID {
				return true
			}
		}
	}
	return false
}

func refuseOwner(writer http.ResponseWriter, owner domain.Owner) {
	// The refusal names the owner asked for and nothing about whether it exists.
	// A distinguishable "no such user" would turn this route into a directory.
	writeError(writer, http.StatusForbidden, fmt.Errorf(
		"notifications for %s %s belong to that owner", owner.Kind, owner.ID))
}

// requestedOwner reads the owner a request asked about, defaulting to the
// caller. An absent owner is the caller's own, which is why the ordinary
// request carries none.
func requestedOwner(query map[string][]string, principal Principal) (domain.Owner, error) {
	first := func(name string) string {
		values := query[name]
		if len(values) == 0 {
			return ""
		}
		return strings.TrimSpace(values[0])
	}
	owner := domain.Owner{Kind: domain.PrincipalKind(first("ownerKind")), ID: first("ownerId")}
	return defaultedOwner(owner, principal)
}

func defaultedOwner(owner domain.Owner, principal Principal) (domain.Owner, error) {
	owner.ID = strings.TrimSpace(owner.ID)
	if owner.Kind == "" {
		owner.Kind = domain.PrincipalUser
	}
	if owner.Kind == domain.PrincipalUser && owner.ID == "" {
		owner.ID = principal.Username
	}
	if owner.Kind != domain.PrincipalUser && owner.Kind != domain.PrincipalTeam {
		return domain.Owner{}, fmt.Errorf("notification owner kind %q must be USER or TEAM", owner.Kind)
	}
	if owner.ID == "" {
		return domain.Owner{}, errors.New("a TEAM notification owner requires the team it names")
	}
	return owner, nil
}

func requestedChannel(value string) (domain.Channel, error) {
	channel := domain.Channel(strings.TrimSpace(value))
	if !channel.Valid() {
		return "", fmt.Errorf(
			"notification channel %q is not one a resource can be configured on; use EMAIL or IN_APP", value)
	}
	return channel, nil
}

// requestedLimit reads a page size. Absent means "the ceiling", which is what
// the application layer already applies; a value it cannot parse is refused
// rather than silently treated as absent, because a caller who wrote limit=fifty
// asked a question and should not be told it was answered.
func requestedLimit(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(trimmed)
	if err != nil || limit < 1 {
		return 0, fmt.Errorf("limit %q must be a positive whole number", value)
	}
	return limit, nil
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}

func writeError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, map[string]string{"error": err.Error(), "status": strconv.Itoa(status)})
}
