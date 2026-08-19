package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/apollo"
	"github.com/jonathanagustin/conduit/internal/apollo/application"
)

// apolloOpenAPIAdapter is the generated public transport boundary. Its method
// set is checked against the pinned OpenAPI at compile time; the legacy
// control-plane facade remains behind this adapter while the Apollo context is
// extracted into neutral application ports.
type apolloOpenAPIAdapter struct {
	legacy *apolloPublicAPI
}

var _ apollo.StrictServerInterface = (*apolloOpenAPIAdapter)(nil)

type apolloHTTPRequestKey struct{}
type apolloHTTPBodyKey struct{}

func retainApolloHTTPRequest(next apollo.StrictHandlerFunc, _ string) apollo.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, request *http.Request, input any) (any, error) {
		return next(context.WithValue(ctx, apolloHTTPRequestKey{}, request), w, request, input)
	}
}

func requireApolloIdentity(legacy *apolloPublicAPI) apollo.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodPost && request.PathValue("environmentId") != "" {
				if !legacy.requireAgent(w, request) {
					return
				}
			} else {
				// Captured rather than written straight through. This
				// middleware refuses before the strict handler exists, so its
				// 401 never reached the adapter's conversion funnel -- which is
				// why every unauthenticated Apollo request served a body no
				// generated client could read.
				capture := newApolloResponseCapture()
				if _, ok := legacy.resolveOperator(capture, request); !ok {
					status, body := apolloAuthorizationRefusal(capture)
					writeJSON(w, status, body)
					return
				}
			}
			next.ServeHTTP(w, request)
		})
	}
}

func retainApolloRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			next.ServeHTTP(w, request)
			return
		}
		const maximum = int64(1 << 20)
		contents, err := io.ReadAll(io.LimitReader(request.Body, maximum+1))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
			return
		}
		if int64(len(contents)) > maximum {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request exceeds payload limit"})
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(contents))
		request.ContentLength = int64(len(contents))
		request = request.WithContext(context.WithValue(request.Context(), apolloHTTPBodyKey{}, bytes.Clone(contents)))
		next.ServeHTTP(w, request)
	})
}

type apolloHTTPResponseError struct {
	status int
	header http.Header
	body   []byte
}

func (err *apolloHTTPResponseError) Error() string {
	return fmt.Sprintf("Apollo public adapter returned HTTP %d", err.status)
}

type apolloResponseCapture struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newApolloResponseCapture() *apolloResponseCapture {
	return &apolloResponseCapture{header: make(http.Header)}
}

func (capture *apolloResponseCapture) Header() http.Header { return capture.header }

func (capture *apolloResponseCapture) WriteHeader(status int) {
	if capture.status == 0 {
		capture.status = status
	}
}

func (capture *apolloResponseCapture) Write(contents []byte) (int, error) {
	if capture.status == 0 {
		capture.status = http.StatusOK
	}
	return capture.body.Write(contents)
}

func writeApolloResponseError(w http.ResponseWriter, _ *http.Request, err error) {
	var response *apolloHTTPResponseError
	if !errors.As(err, &response) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for name, values := range response.header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(response.status)
	_, _ = w.Write(response.body)
}

func writeApolloRequestError(w http.ResponseWriter, _ *http.Request, err error) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
}

func apolloGeneratedRequestError(message string) error {
	capture := newApolloResponseCapture()
	writeJSON(capture, http.StatusBadRequest, map[string]string{"error": message})
	return &apolloHTTPResponseError{
		status: capture.status,
		header: capture.header.Clone(),
		body:   bytes.Clone(capture.body.Bytes()),
	}
}

func apolloStatusError(status int, message string) error {
	capture := newApolloResponseCapture()
	writeJSON(capture, status, map[string]string{"error": message})
	return &apolloHTTPResponseError{
		status: capture.status, header: capture.header.Clone(), body: bytes.Clone(capture.body.Bytes()),
	}
}

func apolloApplicationError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case application.IsKind(err, application.Invalid):
		return apolloStatusError(http.StatusUnprocessableEntity, err.Error())
	case application.IsKind(err, application.NotFound):
		return apolloStatusError(http.StatusNotFound, err.Error())
	case application.IsKind(err, application.Conflict):
		return apolloStatusError(http.StatusConflict, err.Error())
	default:
		return apolloStatusError(http.StatusInternalServerError, err.Error())
	}
}

func apolloRequestFromContext(ctx context.Context) (*http.Request, error) {
	request, ok := ctx.Value(apolloHTTPRequestKey{}).(*http.Request)
	if !ok || request == nil {
		return nil, errors.New("Apollo transport request is unavailable")
	}
	return request, nil
}

func (adapter *apolloOpenAPIAdapter) authorizeOperator(ctx context.Context, operation, resource string) error {
	request, err := apolloRequestFromContext(ctx)
	if err != nil {
		return err
	}
	capture := newApolloResponseCapture()
	principal, ok := adapter.legacy.resolveOperator(capture, request)
	if !ok {
		return capturedApolloAuthorizationError(capture)
	}
	if resource == "" {
		ok = requirePlatformOperation(capture, request, adapter.legacy.governance, principal, adapter.legacy.configuration, operation)
	} else {
		ok = requireResourceOperation(capture, request, adapter.legacy.governance, principal, adapter.legacy.configuration, resource, operation)
	}
	if !ok {
		return capturedApolloAuthorizationError(capture)
	}
	return nil
}

func (adapter *apolloOpenAPIAdapter) authorizePublication(ctx context.Context, coordinate string) error {
	request, err := apolloRequestFromContext(ctx)
	if err != nil {
		return err
	}
	capture := newApolloResponseCapture()
	principal, ok := adapter.legacy.resolveOperator(capture, request)
	if !ok {
		return capturedApolloAuthorizationError(capture)
	}
	group, name, _, valid := parseMavenCoordinate(coordinate)
	if !valid {
		return apolloStatusError(http.StatusUnprocessableEntity, "invalid Maven coordinate")
	}
	if !requireResourceOperation(capture, request, adapter.legacy.governance, principal, adapter.legacy.configuration, "global/artifact", OperationCreateArtifact) {
		return capturedApolloAuthorizationError(capture)
	}
	product := group + ":" + name
	if !adapter.legacy.application.HasProduct(product) &&
		!requireResourceOperation(capture, request, adapter.legacy.governance, principal, adapter.legacy.configuration, "global/product", OperationCreateProduct) {
		return capturedApolloAuthorizationError(capture)
	}
	if !requireResourceOperation(capture, request, adapter.legacy.governance, principal, adapter.legacy.configuration, productRID(product), OperationCreateRelease) {
		return capturedApolloAuthorizationError(capture)
	}
	return nil
}

// capturedApolloAuthorizationError converts a refusal written by the shared
// authorization helpers into the one envelope the contract publishes for it.
//
// ApiUsageDenied is described as "You have not properly authenticated with
// Apollo and will be unable to leverage the APIs", which covers the
// unauthenticated case as well as the unauthorised one, so both statuses the
// helpers produce map here. The HTTP status the helper chose is preserved
// rather than forced to 403: the contract declares no status codes at all --
// every path lists only "200" -- so it constrains the body, not the status, and
// rewriting a 401 into a 403 would lose the distinction for no contract gain.
//
// The helper's own message is carried through rather than replaced. It is the
// only description of what was actually refused, and the envelope has nowhere
// else to put it.
func capturedApolloAuthorizationError(capture *apolloResponseCapture) error {
	return apolloCapturedError(apolloAuthorizationRefusal(capture))
}

func validateApolloGeneratedBody[T any](ctx context.Context) error {
	contents, captured := ctx.Value(apolloHTTPBodyKey{}).([]byte)
	if !captured {
		return apolloGeneratedRequestError("invalid request: request body is unavailable")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		return apolloGeneratedRequestError("invalid request: " + err.Error())
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apolloGeneratedRequestError("request must contain exactly one JSON document")
	}
	return nil
}

func registerApolloOpenAPI(mux *http.ServeMux, legacy *apolloPublicAPI) {
	strict := apollo.NewStrictHandlerWithOptions(
		&apolloOpenAPIAdapter{legacy: legacy},
		[]apollo.StrictMiddlewareFunc{retainApolloHTTPRequest},
		apollo.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  writeApolloRequestError,
			ResponseErrorHandlerFunc: writeApolloResponseError,
		},
	)
	apollo.HandlerWithOptions(strict, apollo.StdHTTPServerOptions{
		BaseRouter: mux,
		Middlewares: []apollo.MiddlewareFunc{
			retainApolloRequestBody,
			requireApolloIdentity(legacy),
		},
		ErrorHandlerFunc: writeApolloRequestError,
	})
}

func (adapter *apolloOpenAPIAdapter) ListEnvironments(ctx context.Context, _ apollo.ListEnvironmentsRequestObject) (apollo.ListEnvironmentsResponseObject, error) {
	if err := adapter.authorizeOperator(ctx, OperationReadResource, ""); err != nil {
		return nil, err
	}
	// The read reaches the store now, so it can fail -- and a failure is
	// propagated rather than served as an empty list. An empty Apollo
	// environments page is indistinguishable from a Hub that governs nothing.
	response, err := adapter.legacy.application.ListEnvironments(ctx)
	if err != nil {
		return nil, apolloApplicationError(err)
	}
	return apollo.ListEnvironments200JSONResponse(response), nil
}

func (adapter *apolloOpenAPIAdapter) GetEnvironment(ctx context.Context, request apollo.GetEnvironmentRequestObject) (apollo.GetEnvironmentResponseObject, error) {
	if err := adapter.authorizeOperator(ctx, OperationReadResource, ""); err != nil {
		return nil, err
	}
	response, err := adapter.legacy.application.GetEnvironment(ctx, request.EnvironmentRid)
	return apollo.GetEnvironment200JSONResponse(response), apolloApplicationError(err)
}

func (adapter *apolloOpenAPIAdapter) ListInstallations(ctx context.Context, request apollo.ListInstallationsRequestObject) (apollo.ListInstallationsResponseObject, error) {
	if err := adapter.authorizeOperator(ctx, OperationReadResource, ""); err != nil {
		return nil, err
	}
	response, err := adapter.legacy.application.ListInstallations(ctx, request.EnvironmentRid)
	if err != nil {
		return nil, apolloApplicationError(err)
	}
	return apollo.ListInstallations200JSONResponse(response), nil
}

func (adapter *apolloOpenAPIAdapter) GetInstallation(ctx context.Context, request apollo.GetInstallationRequestObject) (apollo.GetInstallationResponseObject, error) {
	if err := adapter.authorizeOperator(ctx, OperationReadResource, ""); err != nil {
		return nil, err
	}
	response, err := adapter.legacy.application.GetInstallation(ctx, request.EnvironmentRid, request.InstallationRid)
	return apollo.GetInstallation200JSONResponse(response), apolloApplicationError(err)
}

func (adapter *apolloOpenAPIAdapter) ListNamespaces(ctx context.Context, _ apollo.ListNamespacesRequestObject) (apollo.ListNamespacesResponseObject, error) {
	if err := adapter.authorizeOperator(ctx, OperationReadResource, ""); err != nil {
		return nil, err
	}
	response, err := adapter.legacy.application.ListNamespaces(ctx)
	if err != nil {
		return nil, apolloApplicationError(err)
	}
	return apollo.ListNamespaces200JSONResponse(response), nil
}

func (adapter *apolloOpenAPIAdapter) ListProducts(ctx context.Context, _ apollo.ListProductsRequestObject) (apollo.ListProductsResponseObject, error) {
	if err := adapter.authorizeOperator(ctx, OperationReadResource, "global/product"); err != nil {
		return nil, err
	}
	return apollo.ListProducts200JSONResponse(adapter.legacy.application.ListProducts()), nil
}

func (adapter *apolloOpenAPIAdapter) PublishHelmChart(ctx context.Context, request apollo.PublishHelmChartRequestObject) (apollo.PublishHelmChartResponseObject, error) {
	if err := validateApolloGeneratedBody[apollo.PublishHelmChartRequest](ctx); err != nil {
		return nil, err
	}
	if err := adapter.authorizePublication(ctx, request.MavenCoordinate); err != nil {
		return nil, err
	}
	err := adapter.legacy.application.PublishHelmChart(ctx, request.MavenCoordinate, *request.Body)
	return apollo.PublishHelmChart200JSONResponse{}, apolloApplicationError(err)
}

func (adapter *apolloOpenAPIAdapter) PublishService(ctx context.Context, request apollo.PublishServiceRequestObject) (apollo.PublishServiceResponseObject, error) {
	if err := validateApolloGeneratedBody[apollo.PublishServiceRequest](ctx); err != nil {
		return nil, err
	}
	if err := adapter.authorizePublication(ctx, request.MavenCoordinate); err != nil {
		return nil, err
	}
	err := adapter.legacy.application.PublishService(ctx, request.MavenCoordinate, *request.Body)
	return apollo.PublishService200JSONResponse{}, apolloApplicationError(err)
}

func (adapter *apolloOpenAPIAdapter) GetProduct(ctx context.Context, request apollo.GetProductRequestObject) (apollo.GetProductResponseObject, error) {
	if err := adapter.authorizeOperator(ctx, OperationReadResource, productRID(request.ProductId)); err != nil {
		return nil, err
	}
	response, err := adapter.legacy.application.GetProduct(request.ProductId)
	return apollo.GetProduct200JSONResponse(response), apolloApplicationError(err)
}

func (adapter *apolloOpenAPIAdapter) ListReleases(ctx context.Context, request apollo.ListReleasesRequestObject) (apollo.ListReleasesResponseObject, error) {
	if err := adapter.authorizeOperator(ctx, OperationReadResource, productRID(request.ProductId)); err != nil {
		return nil, err
	}
	response, err := adapter.legacy.application.ListReleases(ctx, request.ProductId)
	if err != nil {
		return nil, err
	}
	return apollo.ListReleases200JSONResponse(response), nil
}

func (adapter *apolloOpenAPIAdapter) GetRelease(ctx context.Context, request apollo.GetReleaseRequestObject) (apollo.GetReleaseResponseObject, error) {
	if err := adapter.authorizeOperator(ctx, OperationReadResource, productRID(request.ProductId)); err != nil {
		return nil, err
	}
	response, err := adapter.legacy.application.GetRelease(ctx, request.ProductId, request.ProductVersion)
	if application.IsKind(err, application.NotFound) {
		// The one miss the contract names. ReleaseNotFound is "The specified
		// release could not be found within the Apollo Catalog", and its
		// parameters require the mavenCoordinate -- which this call already
		// holds as ProductId and ProductVersion, so the envelope can say which
		// release was missing rather than only that one was.
		return nil, apolloConjureError(http.StatusNotFound,
			apollo.ErrorCodeNotFound, apollo.ErrorNameReleaseNotFound, err.Error(),
			map[string]string{"mavenCoordinate": request.ProductId + ":" + request.ProductVersion})
	}
	return apollo.GetRelease200JSONResponse(response), apolloApplicationError(err)
}

func (adapter *apolloOpenAPIAdapter) PutStatusEntitiesHealth(ctx context.Context, request apollo.PutStatusEntitiesHealthRequestObject) (apollo.PutStatusEntitiesHealthResponseObject, error) {
	if err := validateApolloGeneratedBody[apollo.PutStatusEntitiesHealthRequest](ctx); err != nil {
		return nil, err
	}
	err := adapter.legacy.application.PutEntityHealth(ctx, request.EnvironmentId, dereferenceApollo(request.Body.EntitiesHealth))
	return apollo.PutStatusEntitiesHealth200JSONResponse{}, apolloApplicationError(err)
}

func (adapter *apolloOpenAPIAdapter) PutStatusEntitiesInfo(ctx context.Context, request apollo.PutStatusEntitiesInfoRequestObject) (apollo.PutStatusEntitiesInfoResponseObject, error) {
	if err := validateApolloGeneratedBody[apollo.PutStatusEntitiesInfoRequest](ctx); err != nil {
		return nil, err
	}
	err := adapter.legacy.application.PutEntityInfo(ctx, request.EnvironmentId, dereferenceApollo(request.Body.EntitiesInfo))
	return apollo.PutStatusEntitiesInfo200JSONResponse{}, apolloApplicationError(err)
}

func dereferenceApollo[T any](values *[]T) []T {
	if values == nil {
		return nil
	}
	return *values
}
