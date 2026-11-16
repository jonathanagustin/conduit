package httpapi

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	catalogv1 "github.com/jonathanagustin/conduit/gen/go/conduit/catalog/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/catalog/v1/catalogv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

const ProductSummaryConnectRoute = "POST /conduit.catalog.v1.ProductReadService/ListProductSummaries"

// ProductSummaryConfig binds an authenticated native carrier to the shared read.
type ProductSummaryConfig struct {
	Authenticate func(*http.Request) (context.Context, error)
	Read         func(context.Context, string, string, int) (controlplane.ProductCatalogSummaryPage, error)
}

// RegisterProductSummaryConnect mounts only the contracted read. Authentication
// precedes protobuf decoding, including malformed or unsupported requests.
func RegisterProductSummaryConnect(mux *http.ServeMux, cfg ProductSummaryConfig) {
	server := connect.NewServer()
	catalogv1connect.RegisterProductReadServiceHandler(server, &productSummaryService{cfg: cfg})
	rpc := http.NewServeMux()
	connecthttp.Mount(rpc, server)
	mux.Handle(ProductSummaryConnectRoute, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var ctx context.Context
		err := controlplane.ErrUnauthorized
		if cfg.Authenticate != nil {
			ctx, err = cfg.Authenticate(request)
		}
		if err != nil || ctx == nil {
			code := connect.CodeInternal
			if err == nil || errors.Is(err, controlplane.ErrUnauthorized) {
				code = connect.CodeUnauthenticated
			} else if errors.Is(err, controlplane.ErrForbidden) {
				code = connect.CodePermissionDenied
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_ = connecthttp.NewErrorWriter().Write(w, request, connect.NewError(code, code.String()))
			return
		}
		rpc.ServeHTTP(w, request.WithContext(ctx))
	}))
}

type productSummaryService struct{ cfg ProductSummaryConfig }

func (service *productSummaryService) ListProductSummaries(ctx context.Context, request *catalogv1.ListProductSummariesRequest) (*catalogv1.ListProductSummariesResponse, error) {
	limit, err := controlplane.ResolveProductSummaryLimit(request.Limit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	if service.cfg.Read == nil {
		return nil, connect.NewError(connect.CodeUnavailable, "Product summary index unavailable")
	}
	page, err := service.cfg.Read(ctx, request.Search, request.Cursor, limit)
	if err != nil {
		if errors.Is(err, controlplane.ErrInvalidProductSummaryQuery) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
		}
		return nil, connect.NewError(connect.CodeUnavailable, "Product summary index unavailable")
	}
	response := &catalogv1.ListProductSummariesResponse{
		Total: int64(page.Total), PageSize: int32(page.PageSize), Limit: int32(page.Limit),
		NextCursor: page.NextCursor, PreviousCursor: page.PreviousCursor, HasPrevious: page.HasPrevious,
	}
	for _, product := range page.Products {
		response.Products = append(response.Products, &catalogv1.ProductSummary{
			Product: nativeIndexString(product.Product), ReleaseCount: int64(product.ReleaseCount), LatestVersion: nativeIndexString(product.LatestVersion),
		})
	}
	return response, nil
}
