// Package httpapi is the inbound HTTP adapter for notifications: it turns a
// request into a question the application layer already answers, and refuses
// the caller who is not entitled to ask it.
//
// Nothing here decides what a notification is, who a fan-out reaches, or how
// many rows a read may return -- the domain and the application layer own all
// three, and this package would be the wrong place to restate any of them. What
// it owns is the one thing a transport is uniquely able to get wrong: an inbox
// is per-principal, and a request naming somebody else's inbox arrives looking
// exactly like a request naming your own. That is truer of the contact routes
// than of anything else here: a write that reached the wrong owner would not
// leak somebody's mail, it would redirect it.
//
// The routes are a Conduit extension. The pinned Apollo OpenAPI under
// docs/reference-platform/snapshots/ publishes no notification operation at all -- the word
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
//
// Contacts is the one optional member. A deployment whose notification
// component has no address book serves no contact routes rather than serving
// them against nothing and answering 500 -- the same rule the composition root
// applies to this whole surface when there is no store behind it.
type Config struct {
	Reader      *application.Reader
	Preferences *application.Preferences
	Contacts    *application.Contacts
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
	api.registerConnect(mux)
	mux.HandleFunc("GET /v1/notifications/inbox", api.readInbox)
	mux.HandleFunc("GET /v1/notifications/events", api.readEvents)
	mux.HandleFunc("GET /v1/notifications/preferences", api.readPreference)
	mux.HandleFunc("PUT /v1/notifications/preferences", api.writePreference)
	if config.Contacts != nil {
		mux.HandleFunc("GET /v1/notifications/contacts", api.readContacts)
		mux.HandleFunc("PUT /v1/notifications/contacts", api.writeContact)
		mux.HandleFunc("DELETE /v1/notifications/contacts", api.deleteContact)
	}
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
//
// It is a method rather than a map because one route is conditional: a Hub with
// no address book does not serve /v1/notifications/contacts, and answering 405
// for it there would tell a caller the route exists and they used the wrong
// verb. It does not, so the answer is 404.
func (api *api) served(route string) (string, bool) {
	switch route {
	case "inbox", "events":
		return http.MethodGet, true
	case "preferences":
		return http.MethodGet + ", " + http.MethodPut, true
	case "contacts":
		if api.config.Contacts == nil {
			return "", false
		}
		return http.MethodGet + ", " + http.MethodPut + ", " + http.MethodDelete, true
	}
	return "", false
}

func (api *api) unsupported(writer http.ResponseWriter, request *http.Request) {
	route := request.PathValue("path")
	allowed, known := api.served(route)
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
	page, err := api.eventPage(request.Context(), principal, request.URL.Query().Get("resourceRid"), request.URL.Query().Get("limit"))
	if err != nil {
		status := http.StatusInternalServerError
		var failure StatusError
		if errors.As(err, &failure) {
			status = failure.Status
		}
		writeError(writer, status, err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

type eventPage struct {
	ResourceRID string         `json:"resourceRid"`
	Limit       int            `json:"limit"`
	Events      []domain.Event `json:"events"`
}

// eventPage is the shared native resource read for REST and Connect.
func (api *api) eventPage(ctx context.Context, principal Principal, rawResource, rawLimit string) (eventPage, error) {
	resource := strings.TrimSpace(rawResource)
	if resource == "" {
		return eventPage{}, StatusError{Status: http.StatusBadRequest, Message: "reading notification events requires the resource they are about"}
	}
	if err := api.resourceReadError(ctx, principal, resource); err != nil {
		return eventPage{}, err
	}
	limit, err := requestedLimit(rawLimit)
	if err != nil {
		return eventPage{}, StatusError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	rows, err := api.config.Reader.EventsForResource(ctx, resource, limit)
	if err != nil {
		return eventPage{}, StatusError{Status: http.StatusInternalServerError, Message: err.Error()}
	}
	bounded := application.Bound(limit)
	if len(rows) > bounded {
		return eventPage{}, StatusError{Status: http.StatusInternalServerError, Message: "notification source returned an oversized event window"}
	}
	for _, row := range rows {
		if row.ResourceRID != resource {
			return eventPage{}, StatusError{Status: http.StatusInternalServerError, Message: "notification source returned an event for another resource"}
		}
	}
	return eventPage{ResourceRID: resource, Limit: bounded, Events: rows}, nil
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
		writeJSON(writer, http.StatusNotFound, map[string]any{
			"error": err.Error(), "typesNotYetEmitted": domain.NotYetRaised(domain.EventTypes()),
			"typeAvailabilityScope": typeAvailabilityScopeAllKnown,
		})
		return
	case err != nil:
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	writeJSON(writer, http.StatusOK, describedPreference(preference))
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
	writeJSON(writer, http.StatusOK, describedPreference(stored))
}

// preferenceResponse reports the full build coverage, including types not
// selected on this preference, so an editor can refuse unavailable choices
// before a save. Legacy saved selections stay readable and removable.
type preferenceResponse struct {
	domain.Preference
	TypesNotYetEmitted    []domain.EventType `json:"typesNotYetEmitted"`
	TypeAvailabilityScope string             `json:"typeAvailabilityScope"`
}

// Older Hubs returned unavailable types only among the saved selections. The
// scope distinguishes that partial list from complete build availability.
const typeAvailabilityScopeAllKnown = "ALL_KNOWN"

func describedPreference(preference domain.Preference) preferenceResponse {
	return preferenceResponse{
		Preference: preference, TypesNotYetEmitted: domain.NotYetRaised(domain.EventTypes()),
		TypeAvailabilityScope: typeAvailabilityScopeAllKnown,
	}
}

// maximumPreferenceBytes bounds a body whose largest legitimate form is a list
// of event-type names.
const maximumPreferenceBytes = 64 << 10

// The contact routes are where a person's email address or a team's Slack
// channel is written down, which makes them the routes with the most to lose.
// Three properties hold across all three handlers and none of them is optional:
//
// The owner defaults to the caller and is checked by ownedBy, exactly as the
// inbox is. There is no administrator override, for the reason ownedBy states:
// a grant conveys authority over a resource and never over a person. An
// operator who could rewrite somebody's address could redirect their mail.
//
// There is no resource authorization, and its absence is deliberate rather than
// an omission. A contact is not about a resource -- Apollo creates one in Hub
// Settings, and a resource only selects one that already exists -- so there is
// no resourceRid to ask Governance about, and inventing one would mean choosing
// which resource a person's address belongs to.
//
// An address is never written to a log, an error or a metric. It appears in
// exactly two places: the body its owner sent it in, and the body its owner
// reads it back from. Every refusal below names the owner and the kind, and
// nothing else.

// readContacts answers what destinations one owner holds.
//
// The address is in the answer, because the answer goes to the owner and to
// nobody else -- that is what ownedBy has just established. A response that
// withheld it would make the routes unusable for the one thing they exist for:
// checking what Conduit is about to send your mail to.
func (api *api) readContacts(writer http.ResponseWriter, request *http.Request) {
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
	contacts, err := api.config.Contacts.List(request.Context(), owner)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"owner": owner, "contacts": readableContacts(contacts)})
}

// contactResponse is domain.Contact plus the one thing a reader cannot work out
// from it: whether anything will ever be delivered here.
//
// ContactKind.Deliverable is EMAIL or SLACK; a PAGERDUTY contact is accepted by
// Save, listed back by List, and silently receives nothing (#262). Every client
// therefore had to carry its own copy of that predicate to say so -- the console
// did, as contactDeliverable in web/src/api/notifications.ts, and any client
// that did not simply showed a destination that is never used as though it were.
//
// Projected here rather than added to domain.Contact because it is derived, not
// stored: putting it on the domain type would persist an answer that
// Deliverable() already computes, and the two could then disagree.
type contactResponse struct {
	domain.Contact
	Deliverable bool `json:"deliverable"`
}

func readableContacts(contacts []domain.Contact) []contactResponse {
	readable := make([]contactResponse, 0, len(contacts))
	for _, contact := range contacts {
		readable = append(readable, contactResponse{Contact: contact, Deliverable: contact.Kind.Deliverable()})
	}
	return readable
}

// contactRequest is what a caller may set. The identifier and the timestamps
// are absent because the application layer mints them; a body that could carry
// them would let a client backdate a destination it had only just added.
type contactRequest struct {
	Owner                  domain.Owner       `json:"owner"`
	Kind                   domain.ContactKind `json:"kind"`
	Address                string             `json:"address"`
	OptIntoFutureResources bool               `json:"optIntoFutureResources"`
}

func (api *api) writeContact(writer http.ResponseWriter, request *http.Request) {
	principal, ok := api.principal(writer, request)
	if !ok {
		return
	}
	var body contactRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maximumContactBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		// The decoder's message can quote the offending token, so it is reported
		// as a shape failure rather than passed through. A malformed body that
		// nearly parsed could otherwise put an address in the response of a
		// request that was refused, and from there into whatever logs it.
		writeError(writer, http.StatusBadRequest,
			errors.New("the notification contact body is not the object this route accepts: "+
				"an owner, a kind, an address and optIntoFutureResources"))
		return
	}
	owner, err := defaultedOwner(body.Owner, principal)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	// The check this route exists to earn, and it happens before the address is
	// looked at, let alone stored. bob naming alice is refused here.
	if !ownedBy(principal, owner) {
		refuseOwner(writer, owner)
		return
	}
	stored, err := api.config.Contacts.Save(request.Context(), domain.Contact{
		Owner: owner, Kind: body.Kind, Address: body.Address,
		OptIntoFutureResources: body.OptIntoFutureResources,
	})
	if err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(writer, http.StatusOK, stored)
}

// deleteContact removes one destination, and answers 404 when there was none.
//
// The distinction is kept because the two mean different things to the person
// asking: "it is gone now" and "it was never here" are answers to a question
// about your own address book, which you are entitled to. There is no directory
// risk in it, because ownedBy has already established this is the caller's own.
func (api *api) deleteContact(writer http.ResponseWriter, request *http.Request) {
	principal, ok := api.principal(writer, request)
	if !ok {
		return
	}
	query := request.URL.Query()
	owner, err := requestedOwner(query, principal)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if !ownedBy(principal, owner) {
		refuseOwner(writer, owner)
		return
	}
	kind := domain.ContactKind(strings.TrimSpace(query.Get("kind")))
	deleted, err := api.config.Contacts.Delete(request.Context(), owner, kind)
	switch {
	case err != nil && !kind.Valid():
		writeError(writer, http.StatusBadRequest, err)
		return
	case err != nil:
		writeError(writer, http.StatusInternalServerError, err)
		return
	case !deleted:
		writeError(writer, http.StatusNotFound, fmt.Errorf(
			"%s %s holds no %s notification contact", owner.Kind, owner.ID, kind))
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"owner": owner, "kind": kind, "deleted": true})
}

// maximumContactBytes bounds a body whose largest legitimate form is one
// address. Smaller than the preference ceiling because there is no list in it.
const maximumContactBytes = 8 << 10

// principal resolves the caller or writes the refusal and reports false.
func (api *api) principal(writer http.ResponseWriter, request *http.Request) (Principal, bool) {
	principal, err := api.resolvePrincipal(request)
	if err != nil {
		status := StatusError{}
		if errors.As(err, &status) && status.Status != 0 {
			writeError(writer, status.Status, err)
			return Principal{}, false
		}
		writeError(writer, http.StatusUnauthorized, err)
		return Principal{}, false
	}
	return principal, true
}

func (api *api) resolvePrincipal(request *http.Request) (Principal, error) {
	principal, err := api.config.Resolve(request)
	if err != nil {
		return Principal{}, err
	}
	if strings.TrimSpace(principal.Username) == "" {
		return Principal{}, StatusError{Status: http.StatusUnauthorized, Message: "an authenticated username is required to read notifications"}
	}
	return principal, nil
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
	if err := api.resourceReadError(request.Context(), principal, resourceRID); err != nil {
		writeError(writer, http.StatusForbidden, err)
		return false
	}
	return true
}

func (api *api) resourceReadError(ctx context.Context, principal Principal, resourceRID string) error {
	if api.config.Authorize(ctx, principal, resourceRID, OperationReadResource) {
		return nil
	}
	return StatusError{Status: http.StatusForbidden, Message: fmt.Sprintf("forbidden: %s is not entitled to %s on %s", principal.Username, OperationReadResource, resourceRID)}
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
			"notification channel %q is not one a resource can be configured on; use EMAIL, IN_APP or SLACK", value)
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
