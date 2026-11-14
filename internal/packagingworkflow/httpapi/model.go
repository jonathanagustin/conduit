package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

const ModelRoute = "POST /v1/packaging-runs/{runID}/model-proposal"

// ModelProposal runs one live proposal for a packaging run.
type ModelProposal interface {
	Propose(ctx context.Context, input domain.PackagingContext, environment, author string) (application.Result, error)
}

func mountModel(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(ModelRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		runID := request.PathValue("runID")
		if _, ok := requireRun(cfg, writer, request, runID); !ok {
			return
		}
		if cfg.Model == nil {
			writeError(writer, http.StatusUnprocessableEntity, domain.ErrModelUnavailable.Error())
			return
		}
		var body struct {
			ChartValues  string `json:"chartValues"`
			CurrentLimit string `json:"currentLimit"`
		}
		if request.Body != nil {
			decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<16))
			if err := decoder.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
				writeError(writer, http.StatusBadRequest, "request body must be a JSON object")
				return
			}
		}
		author := "operator"
		if cfg.OperatorName != nil {
			if name := strings.TrimSpace(cfg.OperatorName(request)); name != "" {
				author = name
			}
		}
		result, err := cfg.Model.Propose(request.Context(), domain.PackagingContext{
			RunID: runID, ChartValues: body.ChartValues, CurrentLimit: body.CurrentLimit,
			GateRules: "memory limit must not exceed 1Gi",
		}, "dev", author)
		if err != nil {
			writeError(writer, http.StatusBadGateway, err.Error())
			return
		}
		if result.Error != "" {
			writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": result.Error})
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
}
