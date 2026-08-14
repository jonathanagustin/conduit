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
			} else if _, ok := legacy.resolveOperator(w, request); !ok {
				return
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

func invokeApolloFacade[T any](
	ctx context.Context,
	legacy *apolloPublicAPI,
	handler http.HandlerFunc,
	pathValues map[string]string,
	body any,
) (T, error) {
	var zero T
	original, ok := ctx.Value(apolloHTTPRequestKey{}).(*http.Request)
	if !ok || original == nil {
		return zero, errors.New("Apollo transport request is unavailable")
	}
	request := original.Clone(ctx)
	for name, value := range pathValues {
		request.SetPathValue(name, value)
	}
	if body == nil {
		request.Body = http.NoBody
		request.ContentLength = 0
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			return zero, fmt.Errorf("encode generated Apollo request: %w", err)
		}
		request.Body = io.NopCloser(bytes.NewReader(encoded))
		request.ContentLength = int64(len(encoded))
	}
	capture := newApolloResponseCapture()
	if legacy.initializationError != nil {
		writeLifecycleError(capture, legacy.initializationError)
	} else {
		handler(capture, request)
	}
	if capture.status == 0 {
		capture.status = http.StatusOK
	}
	if capture.status < http.StatusOK || capture.status >= http.StatusMultipleChoices {
		return zero, &apolloHTTPResponseError{
			status: capture.status,
			header: capture.header.Clone(),
			body:   bytes.Clone(capture.body.Bytes()),
		}
	}
	if err := json.Unmarshal(capture.body.Bytes(), &zero); err != nil {
		return zero, fmt.Errorf("decode generated Apollo response: %w", err)
	}
	return zero, nil
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
	response, err := invokeApolloFacade[apollo.ListEnvironmentsResponse](ctx, adapter.legacy, adapter.legacy.listEnvironments, nil, nil)
	return apollo.ListEnvironments200JSONResponse(response), err
}

func (adapter *apolloOpenAPIAdapter) GetEnvironment(ctx context.Context, request apollo.GetEnvironmentRequestObject) (apollo.GetEnvironmentResponseObject, error) {
	response, err := invokeApolloFacade[apollo.Environment](ctx, adapter.legacy, adapter.legacy.getEnvironment, map[string]string{"environmentRid": request.EnvironmentRid}, nil)
	return apollo.GetEnvironment200JSONResponse(response), err
}

func (adapter *apolloOpenAPIAdapter) ListInstallations(ctx context.Context, request apollo.ListInstallationsRequestObject) (apollo.ListInstallationsResponseObject, error) {
	response, err := invokeApolloFacade[apollo.ListInstallationsResponse](ctx, adapter.legacy, adapter.legacy.listInstallations, map[string]string{"environmentRid": request.EnvironmentRid}, nil)
	return apollo.ListInstallations200JSONResponse(response), err
}

func (adapter *apolloOpenAPIAdapter) GetInstallation(ctx context.Context, request apollo.GetInstallationRequestObject) (apollo.GetInstallationResponseObject, error) {
	response, err := invokeApolloFacade[apollo.Installation](ctx, adapter.legacy, adapter.legacy.getInstallation, map[string]string{
		"environmentRid": request.EnvironmentRid, "installationRid": request.InstallationRid,
	}, nil)
	return apollo.GetInstallation200JSONResponse(response), err
}

func (adapter *apolloOpenAPIAdapter) ListNamespaces(ctx context.Context, _ apollo.ListNamespacesRequestObject) (apollo.ListNamespacesResponseObject, error) {
	response, err := invokeApolloFacade[apollo.ListNamespacesResponse](ctx, adapter.legacy, adapter.legacy.listNamespaces, nil, nil)
	return apollo.ListNamespaces200JSONResponse(response), err
}

func (adapter *apolloOpenAPIAdapter) ListProducts(ctx context.Context, _ apollo.ListProductsRequestObject) (apollo.ListProductsResponseObject, error) {
	response, err := invokeApolloFacade[apollo.ListProductsResponse](ctx, adapter.legacy, adapter.legacy.listProducts, nil, nil)
	return apollo.ListProducts200JSONResponse(response), err
}

func (adapter *apolloOpenAPIAdapter) PublishHelmChart(ctx context.Context, request apollo.PublishHelmChartRequestObject) (apollo.PublishHelmChartResponseObject, error) {
	if err := validateApolloGeneratedBody[apollo.PublishHelmChartRequest](ctx); err != nil {
		return nil, err
	}
	response, err := invokeApolloFacade[apollo.PublishHelmChartResponse](ctx, adapter.legacy, adapter.legacy.publishHelmChart, map[string]string{"mavenCoordinate": request.MavenCoordinate}, request.Body)
	return apollo.PublishHelmChart200JSONResponse(response), err
}

func (adapter *apolloOpenAPIAdapter) PublishService(ctx context.Context, request apollo.PublishServiceRequestObject) (apollo.PublishServiceResponseObject, error) {
	if err := validateApolloGeneratedBody[apollo.PublishServiceRequest](ctx); err != nil {
		return nil, err
	}
	response, err := invokeApolloFacade[apollo.PublishServiceResponse](ctx, adapter.legacy, adapter.legacy.publishService, map[string]string{"mavenCoordinate": request.MavenCoordinate}, request.Body)
	return apollo.PublishService200JSONResponse(response), err
}

func (adapter *apolloOpenAPIAdapter) GetProduct(ctx context.Context, request apollo.GetProductRequestObject) (apollo.GetProductResponseObject, error) {
	response, err := invokeApolloFacade[apollo.Product](ctx, adapter.legacy, adapter.legacy.getProduct, map[string]string{"productId": request.ProductId}, nil)
	return apollo.GetProduct200JSONResponse(response), err
}

func (adapter *apolloOpenAPIAdapter) ListReleases(ctx context.Context, request apollo.ListReleasesRequestObject) (apollo.ListReleasesResponseObject, error) {
	response, err := invokeApolloFacade[apollo.ListReleasesResponse](ctx, adapter.legacy, adapter.legacy.listReleases, map[string]string{"productId": request.ProductId}, nil)
	return apollo.ListReleases200JSONResponse(response), err
}

func (adapter *apolloOpenAPIAdapter) GetRelease(ctx context.Context, request apollo.GetReleaseRequestObject) (apollo.GetReleaseResponseObject, error) {
	response, err := invokeApolloFacade[apollo.Release](ctx, adapter.legacy, adapter.legacy.getRelease, map[string]string{
		"productId": request.ProductId, "productVersion": request.ProductVersion,
	}, nil)
	return apollo.GetRelease200JSONResponse(response), err
}

func (adapter *apolloOpenAPIAdapter) PutStatusEntitiesHealth(ctx context.Context, request apollo.PutStatusEntitiesHealthRequestObject) (apollo.PutStatusEntitiesHealthResponseObject, error) {
	if err := validateApolloGeneratedBody[apollo.PutStatusEntitiesHealthRequest](ctx); err != nil {
		return nil, err
	}
	response, err := invokeApolloFacade[apollo.PutStatusEntitiesHealthResponse](ctx, adapter.legacy, adapter.legacy.putEntityHealth, map[string]string{"environmentId": request.EnvironmentId}, request.Body)
	return apollo.PutStatusEntitiesHealth200JSONResponse(response), err
}

func (adapter *apolloOpenAPIAdapter) PutStatusEntitiesInfo(ctx context.Context, request apollo.PutStatusEntitiesInfoRequestObject) (apollo.PutStatusEntitiesInfoResponseObject, error) {
	if err := validateApolloGeneratedBody[apollo.PutStatusEntitiesInfoRequest](ctx); err != nil {
		return nil, err
	}
	response, err := invokeApolloFacade[apollo.PutStatusEntitiesInfoResponse](ctx, adapter.legacy, adapter.legacy.putEntityInfo, map[string]string{"environmentId": request.EnvironmentId}, request.Body)
	return apollo.PutStatusEntitiesInfo200JSONResponse(response), err
}
