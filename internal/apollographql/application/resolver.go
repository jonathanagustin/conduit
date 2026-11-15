// Package application answers a GraphQL query against the Apollo-shaped
// interface Conduit serves.
//
// Three things are deliberately not done here.
//
// It does not parse or validate GraphQL. gqlparser does, against the served
// SDL, so a query naming a field the schema does not declare is rejected before
// any resolver runs. Hand-rolling that would have made the interface's contract
// a property of this file rather than of the schema the provenance gate governs.
//
// It does not decide what a caller may see. The ports it reads are already
// authorized: Reporting filters before counting, and the Apollo projection
// serves what the REST surface serves. Filtering again here would put a second,
// drifting copy of Governance's rules in a read path.
//
// It does not answer a coordinate absent from the coverage table, even when the
// data happens to be at hand. Coverage is data so it can be reconciled against
// the schema; a resolver that could quietly exceed it would make that
// reconciliation meaningless.
package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"github.com/vektah/gqlparser/v2/validator"
	"github.com/vektah/gqlparser/v2/validator/rules"

	apollo "github.com/jonathanagustin/conduit/internal/apollo"
	"github.com/jonathanagustin/conduit/internal/apollographql/domain"
	"github.com/jonathanagustin/conduit/internal/apollographql/ports"
	"github.com/jonathanagustin/conduit/internal/apollographql/schema"
	reporting "github.com/jonathanagustin/conduit/internal/reporting/domain"
)

// staleAfter is how old a report may be before the fleet calls it stale. The
// overview already owns this decision for the console; the value is restated
// rather than plumbed because a GraphQL reader asking about health wants the
// same answer the console shows, and a second knob would let them diverge.
const staleAfter = 15 * time.Minute

// validationRules is the spec's own rule set, taken whole. Conduit adds nothing
// and removes nothing: what this interface will answer is decided by the served
// schema, and a locally-relaxed validator would make it decided here instead.
var validationRules = rules.NewDefaultRules()

// Resolver holds the loaded interface. It is built once and shared: the schema,
// the provenance it declares and the coverage it reconciles against do not vary
// by caller.
//
// Sources do vary by caller -- Reporting's snapshot is filtered for the
// principal that asked -- so they are bound per request rather than held here.
// A resolver that captured one reader's sources would answer a second reader
// from the first one's authorization.
type Resolver struct {
	schema     *ast.Schema
	provenance domain.Interface
	now        func() time.Time
}

// bound is one resolver serving one request's sources.
type bound struct {
	*Resolver
	sources ports.Sources
}

// New loads the served interface and fails if the resolver could not honestly
// serve it. Reconcile's defects are refused at construction rather than at
// request time: a Conduit-native field nothing backs is a promise the process
// should not start having made.
func New() (*Resolver, error) {
	loaded, err := gqlparser.LoadSchema(&ast.Source{Name: schema.Name, Input: schema.SDL()})
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", schema.Name, err)
	}
	provenance, err := domain.ReadProvenance(schema.SDL())
	if err != nil {
		return nil, fmt.Errorf("read provenance from %s: %w", schema.Name, err)
	}
	if defects := domain.Defects(domain.Reconcile(provenance, coverage)); len(defects) > 0 {
		reasons := make([]string, 0, len(defects))
		for _, defect := range defects {
			reasons = append(reasons, defect.String())
		}
		return nil, fmt.Errorf("the served interface and this resolver disagree:\n\t%s", strings.Join(reasons, "\n\t"))
	}
	return &Resolver{schema: loaded, provenance: provenance, now: time.Now}, nil
}

// Schema is the loaded interface, so a caller can serve introspection without
// loading a second copy that could differ from the one answering queries.
func (resolver *Resolver) Schema() *ast.Schema { return resolver.schema }

// Request is one GraphQL operation, in the shape GraphQL over HTTP sends it.
type Request struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName,omitempty"`
	Variables     map[string]any `json:"variables,omitempty"`
}

// Response is one GraphQL response. Provenance rides in extensions rather than
// in data: it describes the answer rather than being part of it, and a client
// that ignores extensions still gets a spec-shaped response.
type Response struct {
	Data       map[string]any `json:"data,omitempty"`
	Errors     gqlerror.List  `json:"errors,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// Execute answers one request.
//
// Every response carries an extensions.provenance block naming the invented
// coordinates the query actually selected. That is the difference between a
// repository that documents its inventions and an interface that discloses
// them: a client reading Conduit's plans is told, in the response, that no
// Apollo Hub established the shape it just read.
func (resolver *Resolver) Execute(ctx context.Context, sources ports.Sources, request Request) Response {
	document, listErr := gqlparser.LoadQueryWithRules(resolver.schema, request.Query, validationRules)
	if listErr != nil {
		return Response{Errors: listErr}
	}
	operation := document.Operations.ForName(request.OperationName)
	if operation == nil {
		return Response{Errors: gqlerror.List{gqlerror.Errorf("no operation named %q in this document", request.OperationName)}}
	}
	variables, err := validator.VariableValues(resolver.schema, operation, request.Variables)
	if err != nil {
		return Response{Errors: gqlerror.List{gqlerror.WrapPath(nil, err)}}
	}

	walk := &walk{
		bound:     bound{Resolver: resolver, sources: sources},
		variables: variables,
		selected:  map[domain.Coordinate]domain.Tier{},
	}
	data := walk.selectionSet(ctx, resolver.schema.Query.Name, rootSource{}, operation.SelectionSet, nil)
	return Response{Data: data, Errors: walk.errors, Extensions: walk.provenanceExtension()}
}

// --- the walk ---------------------------------------------------------------

type rootSource struct{}

// child is a resolved object together with the schema type it is to be read as.
// Carrying the type name alongside the value is what lets one switch resolve
// every field without the Go types having to mirror the GraphQL ones.
type child struct {
	typeName string
	source   any
}

type walk struct {
	bound
	variables map[string]any
	errors    gqlerror.List
	selected  map[domain.Coordinate]domain.Tier
}

func (state *walk) selectionSet(
	ctx context.Context, typeName string, source any, set ast.SelectionSet, path ast.Path,
) map[string]any {
	if source == nil {
		return nil
	}
	result := map[string]any{}
	for _, selection := range set {
		switch selected := selection.(type) {
		case *ast.Field:
			state.field(ctx, typeName, source, selected, path, result)
		case *ast.InlineFragment:
			for key, value := range state.selectionSet(ctx, typeName, source, selected.SelectionSet, path) {
				result[key] = value
			}
		case *ast.FragmentSpread:
			if selected.Definition == nil {
				continue
			}
			for key, value := range state.selectionSet(ctx, typeName, source, selected.Definition.SelectionSet, path) {
				result[key] = value
			}
		}
	}
	return result
}

func (state *walk) field(
	ctx context.Context, typeName string, source any, field *ast.Field, path ast.Path, into map[string]any,
) {
	here := append(append(ast.Path{}, path...), ast.PathName(field.Alias))
	coordinate := domain.Coordinate{Type: typeName, Field: field.Name}

	// __typename and the introspection fields are answered by the schema, not by
	// Conduit's data, so they carry no provenance and need no coverage.
	if strings.HasPrefix(field.Name, "__") {
		if field.Name == "__typename" {
			into[field.Alias] = typeName
		}
		return
	}

	tier, declared := state.provenance.TierOf(coordinate)
	if !declared {
		// LoadQuery already rejected undeclared fields, so reaching this means
		// the SDL carries a field with no provenance annotation -- which
		// ReadProvenance refuses at construction. Stated rather than assumed.
		state.errors = append(state.errors, errorAt(here, fmt.Sprintf("%s carries no provenance", coordinate)))
		into[field.Alias] = nil
		return
	}
	if _, served := coverage.SourceOf(coordinate); !served {
		state.errors = append(state.errors, errorAt(here, domain.RefuseUnserved(coordinate, tier)))
		into[field.Alias] = nil
		return
	}

	value, err := state.bound.resolve(ctx, coordinate, source, field.ArgumentMap(state.variables))
	if err != nil {
		state.errors = append(state.errors, errorAt(here, err.Error()))
		into[field.Alias] = nil
		return
	}
	// Recorded here, after the refusal gate and after resolution, because the
	// disclosure says these are "the coordinates this response actually answered
	// from" and that has to be true of every path that reaches it. Recorded
	// before the gate, a refused coordinate counted as one Conduit answered --
	// so a query selecting a served field and a refused one reported both, and
	// the extension overstated the compatibility claim it exists to bound. An
	// errored coordinate was counted the same way.
	//
	// A field returning an empty collection is still counted: it was reached and
	// it answered "nothing here", which is a fact Conduit produced. Its
	// subfields are never visited and so are never counted, which is the case
	// the note already describes.
	state.selected[coordinate] = tier

	switch resolved := value.(type) {
	case child:
		into[field.Alias] = state.selectionSet(ctx, resolved.typeName, resolved.source, field.SelectionSet, here)
	case []child:
		items := make([]any, 0, len(resolved))
		for index, item := range resolved {
			at := append(append(ast.Path{}, here...), ast.PathIndex(index))
			items = append(items, state.selectionSet(ctx, item.typeName, item.source, field.SelectionSet, at))
		}
		into[field.Alias] = items
	default:
		into[field.Alias] = value
	}
}

func errorAt(path ast.Path, message string) *gqlerror.Error {
	err := gqlerror.Errorf("%s", message)
	err.Path = path
	return err
}

// provenanceExtension states, per response, which of the coordinates this
// response was actually answered from no Apollo Hub established. A client that
// reads it learns the same thing the repository's gate enforces, without having
// to read the repository.
//
// The two lower tiers are disclosed separately rather than lumped together,
// because they are different admissions. A DOCUMENTED coordinate carries
// Apollo's own published name and Conduit can say where it got it; a
// CONDUIT_NATIVE one is Conduit's idea and nothing more. Collapsing them would
// overstate the invention, which is the error this disclosure was built to
// avoid in the other direction.
//
// It reports what was served rather than what was selected, and the note says
// so. Reporting the selection would name coordinates under an empty collection
// that no data ever came from, which would make the disclosure a statement
// about the query rather than about the answer.
func (state *walk) provenanceExtension() map[string]any {
	documented, native, minted := []string{}, []string{}, []string{}
	observed := 0
	for coordinate, tier := range state.selected {
		// A recovered coordinate can still be answered from a value Conduit
		// built. Until ADR 0007 this disclosure had no way to say that, so
		// ApolloEnvironment.rid counted as OBSERVED under a note promising the
		// value came from an Apollo Hub -- true of the coordinate, false of the
		// string. It is still counted as observed, because the coordinate really
		// was recovered and moving it would understate the recovery; it is listed
		// here as well, and the note says the list is a subset rather than a
		// fourth bucket.
		if valueTier, stated := state.provenance.ValueTierOf(coordinate); stated && valueTier == domain.ConduitNative {
			minted = append(minted, coordinate.String())
		}
		switch tier {
		case domain.Observed:
			observed++
		case domain.Documented:
			documented = append(documented, coordinate.String())
		default:
			native = append(native, coordinate.String())
		}
	}
	sort.Strings(documented)
	sort.Strings(native)
	sort.Strings(minted)
	return map[string]any{
		"provenance": map[string]any{
			"note": "OBSERVED coordinates were recovered from an authorized Apollo Hub, which is a claim " +
				"about the coordinate and not about the value behind it: conduitMintedValues names the " +
				"OBSERVED coordinates whose VALUE Conduit constructed, and is a subset of observedServed " +
				"rather than a separate bucket. Do not round-trip one of those values into an Apollo API. " +
				"DOCUMENTED " +
				"coordinates carry a name Apollo publishes or a concept Apollo documents, but no Hub " +
				"established them in GraphQL. CONDUIT_NATIVE coordinates were invented by Conduit and carry " +
				"no Apollo claim at all. Neither lower tier is an Apollo compatibility claim. Conduit refuses " +
				"a declared coordinate it cannot answer rather than returning null, so a null here is a value " +
				"and not an absence of one. These lists name the coordinates this response actually answered " +
				"from, not every coordinate the document selected: a field under an empty collection was " +
				"never reached and so contributed nothing.",
			"observedServed":      observed,
			"conduitMintedValues": minted,
			"documentedServed":    documented,
			"conduitNativeServed": native,
			"schema":              schema.Name,
		},
	}
}

// --- resolution -------------------------------------------------------------

var errNoSource = fmt.Errorf("no source is configured for this half of the interface")

// resolve answers one covered coordinate. The switch is exhaustive over the
// coverage table by construction: a covered coordinate with no case here fails
// with a plain statement rather than a nil, and a test drives every entry.
func (resolver *bound) resolve(
	ctx context.Context, coordinate domain.Coordinate, source any, arguments map[string]any,
) (any, error) {
	switch coordinate.Type {
	case "ConduitCurrentUser":
		user, ok := source.(ports.CurrentUser)
		if !ok {
			return nil, errNoSource
		}
		if coordinate.Field == "id" {
			return user.ID, nil
		}
		if coordinate.Field == "fullName" {
			if user.FullName == nil {
				return nil, nil
			}
			return *user.FullName, nil
		}
		return nil, errNoSource
	case "Query":
		if coordinate.Field == "me" {
			if resolver.sources.CurrentUser == nil {
				return nil, errNoSource
			}
			return child{typeName: "ConduitCurrentUser", source: *resolver.sources.CurrentUser}, nil
		}
		// The entry point. It holds no data of its own; everything below it does.
		return child{typeName: "ApolloQuery", source: rootSource{}}, nil
	case "ApolloQuery":
		return resolver.resolveQuery(ctx, coordinate.Field, arguments)
	case "ApolloBundle":
		return resolver.resolveBundle(ctx, coordinate.Field, source)
	case "ApolloEnvironment":
		return resolver.resolveEnvironment(ctx, coordinate.Field, source, arguments)
	case "ApolloEntity":
		return resolver.resolveEntity(ctx, coordinate.Field, source)
	case "ApolloProduct":
		return resolver.resolveProduct(ctx, coordinate.Field, source)
	case "ApolloProductRelease":
		return resolver.resolveProductRelease(ctx, coordinate.Field, source)
	case "ApolloProductReleaseManifest":
		return resolveManifest(coordinate.Field, source)
	case "ApolloProductDependency":
		return resolveDependency(coordinate.Field, source)
	case "ApolloPlan", "ApolloChangeRequest", "ApolloPromotion":
		return resolveRecord(coordinate, source)
	case "ApolloEnvironmentPage", "ApolloEntityPage", "ApolloProductPage", "ApolloProductReleasePage",
		"ApolloPlanPage", "ApolloChangeRequestPage", "ApolloPromotionPage":
		return resolvePage(coordinate.Field, source)
	}
	return nil, fmt.Errorf("%s is covered but this resolver has no case for it", coordinate)
}

// stringArgument reads a declared String/RID argument, and refuses rather than
// substituting "" when the value is not one.
//
// Discarding the type assertion was the bug: `environment(rid: 5)` -- as a
// literal and as a variable -- passes gqlparser's custom-scalar validation,
// coerced to "", and looked up an environment whose id is the empty string.
// The client received `environment: null` with no error, so a malformed query
// was indistinguishable from a missing environment. Two different unknowns,
// one answer, which is exactly what this interface exists not to do.
func stringArgument(arguments map[string]any, name string) (string, error) {
	raw, present := arguments[name]
	if !present {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("argument %s must be a string, got %T", name, raw)
	}
	return value, nil
}

func (resolver *bound) resolveQuery(ctx context.Context, field string, arguments map[string]any) (any, error) {
	switch field {
	case "bundle", "bundles":
		return resolver.queryBundle(ctx, field, arguments)
	case "environment":
		if resolver.sources.Apollo == nil {
			return nil, errNoSource
		}
		rid, argumentErr := stringArgument(arguments, "rid")
		if argumentErr != nil {
			return nil, argumentErr
		}
		environment, err := resolver.sources.Apollo.GetEnvironment(rid)
		// Only the port's own not-found sentinel becomes a null. Any other
		// error is a failure to answer and is propagated, the way `plans`
		// below already does. Collapsing them made a store that is down
		// indistinguishable from an environment that does not exist -- the
		// client saw `"environment": null` and zero errors.
		if errors.Is(err, ports.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return child{typeName: "ApolloEnvironment", source: environment}, nil
	case "environments":
		if resolver.sources.Apollo == nil {
			return nil, errNoSource
		}
		listed, err := resolver.sources.Apollo.ListEnvironments()
		if err != nil {
			return nil, err
		}
		return child{typeName: "ApolloEnvironmentPage", source: page{records: anySlice(listed.Environments)}}, nil
	case "entity":
		rid, argumentErr := stringArgument(arguments, "rid")
		if argumentErr != nil {
			return nil, argumentErr
		}
		return resolver.entityByID(rid)
	case "productRelease":
		if resolver.sources.Apollo == nil {
			return nil, errNoSource
		}
		rid, argumentErr := stringArgument(arguments, "rid")
		if argumentErr != nil {
			return nil, argumentErr
		}
		release, err := resolver.productReleaseByRID(rid)
		// Same rule as `environment` above: only the not-found sentinel becomes
		// a null. Here it also covers the identifier that was never Conduit's,
		// which is a genuine "no such record HERE" rather than a failure -- and
		// deliberately not an assertion that no such release exists anywhere.
		if errors.Is(err, ports.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return release, nil
	case "products":
		if resolver.sources.Apollo == nil {
			return nil, errNoSource
		}
		listed, err := resolver.sources.Apollo.ListProducts()
		if err != nil {
			return nil, err
		}
		return child{typeName: "ApolloProductPage", source: page{records: anySlice(listed.Products)}}, nil
	case "plans":
		plans, err := resolver.plans(ctx)
		if err != nil {
			return nil, err
		}
		return child{typeName: "ApolloPlanPage", source: page{records: plans}}, nil
	case "plan":
		plans, err := resolver.plans(ctx)
		if err != nil {
			return nil, err
		}
		rid, argumentErr := stringArgument(arguments, "rid")
		if argumentErr != nil {
			return nil, argumentErr
		}
		for _, plan := range plans {
			if plan.(reporting.PlanSummary).ID == rid {
				return child{typeName: "ApolloPlan", source: plan}, nil
			}
		}
		return nil, nil
	case "changeRequests":
		if resolver.sources.Overview == nil {
			return nil, errNoSource
		}
		summaries, err := resolver.sources.Overview.VisibleChangeRequests(ctx)
		if err != nil {
			return nil, err
		}
		return child{typeName: "ApolloChangeRequestPage", source: page{records: boxed(summaries)}}, nil
	case "promotions":
		if resolver.sources.Overview == nil {
			return nil, errNoSource
		}
		summaries, err := resolver.sources.Overview.VisiblePromotions(ctx)
		if err != nil {
			return nil, err
		}
		return child{typeName: "ApolloPromotionPage", source: page{records: boxed(summaries)}}, nil
	}
	return nil, fmt.Errorf("ApolloQuery.%s is covered but this resolver has no case for it", field)
}

func boxed[T any](values []T) []any {
	records := make([]any, 0, len(values))
	for _, value := range values {
		records = append(records, value)
	}
	return records
}

func (resolver *bound) plans(ctx context.Context) ([]any, error) {
	if resolver.sources.Overview == nil {
		return nil, errNoSource
	}
	summaries, err := resolver.sources.Overview.VisiblePlans(ctx)
	if err != nil {
		return nil, err
	}
	return boxed(summaries), nil
}

// page is a page's own state. It carries the records for a Conduit-native
// collection and nothing for a recovered one, because the recovered pages'
// collection fields are refused: validation probing never established whether
// they are lists, and a page that guessed would be inventing cardinality.
type page struct {
	records []any
}

func resolvePage(field string, source any) (any, error) {
	held, ok := source.(page)
	if !ok {
		return nil, fmt.Errorf("page field %q resolved against %T", field, source)
	}
	if field == "nextPageToken" {
		// Null, and it is a value rather than a refusal: there genuinely is no
		// next page, because there is only ever one.
		//
		// The comment here used to read "every visible record fits one page
		// today", which described a capacity Conduit does not enforce. `pageSize`
		// is a required argument on every collection field and NO resolver reads
		// it -- `environments(pageSize: 1)` against five returns five (issue
		// #112, finding 5). So a page is always the whole collection and the
		// token is always null; the two statements are consistent, and the
		// second is the one a client needs.
		//
		// Honouring pageSize is NOT the fix it looks like. Slicing the records is
		// trivial; the token is not. Emitting one means minting a cursor grammar,
		// and no capture establishes what an Apollo page token looks like -- so a
		// client would receive a Conduit-invented string under a coordinate
		// recovered from a Hub, which is precisely the defect issues #57 and #112
		// are about. Truncating without a token is worse still: the client is
		// silently told the collection ended.
		//
		// So this is DECLARED rather than fixed, in
		// docs/operations/TENANT-FIDELITY.md, and the coverage table above says
		// the same thing in the entry a client can read.
		return nil, nil
	}
	items := make([]child, 0, len(held.records))
	for _, record := range held.records {
		items = append(items, child{typeName: recordTypeOf(record), source: record})
	}
	return items, nil
}

// anySlice flattens the generated pointer-to-slice the Apollo transport returns.
// A nil pointer and an empty slice both mean "no records", and both must produce
// an empty list rather than a null: the collection was answered, and it was
// empty.
func anySlice[T any](values *[]T) []any {
	if values == nil {
		return []any{}
	}
	records := make([]any, 0, len(*values))
	for _, value := range *values {
		records = append(records, value)
	}
	return records
}

func recordTypeOf(record any) string {
	switch record.(type) {
	case apollo.Environment:
		return "ApolloEnvironment"
	case apollo.Product:
		return "ApolloProduct"
	case entitySource:
		return "ApolloEntity"
	case releaseSource:
		return "ApolloProductRelease"
	case apollo.ProductDependency:
		return "ApolloProductDependency"
	case reporting.PlanSummary:
		return "ApolloPlan"
	case reporting.ChangeRequestSummary:
		return "ApolloChangeRequest"
	case reporting.PromotionSummary:
		return "ApolloPromotion"
	}
	return ""
}

func resolveRecord(coordinate domain.Coordinate, source any) (any, error) {
	switch record := source.(type) {
	case reporting.PlanSummary:
		switch coordinate.Field {
		case "rid":
			return record.ID, nil
		case "environmentRid":
			return record.Environment, nil
		case "status":
			return string(record.Phase), nil
		}
	case reporting.ChangeRequestSummary:
		switch coordinate.Field {
		case "rid":
			return record.ID, nil
		case "title":
			return record.Title, nil
		case "status":
			// Governance judged this, and Reporting carries the judgement as a
			// boolean. Apollo publishes the field name -- `apollo-cli
			// changerequest list --status` -- but not its vocabulary, so the two
			// strings below are Conduit's own and are not claimed otherwise.
			if record.AwaitingApproval {
				return "AWAITING_APPROVAL", nil
			}
			return "NOT_AWAITING_APPROVAL", nil
		}
	case reporting.PromotionSummary:
		switch coordinate.Field {
		case "rid":
			return record.ID, nil
		case "productId":
			return record.Product, nil
		case "status":
			if record.Blocked {
				return "BLOCKED", nil
			}
			return "NOT_BLOCKED", nil
		}
	}
	return nil, fmt.Errorf("%s resolved against %T", coordinate, source)
}

func (resolver *bound) resolveEnvironment(
	ctx context.Context, field string, source any, _ map[string]any,
) (any, error) {
	environment, ok := source.(apollo.Environment)
	if !ok {
		return nil, fmt.Errorf("ApolloEnvironment.%s resolved against %T", field, source)
	}
	switch field {
	case "spaceId":
		return resolver.spaceID(ctx, "ENVIRONMENT", string(environment.Name))
	case "name", "id":
		// One value answers both, because Conduit holds one human identifier for
		// an Environment and Apollo's own contract accepts that value under both
		// spellings: `Environment.name` is documented as "a unique resource
		// identifier of an Environment, entered at Environment creation time",
		// and the two status routes take it as `{environmentId}` where every
		// other route takes `{environmentRid}`. Conduit already keys on it --
		// PutEntityHealth resolves reported state by exactly this string.
		//
		// This says the two coincide HERE. It is not a claim that Apollo's `id`
		// and `name` are one field: Conduit has one identifier to give, and
		// giving it under both names is more honest than refusing `id` while
		// holding the value the contract calls by that name.
		return string(environment.Name), nil
	case "rid":
		return string(environment.Rid), nil
	case "entities":
		if resolver.sources.Apollo == nil {
			return nil, errNoSource
		}
		installations, err := resolver.sources.Apollo.ListInstallations(string(environment.Rid))
		if err != nil {
			return nil, err
		}
		entities := []any{}
		if installations.Installations != nil {
			for _, installation := range *installations.Installations {
				// The Environment travels with each Entity because an Entity
				// resolves environment-scoped fields, and rediscovering the
				// Environment from the installation would mean a second lookup
				// that could disagree with this one.
				entities = append(entities, entitySource{installation: installation, environment: environment})
			}
		}
		return child{typeName: "ApolloEntityPage", source: page{records: entities}}, nil
	case "health":
		// Keyed on the name, not the rid. Reporting records an installation
		// against the Environment's plain identifier -- the same string
		// PutEntityHealth resolves reported state by, as the id/name case above
		// says -- while Rid is a digest Conduit mints for the Apollo surface.
		// Comparing the two never matched, so this field answered UNKNOWN for
		// every Environment however healthy the fleet was, and said in the
		// provenance extension that it had answered.
		return resolver.environmentHealth(ctx, string(environment.Name))
	}
	return nil, fmt.Errorf("ApolloEnvironment.%s is covered but this resolver has no case for it", field)
}

// environmentHealth is Conduit's own aggregate, and the vocabulary is stated
// rather than invented per call: an environment reads UNHEALTHY if anything in
// it does, UNKNOWN if the fleet has nothing to say, and HEALTHY otherwise. A
// stale report counts as unknown rather than healthy, because an old "healthy"
// is not evidence of a current one.
func (resolver *bound) environmentHealth(ctx context.Context, environment string) (any, error) {
	if resolver.sources.Overview == nil {
		return nil, errNoSource
	}
	installations, err := resolver.sources.Overview.VisibleInstallations(ctx, staleAfter, resolver.now())
	if err != nil {
		return nil, err
	}
	worst := ""
	for _, installation := range installations {
		if installation.Environment != environment {
			continue
		}
		health := installation.Health
		if installation.Stale {
			health = reporting.HealthUnknown
		}
		if health == reporting.HealthUnhealthy {
			return reporting.HealthUnhealthy, nil
		}
		if health == reporting.HealthUnknown || worst == "" {
			worst = health
		}
	}
	if worst == "" {
		return reporting.HealthUnknown, nil
	}
	return worst, nil
}

// entitySource is one installation together with the environment it belongs to,
// so ApolloEntity.environment can be answered without a second lookup that
// might see a different moment.
type entitySource struct {
	installation apollo.Installation
	environment  apollo.Environment
}

func (resolver *bound) entityByID(id string) (any, error) {
	if resolver.sources.Apollo == nil {
		return nil, errNoSource
	}
	listed, err := resolver.sources.Apollo.ListEnvironments()
	if err != nil {
		return nil, err
	}
	if listed.Environments == nil {
		return nil, nil
	}
	for _, environment := range *listed.Environments {
		installations, err := resolver.sources.Apollo.ListInstallations(string(environment.Rid))
		if err != nil {
			return nil, err
		}
		if installations.Installations == nil {
			continue
		}
		for _, installation := range *installations.Installations {
			if string(installation.ApolloEntityId) != id {
				continue
			}
			return child{
				typeName: "ApolloEntity",
				source:   entitySource{installation: installation, environment: environment},
			}, nil
		}
	}
	return nil, nil
}

func (resolver *bound) resolveEntity(ctx context.Context, field string, source any) (any, error) {
	entity, ok := source.(entitySource)
	if !ok {
		return nil, fmt.Errorf("ApolloEntity.%s resolved against %T", field, source)
	}
	switch field {
	case "spaceId":
		return resolver.spaceID(ctx, "ENVIRONMENT", string(entity.environment.Name))
	case "id":
		return string(entity.installation.ApolloEntityId), nil
	case "packageRid":
		return string(entity.installation.PackageRid), nil
	case "environment":
		return child{typeName: "ApolloEnvironment", source: entity.environment}, nil
	case "version":
		if entity.installation.Version == nil {
			// The Agent has not reported one. That is an absence Conduit has
			// actually observed, so it is a null rather than a refusal.
			return nil, nil
		}
		return *entity.installation.Version, nil
	case "health":
		if resolver.sources.Overview == nil {
			return nil, errNoSource
		}
		installations, err := resolver.sources.Overview.VisibleInstallations(ctx, staleAfter, resolver.now())
		if err != nil {
			return nil, err
		}
		// The same mismatch as ApolloEnvironment.health, one level down:
		// reporting keys an installation by the Entity's plain name while
		// installation.Rid is a minted digest, so this compared two vocabularies
		// and answered UNKNOWN for everything.
		name := apollo.HelmChartEntityName(string(entity.installation.ApolloEntityId))
		for _, installation := range installations {
			if name == "" || installation.InstallationID != name {
				continue
			}
			if installation.Stale {
				return reporting.HealthUnknown, nil
			}
			return installation.Health, nil
		}
		return reporting.HealthUnknown, nil
	}
	return nil, fmt.Errorf("ApolloEntity.%s is covered but this resolver has no case for it", field)
}

func (resolver *bound) resolveProduct(ctx context.Context, field string, source any) (any, error) {
	product, ok := source.(apollo.Product)
	if !ok {
		return nil, fmt.Errorf("ApolloProduct.%s resolved against %T", field, source)
	}
	switch field {
	case "spaceId":
		return resolver.spaceID(ctx, "PRODUCT", string(product.Id))
	case "rid":
		return productRID(string(product.Id)), nil
	case "productId":
		return string(product.Id), nil
	case "productReleases":
		if resolver.sources.Apollo == nil {
			return nil, errNoSource
		}
		listed, err := resolver.sources.Apollo.ListReleases(string(product.Id))
		if err != nil {
			return nil, err
		}
		releases := []any{}
		if listed.Releases != nil {
			for _, release := range *listed.Releases {
				releases = append(releases, releaseSource{product: product, release: release})
			}
		}
		return child{typeName: "ApolloProductReleasePage", source: page{records: releases}}, nil
	}
	return nil, fmt.Errorf("ApolloProduct.%s is covered but this resolver has no case for it", field)
}

// releaseSource keeps the owning product beside the release so
// ApolloProductRelease.product answers from the same read.
type releaseSource struct {
	product apollo.Product
	release apollo.Release
}

func (resolver *bound) resolveProductRelease(ctx context.Context, field string, source any) (any, error) {
	held, ok := source.(releaseSource)
	if !ok {
		return nil, fmt.Errorf("ApolloProductRelease.%s resolved against %T", field, source)
	}
	switch field {
	case "spaceId":
		return resolver.spaceID(ctx, "PRODUCT", string(held.product.Id))
	case "rid":
		return productReleaseRID(string(held.release.MavenCoordinate)), nil
	case "mavenCoordinate":
		return string(held.release.MavenCoordinate), nil
	case "product":
		return child{typeName: "ApolloProduct", source: held.product}, nil
	case "manifest":
		return child{typeName: "ApolloProductReleaseManifest", source: held.release}, nil
	}
	return nil, fmt.Errorf("ApolloProductRelease.%s is covered but this resolver has no case for it", field)
}

func resolveManifest(field string, source any) (any, error) {
	release, ok := source.(apollo.Release)
	if !ok {
		return nil, fmt.Errorf("ApolloProductReleaseManifest.%s resolved against %T", field, source)
	}
	switch field {
	case "upgradeStrategy":
		if release.UpgradeStrategy == nil {
			return nil, nil
		}
		return string(*release.UpgradeStrategy), nil
	case "productDependencies":
		// A Release with no declared dependencies answers with an empty list
		// rather than a null. "This Release depends on nothing" is a fact the
		// manifest states; a null would say the question was not answered.
		items := make([]child, 0)
		for _, dependency := range anySlice(release.ProductDependencies) {
			items = append(items, child{typeName: "ApolloProductDependency", source: dependency})
		}
		return items, nil
	}
	return nil, fmt.Errorf("ApolloProductReleaseManifest.%s is covered but this resolver has no case for it", field)
}

func resolveDependency(field string, source any) (any, error) {
	dependency, ok := source.(apollo.ProductDependency)
	if !ok {
		return nil, fmt.Errorf("ApolloProductDependency.%s resolved against %T", field, source)
	}
	switch field {
	case "optional":
		// The recovered field is a String, not a Boolean: the Hub was never
		// asked for its scalar type. Conduit renders its own boolean rather
		// than claiming the Hub's shape.
		//
		// CONDUIT-INVENTED: apollographql.DependencyOptionalDefault
		// Displaced-by: a published Apollo publish request model carrying
		// dependency optionality, which would make the served value the
		// publisher's rather than Conduit's.
		//
		// This used to answer under a bare OBSERVED annotation, which told a
		// client an Apollo Hub had said "false" (issues #57, #112). Neither
		// pinned publish request carries an optionality input, so for a publicly
		// published dependency nobody said anything and this default is Conduit's
		// own. The coordinate now discloses valueTier: CONDUIT_NATIVE, which is
		// the only thing a GraphQL client ever sees, and the annotation covers
		// the rendering as well as the default: "true"/"false" are Conduit's
		// spelling of a bool into a String whose real scalar type is unknown.
		//
		// The disclosure is field-wide rather than per-value because a valueTier
		// is a property of the coordinate. That over-discloses the case where
		// Conduit's own manifest route DID declare optionality -- and that is the
		// safe direction. Under-disclosure is the defect these issues reported.
		if dependency.Optional != nil && *dependency.Optional {
			return "true", nil
		}
		return "false", nil
	case "product":
		if dependency.ProductId == nil {
			return nil, nil
		}
		return child{typeName: "ApolloProduct", source: apollo.Product{Id: *dependency.ProductId}}, nil
	}
	return nil, fmt.Errorf("ApolloProductDependency.%s is covered but this resolver has no case for it", field)
}

// productReleaseByRID finds the Release a Conduit-minted identifier names.
//
// A scan rather than an index, because the identifier is a digest and cannot be
// reversed: the only way to answer is to mint each candidate and compare. The
// catalogue is the same one ListProducts already walks for every products query,
// so this costs what that costs.
//
// An identifier that is not Conduit's is refused before the scan rather than
// after it. Scanning first would spend the catalogue to reach the same answer,
// and would report "no record" for an Apollo identifier that may well name a
// real Release on a real Hub -- an absence Conduit is in no position to assert.
func (resolver *bound) productReleaseByRID(rid string) (any, error) {
	if !isConduitRID(rid) {
		return nil, ports.ErrRecordNotFound
	}
	listedProducts, err := resolver.sources.Apollo.ListProducts()
	if err != nil {
		return nil, err
	}
	if listedProducts.Products == nil {
		return nil, ports.ErrRecordNotFound
	}
	for _, product := range *listedProducts.Products {
		listed, err := resolver.sources.Apollo.ListReleases(string(product.Id))
		if err != nil {
			return nil, err
		}
		if listed.Releases == nil {
			continue
		}
		for _, release := range *listed.Releases {
			if productReleaseRID(string(release.MavenCoordinate)) == rid {
				return child{typeName: "ApolloProductRelease", source: releaseSource{product: product, release: release}}, nil
			}
		}
	}
	return nil, ports.ErrRecordNotFound
}
