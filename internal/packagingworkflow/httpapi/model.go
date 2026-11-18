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
		environment, ok := modelRunEnvironment(cfg, writer, request, runID)
		if !ok {
			return
		}
		if cfg.Model == nil {
			writeError(writer, http.StatusUnprocessableEntity, domain.ErrModelUnavailable.Error())
			return
		}
		body := &struct {
			// AttachmentSHA256 binds stored intake files as read-only context.
			AttachmentSHA256 []string `json:"attachmentSha256"`
		}{}
		if request.Body != nil {
			decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<16))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
				writeError(writer, http.StatusBadRequest, "request body must be a JSON object")
				return
			}

			if body == nil {
				writeError(writer, http.StatusBadRequest, "request body must be a JSON object")
				return
			}
			if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
				writeError(writer, http.StatusBadRequest, "request body must contain only one JSON object")
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
			RunID: runID, GateRules: domain.MemoryGateRule, AttachmentSHA256: body.AttachmentSHA256,
		}, environment, author)
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

// One recorded snapshot binds the proposal target. Existence alone cannot
// authorize substituting a default environment or another run's coordinates.
func modelRunEnvironment(cfg Config, writer http.ResponseWriter, request *http.Request, runID string) (string, bool) {
	if cfg.Reader == nil {
		writeError(writer, http.StatusNotFound, "packaging run not found")
		return "", false
	}
	run, err := cfg.Reader.Read(request.Context(), runID)
	if errors.Is(err, ErrNotFound) {
		writeError(writer, http.StatusNotFound, "packaging run not found")
		return "", false
	}
	if err != nil {
		writeError(writer, http.StatusBadGateway, "packaging run status is unavailable")
		return "", false
	}
	if runID == "" || run.RunID != runID {
		writeError(writer, http.StatusBadGateway, "packaging run status did not match the requested run")
		return "", false
	}
	environment := domain.EnvironmentOf(run.TargetPlanRef)
	_, target, _ := strings.Cut(run.TargetPlanRef, "/")
	if environment == "" || strings.TrimSpace(target) == "" || strings.ContainsAny(run.TargetPlanRef, " \t\r\n") {
		writeError(writer, http.StatusConflict, "recorded packaging target is unavailable; no proposal was requested")
		return "", false
	}
	return environment, true
}
