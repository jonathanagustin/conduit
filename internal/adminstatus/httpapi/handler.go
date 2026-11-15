package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/adminstatus/domain"
	api "github.com/jonathanagustin/conduit/internal/adminstatus/transport"
)

type Reader interface {
	Read(context.Context) (domain.Summary, error)
}
type Config struct {
	Reader    Reader
	Authorize func(*http.Request) int
}
type server struct{ reader Reader }

func Register(mux *http.ServeMux, cfg Config) {
	guard := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("cache-control", "no-store")
			code := http.StatusUnauthorized
			if cfg.Authorize != nil {
				code = cfg.Authorize(r)
			}
			if code != 0 {
				w.Header().Set("content-type", "application/json")
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode(api.Error{Error: http.StatusText(code)})
				return
			}
			w.Header().Set("x-content-type-options", "nosniff")
			next.ServeHTTP(w, r)
		})
	}
	strict := api.NewStrictHandler(server{reader: cfg.Reader}, nil)
	api.HandlerWithOptions(strict, api.StdHTTPServerOptions{BaseRouter: mux, Middlewares: []api.MiddlewareFunc{guard}})
}
func (handler server) GetAdminStatus(ctx context.Context, _ api.GetAdminStatusRequestObject) (api.GetAdminStatusResponseObject, error) {
	if handler.reader == nil {
		return api.GetAdminStatus503JSONResponse{Error: "Status source unavailable"}, nil
	}
	snapshot, err := handler.reader.Read(ctx)
	if err != nil {
		return api.GetAdminStatus503JSONResponse{Error: "Status source unavailable"}, nil
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return api.GetAdminStatus503JSONResponse{Error: "Status source unavailable"}, nil
	}
	var response api.GetAdminStatus200JSONResponse
	if err = json.Unmarshal(data, &response); err != nil {
		return api.GetAdminStatus503JSONResponse{Error: "Status source unavailable"}, nil
	}
	return response, nil
}
