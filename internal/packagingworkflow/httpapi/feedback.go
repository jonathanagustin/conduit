package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	FeedbackRoute     = "POST /v1/packaging-runs/{runID}/feedback"
	FeedbackListRoute = "GET /v1/packaging-runs/{runID}/feedback"
)

// FeedbackNote is review input attached to one element of a packaging run.
// Approval is always false: recording a note does not approve the run or a Plan.
type FeedbackNote struct {
	ID        string    `json:"id"`
	RunID     string    `json:"runId"`
	Target    string    `json:"target"`
	Selector  string    `json:"selector"`
	Note      string    `json:"note"`
	Author    string    `json:"author"`
	Approval  bool      `json:"approval"`
	CreatedAt time.Time `json:"createdAt"`
}

// FeedbackStore keeps review notes. It has no approval method.
type FeedbackStore interface {
	Add(ctx context.Context, note FeedbackNote) (FeedbackNote, error)
	List(ctx context.Context, runID string) ([]FeedbackNote, error)
}

// MemoryFeedback is the process-local review-note log.
type MemoryFeedback struct {
	mu    sync.Mutex
	notes []FeedbackNote
	next  uint64
}

// NewMemoryFeedback returns an empty note log.
func NewMemoryFeedback() *MemoryFeedback { return &MemoryFeedback{} }

func (store *MemoryFeedback) Add(_ context.Context, note FeedbackNote) (FeedbackNote, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.next++
	note.Approval = false
	note.ID = feedbackID(note, store.next)
	store.notes = append(store.notes, note)
	return note, nil
}

func (store *MemoryFeedback) List(_ context.Context, runID string) ([]FeedbackNote, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	rows := []FeedbackNote{}
	for _, note := range store.notes {
		if note.RunID == runID {
			rows = append(rows, note)
		}
	}
	return rows, nil
}

func feedbackID(note FeedbackNote, sequence uint64) string {
	sum := sha256.Sum256([]byte(note.RunID + "\x00" + note.Target + "\x00" + note.Selector + "\x00" + note.Note))
	return "fb_" + hex.EncodeToString(sum[:6]) + hex.EncodeToString([]byte{byte(sequence)})
}

type feedbackBody struct {
	Target   string `json:"target"`
	Selector string `json:"selector"`
	Note     string `json:"note"`
}

func mountFeedback(mux *http.ServeMux, cfg Config) {
	if cfg.Feedback == nil {
		cfg.Feedback = NewMemoryFeedback()
	}
	mux.HandleFunc(FeedbackListRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		runID := request.PathValue("runID")
		if _, ok := requireRun(cfg, writer, request, runID); !ok {
			return
		}
		notes, err := cfg.Feedback.List(request.Context(), runID)
		if err != nil {
			writeError(writer, http.StatusBadGateway, "review notes are unavailable")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"items": notes})
	})
	mux.HandleFunc(FeedbackRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		runID := request.PathValue("runID")
		if _, ok := requireRun(cfg, writer, request, runID); !ok {
			return
		}
		body, err := decodeFeedback(request)
		if writeCoded(writer, err) {
			return
		}
		author := "operator"
		if cfg.OperatorName != nil {
			if name := strings.TrimSpace(cfg.OperatorName(request)); name != "" {
				author = name
			}
		}
		note, err := cfg.Feedback.Add(request.Context(), FeedbackNote{
			RunID: runID, Target: body.Target, Selector: body.Selector, Note: body.Note,
			Author: author, Approval: false, CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			writeError(writer, http.StatusBadGateway, "review note was not recorded")
			return
		}
		note.Approval = false
		writeJSON(writer, http.StatusCreated, note)
	})
}

func requireRun(cfg Config, writer http.ResponseWriter, request *http.Request, runID string) (bool, bool) {
	if cfg.Reader == nil {
		writeError(writer, http.StatusNotFound, "packaging run not found")
		return false, false
	}
	if _, err := cfg.Reader.Read(request.Context(), runID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(writer, http.StatusNotFound, "packaging run not found")
			return false, false
		}
		writeError(writer, http.StatusBadGateway, "packaging run status is unavailable")
		return false, false
	}
	return true, true
}

func decodeFeedback(request *http.Request) (feedbackBody, error) {
	var body feedbackBody
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<16))
	if err := decoder.Decode(&body); err != nil {
		return feedbackBody{}, StatusError{Status: http.StatusBadRequest, Message: "request body must be a JSON object"}
	}
	body.Target = strings.TrimSpace(body.Target)
	body.Selector = strings.TrimSpace(body.Selector)
	body.Note = strings.TrimSpace(body.Note)
	switch body.Target {
	case "file", "manifest", "step":
	default:
		return feedbackBody{}, StatusError{Status: http.StatusUnprocessableEntity, Message: "target must be file, manifest, or step"}
	}
	if body.Selector == "" || len(body.Selector) > 200 {
		return feedbackBody{}, StatusError{Status: http.StatusUnprocessableEntity, Message: "selector is required"}
	}
	if body.Note == "" || len(body.Note) > 2000 {
		return feedbackBody{}, StatusError{Status: http.StatusUnprocessableEntity, Message: "note is required"}
	}
	return body, nil
}
