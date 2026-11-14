// Package httpapi serves the ontology catalog. Anonymous callers are refused
// before any row is read. Creating a change request goes through the source,
// which calls governance and does not approve the request itself.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/jonathanagustin/conduit/internal/ontology/domain"
	"github.com/jonathanagustin/conduit/internal/ontology/ports"
)

const (
	routeList               = "GET /api/v2/ontologies"
	routeOntology           = "GET /api/v2/ontologies/{ontology}"
	routeMetadata           = "GET /api/v2/ontologies/{ontology}/fullMetadata"
	routeObjectTypes        = "GET /api/v2/ontologies/{ontology}/objectTypes"
	routeObjectType         = "GET /api/v2/ontologies/{ontology}/objectTypes/{objectType}"
	routeObjectTypeMetadata = "GET /api/v2/ontologies/{ontology}/objectTypes/{objectType}/fullMetadata"
	routeInterfaceTypes     = "GET /api/v2/ontologies/{ontology}/interfaceTypes"
	routeInterfaceType      = "GET /api/v2/ontologies/{ontology}/interfaceTypes/{interfaceType}"
	routeActionTypes        = "GET /api/v2/ontologies/{ontology}/actionTypes"
	routeActionType         = "GET /api/v2/ontologies/{ontology}/actionTypes/{actionType}"
	routeLoadObjects        = "POST /api/v2/ontologies/{ontology}/objectSets/loadObjects"
	routeApply              = "POST /api/v2/ontologies/{ontology}/actions/{action}/apply"
)

// StatusError is an authorization failure with an HTTP status.
type StatusError struct {
	Status  int
	Message string
}

func (e StatusError) Error() string {
	if e.Message == "" {
		return "unauthorized"
	}
	return e.Message
}

// Config is the route's dependencies. A nil Authorize refuses everyone, which
// is what an unconfigured deployment must do.
type Config struct {
	Source    ports.Source
	Authorize func(*http.Request) error
	Allow     func(*http.Request, string) bool
	Caller    func(*http.Request) (ports.Caller, error)
	Permit    func(*http.Request, string) error
	// Execute performs a workspace action after the server allows it. Nil leaves
	// the decision as an audit record only.
	Execute func(ctx context.Context, caller ports.Caller, action string, parameters map[string]string) error
	// Audit records an allow or deny on the authorization hash chain. Nil
	// refuses the governed action instead of skipping the record.
	Audit func(ctx context.Context, subject, action, resource, decision, reason string) error
	// Facts loads server-side observations for an action. The caller cannot
	// supply them. Nil facts fail closed.
	Facts func(ctx context.Context, action, resource string) (domain.ActionFacts, error)
}

// Register mounts the ontology routes.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(routeList, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) {
			return
		}
		writeList(w, r, []domain.Ontology{domain.OntologyDocument()})
	})
	mux.HandleFunc(routeOntology, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) || !knownOntology(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, domain.OntologyDocument())
	})
	mux.HandleFunc(routeMetadata, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) || !knownOntology(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, domain.Metadata())
	})
	mux.HandleFunc(routeObjectTypes, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) || !knownOntology(w, r) {
			return
		}
		types := make([]domain.ObjectTypeV2, 0, len(domain.Types()))
		for _, spec := range domain.Types() {
			objectType, _ := domain.ObjectTypeByName(spec.APIName)
			types = append(types, objectType)
		}
		writeList(w, r, types)
	})
	mux.HandleFunc(routeObjectType, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) || !knownOntology(w, r) {
			return
		}
		objectType, ok := domain.ObjectTypeByName(r.PathValue("objectType"))
		if !ok {
			writeError(w, http.StatusNotFound, "object type not found")
			return
		}
		writeJSON(w, http.StatusOK, objectType)
	})
	mux.HandleFunc(routeObjectTypeMetadata, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) || !knownOntology(w, r) {
			return
		}
		full, ok := domain.ObjectTypeFullByName(r.PathValue("objectType"))
		if !ok {
			writeError(w, http.StatusNotFound, "object type not found")
			return
		}
		writeJSON(w, http.StatusOK, full)
	})
	mux.HandleFunc(routeInterfaceTypes, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) || !knownOntology(w, r) {
			return
		}
		writeList(w, r, []any{})
	})
	mux.HandleFunc(routeInterfaceType, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) || !knownOntology(w, r) {
			return
		}
		writeError(w, http.StatusNotFound, "interface type not found")
	})
	mux.HandleFunc(routeActionTypes, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) || !knownOntology(w, r) {
			return
		}
		action, _ := domain.ActionByName(domain.ActionCreateChange)
		names := make([]string, 0, len(domain.Metadata().ActionTypes))
		listed := make([]domain.ActionTypeV2, 0, len(names))
		for name := range domain.Metadata().ActionTypes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			item, _ := domain.ActionByName(name)
			listed = append(listed, item)
		}
		if len(listed) == 0 {
			listed = []domain.ActionTypeV2{action}
		}
		writeList(w, r, listed)
	})
	mux.HandleFunc(routeActionType, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) || !knownOntology(w, r) {
			return
		}
		action, ok := domain.ActionByName(r.PathValue("actionType"))
		if !ok {
			writeError(w, http.StatusNotFound, "action type not found")
			return
		}
		writeJSON(w, http.StatusOK, action)
	})
	mux.HandleFunc(routeLoadObjects, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) || !knownOntology(w, r) {
			return
		}
		loadObjects(cfg, w, r)
	})
	mux.HandleFunc(routeApply, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) || !knownOntology(w, r) {
			return
		}
		applyAction(cfg, w, r)
	})
}

func authorized(cfg Config, w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return false
	}
	if cfg.Authorize == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	if err := cfg.Authorize(r); err != nil {
		status, message := refusal(err)
		writeError(w, status, message)
		return false
	}
	return true
}

func knownOntology(w http.ResponseWriter, r *http.Request) bool {
	if domain.KnownOntology(r.PathValue("ontology")) {
		return true
	}
	writeError(w, http.StatusNotFound, "ontology not found")
	return false
}

func loadObjects(cfg Config, w http.ResponseWriter, r *http.Request) {
	if cfg.Source == nil {
		writeError(w, http.StatusBadGateway, "ontology catalog is unavailable")
		return
	}
	var body struct {
		ObjectSet  json.RawMessage `json:"objectSet"`
		Select     []string        `json:"select"`
		PageSize   *int            `json:"pageSize"`
		PageToken  string          `json:"pageToken"`
		ExcludeRid bool            `json:"excludeRid"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid JSON")
		return
	}
	set, err := parseSet(body.ObjectSet)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	catalog, err := cfg.Source.Catalog(r.Context(), func(environmentID string) bool {
		if cfg.Allow == nil {
			return true
		}
		return cfg.Allow(r, environmentID)
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "ontology catalog is unavailable")
		return
	}
	objects, err := domain.Resolve(catalog, set)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	if err := validateSelect(objects, body.Select); err != nil {
		writeQueryError(w, err)
		return
	}
	pageSize := 0
	if body.PageSize != nil {
		pageSize = *body.PageSize
	}
	page, err := domain.PageOf(objects, pageSize, body.PageToken)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	data := make([]map[string]any, 0, len(page.Objects))
	for _, object := range page.Objects {
		data = append(data, objectJSON(object, body.Select, body.ExcludeRid))
	}
	payload := map[string]any{"data": data, "totalCount": strconv.Itoa(page.TotalCount)}
	if page.NextPageToken != "" {
		payload["nextPageToken"] = page.NextPageToken
	}
	writeJSON(w, http.StatusOK, payload)
}

func enforceAction(cfg Config, w http.ResponseWriter, r *http.Request, actionName string) {
	if cfg.Caller == nil || cfg.Audit == nil {
		writeError(w, http.StatusBadGateway, "ontology actions are unavailable")
		return
	}
	caller, err := cfg.Caller(r)
	if err != nil {
		status, message := refusal(err)
		writeError(w, status, message)
		return
	}
	var body struct {
		Parameters map[string]json.RawMessage `json:"parameters"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid JSON")
		return
	}
	resource := strings.TrimSpace(scalarOrEmpty(body.Parameters["resource"]))
	if resource == "" {
		resource = strings.TrimSpace(scalarOrEmpty(body.Parameters["workspaceId"]))
	}
	if resource == "" {
		resource = "action/" + actionName
	}
	parameters := map[string]string{}
	for name, raw := range body.Parameters {
		if value, ok := scalar(raw); ok {
			parameters[name] = value
		}
	}
	facts := domain.ActionFacts{Roles: append([]string{}, caller.Teams...)}
	facts.Roles = append(facts.Roles, caller.Groups...)
	if strings.HasPrefix(actionName, "workspace_") {
		facts.Confirmation = parameters["confirmation"]
		if parameters["approve"] == "true" {
			facts.CallerAlreadySigned = true
		}
	}
	if cfg.Facts != nil {
		observed, err := cfg.Facts(r.Context(), actionName, resource)
		if err != nil {
			writeError(w, http.StatusBadGateway, "action facts are unavailable")
			return
		}
		observed.Roles = facts.Roles
		observed.Confirmation = facts.Confirmation
		if facts.CallerAlreadySigned {
			observed.CallerAlreadySigned = true
		}
		facts = observed
	}
	decision := domain.Decide(actionName, facts)
	verdict := "DENY"
	if decision.Allowed {
		verdict = "ALLOW"
	}
	if err := cfg.Audit(r.Context(), caller.Username, decision.AuditEvent, resource, verdict, decision.Reason); err != nil {
		writeError(w, http.StatusBadGateway, "the audit record could not be written")
		return
	}
	if !decision.Allowed {
		writeValidation(w, domain.ApplyResult{SubmissionMessage: decision.Reason})
		return
	}
	if cfg.Execute != nil && strings.HasPrefix(actionName, "workspace_") {
		if err := cfg.Execute(r.Context(), caller, actionName, parameters); err != nil {
			writeValidation(w, domain.ApplyResult{SubmissionMessage: err.Error()})
			return
		}
	}
	writeValidation(w, domain.ApplyResult{Valid: true})
}

func scalarOrEmpty(raw json.RawMessage) string {
	value, ok := scalar(raw)
	if !ok {
		return ""
	}
	return value
}

func applyAction(cfg Config, w http.ResponseWriter, r *http.Request) {
	if cfg.Source == nil || cfg.Caller == nil {
		writeError(w, http.StatusBadGateway, "ontology actions are unavailable")
		return
	}
	actionName := r.PathValue("action")
	if _, governed := domain.LookupAction(actionName); governed {
		enforceAction(cfg, w, r, actionName)
		return
	}
	if _, ok := domain.ActionByName(actionName); !ok {
		writeError(w, http.StatusNotFound, "action type not found")
		return
	}
	var body struct {
		Parameters map[string]json.RawMessage `json:"parameters"`
		Options    struct {
			Mode string `json:"mode"`
		} `json:"options"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid JSON")
		return
	}
	parameters := map[string]string{}
	for name, raw := range body.Parameters {
		value, ok := scalar(raw)
		if !ok {
			writeValidation(w, domain.ApplyResult{ParameterMessages: map[string]string{name: "parameter must be a string"}})
			return
		}
		parameters[name] = value
	}
	if environmentID := strings.TrimSpace(parameters["environment"]); environmentID != "" && cfg.Permit != nil {
		if err := cfg.Permit(r, environmentID); err != nil {
			status, message := refusal(err)
			writeError(w, status, message)
			return
		}
	}
	caller, err := cfg.Caller(r)
	if err != nil {
		status, message := refusal(err)
		writeError(w, status, message)
		return
	}
	result, err := cfg.Source.Apply(r.Context(), caller, domain.ApplyRequest{
		Action: actionName, Parameters: parameters, Mode: body.Options.Mode,
	})
	if errors.Is(err, domain.ErrUnknownAction) {
		writeError(w, http.StatusNotFound, "action type not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "the change request could not be evaluated")
		return
	}
	writeValidation(w, result)
}

func writeValidation(w http.ResponseWriter, result domain.ApplyResult) {
	parameters := map[string]any{}
	names := map[string]bool{}
	for name := range result.ParameterMessages {
		names[name] = true
	}
	for _, name := range domain.RequiredActionParameters() {
		names[name] = true
	}
	outcome := domain.ResultInvalid
	if result.Valid && len(result.ParameterMessages) == 0 {
		outcome = domain.ResultValid
	}
	for name := range names {
		entry := map[string]any{"result": domain.ResultValid, "required": requiredParam(name)}
		if message := result.ParameterMessages[name]; message != "" {
			entry["result"] = domain.ResultInvalid
			entry["evaluatedConstraints"] = []any{map[string]string{"result": domain.ResultInvalid, "message": message}}
		}
		parameters[name] = entry
	}
	validation := map[string]any{
		"result": outcome, "parameters": parameters, "submissionCriteria": []any{},
	}
	if result.SubmissionMessage != "" && outcome == domain.ResultInvalid && len(result.ParameterMessages) == 0 {
		validation["submissionCriteria"] = []any{map[string]string{
			"result": domain.ResultInvalid, "configuredFailureMessage": result.SubmissionMessage,
		}}
	}
	payload := map[string]any{"validation": validation}
	if result.Stored && result.ChangeRequestID != "" {
		payload["edits"] = map[string]any{
			"type": "edits",
			"edits": []any{map[string]string{
				"type": "addObject", "objectType": "ChangeRequest", "primaryKey": result.ChangeRequestID,
			}},
			"addedObjectCount": 1, "modifiedObjectsCount": 0, "deletedObjectsCount": 0,
		}
	}
	writeJSON(w, http.StatusOK, payload)
}

func requiredParam(name string) bool {
	for _, required := range domain.RequiredActionParameters() {
		if required == name {
			return true
		}
	}
	return false
}

func objectJSON(object domain.Object, selected []string, excludeRid bool) map[string]any {
	out := map[string]any{
		"__primaryKey": object.PrimaryKey,
		"__apiName":    object.Type,
		"__title":      object.Title(),
	}
	if !excludeRid {
		out["__rid"] = object.RID()
	}
	write := func(key, value string) {
		out[key] = domain.WireValue(object.Type, key, value)
	}
	if len(selected) == 0 {
		for key, value := range object.Properties {
			write(key, value)
		}
		return out
	}
	for _, key := range selected {
		if key == "__primaryKey" || key == "__apiName" || key == "__rid" || key == "__title" {
			continue
		}
		if value, ok := object.Properties[key]; ok {
			write(key, value)
		}
	}
	return out
}

func validateSelect(objects []domain.Object, selected []string) error {
	if len(selected) == 0 || len(objects) == 0 {
		return nil
	}
	apiName := objects[0].Type
	for _, key := range selected {
		if key == "__primaryKey" || key == "__apiName" || key == "__rid" || key == "__title" {
			continue
		}
		if !domain.KnownProperty(apiName, key) {
			return domain.ErrBadQuery
		}
	}
	return nil
}

func writeQueryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrUnknownType):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

type rawSet struct {
	Type       string          `json:"type"`
	ObjectType string          `json:"objectType"`
	ObjectSet  json.RawMessage `json:"objectSet"`
	Where      json.RawMessage `json:"where"`
	Link       string          `json:"link"`
}

func parseSet(raw json.RawMessage) (domain.ObjectSet, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return domain.ObjectSet{}, errors.New("objectSet is required")
	}
	var decoded rawSet
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return domain.ObjectSet{}, errors.New("objectSet is not valid")
	}
	set := domain.ObjectSet{Type: decoded.Type, ObjectType: decoded.ObjectType, Link: decoded.Link}
	if len(decoded.ObjectSet) > 0 && string(decoded.ObjectSet) != "null" {
		inner, err := parseSet(decoded.ObjectSet)
		if err != nil {
			return domain.ObjectSet{}, err
		}
		set.Inner = &inner
	}
	if len(decoded.Where) > 0 && string(decoded.Where) != "null" {
		where, err := parseWhere(decoded.Where)
		if err != nil {
			return domain.ObjectSet{}, err
		}
		set.Where = &where
	}
	return set, nil
}

func parseWhere(raw json.RawMessage) (domain.Where, error) {
	var decoded struct {
		Type  string          `json:"type"`
		Field string          `json:"field"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return domain.Where{}, errors.New("where is not valid")
	}
	where := domain.Where{Type: decoded.Type, Field: decoded.Field}
	switch decoded.Type {
	case "eq":
		value, ok := scalar(decoded.Value)
		if !ok {
			return domain.Where{}, errors.New("eq value must be a string or number")
		}
		where.Value = value
	case "and", "or":
		var clauses []json.RawMessage
		if err := json.Unmarshal(decoded.Value, &clauses); err != nil {
			return domain.Where{}, errors.New(decoded.Type + " value must be a list")
		}
		for _, clause := range clauses {
			parsed, err := parseWhere(clause)
			if err != nil {
				return domain.Where{}, err
			}
			where.Clauses = append(where.Clauses, parsed)
		}
	case "not":
		parsed, err := parseWhere(decoded.Value)
		if err != nil {
			return domain.Where{}, err
		}
		where.Clauses = []domain.Where{parsed}
	default:
		return domain.Where{}, domain.ErrBadQuery
	}
	return where, nil
}

func scalar(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, true
	}
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		return number.String(), true
	}
	return "", false
}

func decode(r *http.Request, dest any) error {
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dest); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("request body is required")
		}
		return err
	}
	return nil
}

func writeList[T any](w http.ResponseWriter, r *http.Request, rows []T) {
	pageSize := len(rows)
	if raw := r.URL.Query().Get("pageSize"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 500 {
			writeError(w, http.StatusBadRequest, "pageSize is invalid")
			return
		}
		pageSize = parsed
	}
	offset := 0
	if raw := r.URL.Query().Get("pageToken"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "pageToken is invalid")
			return
		}
		offset = parsed
	}
	if offset > len(rows) {
		offset = len(rows)
	}
	end := offset + pageSize
	if end > len(rows) {
		end = len(rows)
	}
	payload := map[string]any{"data": rows[offset:end]}
	if end < len(rows) {
		payload["nextPageToken"] = strconv.Itoa(end)
	}
	writeJSON(w, http.StatusOK, payload)
}

func refusal(err error) (int, string) {
	var coded StatusError
	if errors.As(err, &coded) && coded.Status != 0 {
		message := coded.Message
		if message == "" {
			message = "unauthorized"
		}
		return coded.Status, message
	}
	return http.StatusUnauthorized, "unauthorized"
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"errorCode": statusText(status), "errorName": statusText(status),
		"errorInstanceId": newErrorID(), "parameters": map[string]any{"message": message},
	})
}

func newErrorID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:])
}

func statusText(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "INVALID_ARGUMENT"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	default:
		return "INTERNAL"
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
