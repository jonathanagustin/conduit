// Package httpgraphql serves the Apollo-shaped interface over HTTP.
//
// It is deliberately thin. Everything that could be got wrong -- what the
// contract is, which coordinates Conduit may answer, what a refusal says -- is
// settled before a request arrives, so this decodes, delegates and encodes.
//
// The one judgement it makes is the status code, and it makes the unusual
// choice on purpose: a response carrying field errors is still 200. GraphQL
// puts partial failure in the body, and this interface produces partial
// failures by design, because every declared-but-unanswerable coordinate
// refuses. Returning 500 for those would tell a client the server broke when
// what actually happened is that it declined to invent an answer.
package httpgraphql

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/jonathanagustin/conduit/internal/apollographql/application"
	"github.com/jonathanagustin/conduit/internal/apollographql/ports"
	"github.com/jonathanagustin/conduit/internal/apollographql/schema"
)

// maximumBody bounds one request. GraphQL documents are small, and an unbounded
// read on an authenticated endpoint is a way to spend memory rather than a way
// to ask a question.
const maximumBody = 256 << 10

func errorList(message string) gqlerror.List {
	return gqlerror.List{gqlerror.Errorf("%s", message)}
}

// Handler serves POST for queries and GET for the schema document itself.
//
// Sources are supplied per request rather than held, because what a reader may
// see is decided for that reader. The control plane builds them from the
// authenticated principal; this handler never widens them.
type Handler struct {
	resolver   *application.Resolver
	sourcesFor func(http.ResponseWriter, *http.Request) (ports.Sources, bool)
}

// New takes the resolver and the function that binds one request's sources. A
// sourcesFor that returns false has already written its own response -- an
// unauthenticated caller gets Governance's answer, not a GraphQL error that
// would leak the shape of the question.
func New(resolver *application.Resolver, sourcesFor func(http.ResponseWriter, *http.Request) (ports.Sources, bool)) *Handler {
	return &Handler{resolver: resolver, sourcesFor: sourcesFor}
}

// Schema serves the served interface as SDL, provenance annotations included.
//
// Publishing it is the point. A client that fetches this can see, before
// writing a query, which coordinates an Apollo Hub established and which
// Conduit invented -- which is the disclosure the whole design turns on.
func (handler *Handler) Schema(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/graphql; charset=utf-8")
	writer.Header().Set("X-Conduit-Graphql-Schema", schema.Name)
	_, _ = io.WriteString(writer, schema.SDL())
}

// Query answers one GraphQL request.
func (handler *Handler) Query(writer http.ResponseWriter, request *http.Request) {
	var decoded application.Request
	if err := json.NewDecoder(io.LimitReader(request.Body, maximumBody)).Decode(&decoded); err != nil {
		writeJSON(writer, http.StatusBadRequest, application.Response{
			Errors: errorList("this endpoint takes a GraphQL over HTTP JSON body: " + err.Error()),
		})
		return
	}
	if decoded.Query == "" {
		writeJSON(writer, http.StatusBadRequest, application.Response{Errors: errorList("no query was sent")})
		return
	}
	sources, ok := handler.sourcesFor(writer, request)
	if !ok {
		return
	}
	// Field errors ride in the body with a 200. See the package comment: a
	// refusal is an answer this interface gives on purpose, not a fault.
	writeJSON(writer, http.StatusOK, handler.resolver.Execute(request.Context(), sources, decoded))
}

func writeJSON(writer http.ResponseWriter, status int, response application.Response) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(response)
}
