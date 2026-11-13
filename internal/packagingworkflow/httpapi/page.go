package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const (
	summaryDefaultLimit = 100
	summaryMaxLimit     = 200
	fullPageLimit       = 20
	maxCursorBytes      = 2048
)

type pageQuery struct {
	view    string
	limit   int
	cursor  string
	phase   string
	outcome string
	planID  string
	search  string
}

type pageMeta struct {
	PageSize       int
	Limit          int
	NextCursor     string
	PreviousCursor string
	HasPrevious    bool
}

type cursorBody struct {
	Version int    `json:"v"`
	Filter  string `json:"f"`
	Key     string `json:"k"`
}

type filterStamp struct {
	Phase   string `json:"phase"`
	Outcome string `json:"outcome"`
	PlanID  string `json:"planId"`
	Search  string `json:"search"`
	View    string `json:"view"`
}

func parsePage(query url.Values) (pageQuery, error) {
	view := strings.TrimSpace(query.Get("view"))
	switch view {
	case "", "full":
		view = "full"
	case "summary":
	default:
		return pageQuery{}, StatusError{Status: http.StatusUnprocessableEntity, Message: "view must be summary or full"}
	}
	maximum := fullPageLimit
	fallback := fullPageLimit
	if view == "summary" {
		maximum = summaryMaxLimit
		fallback = summaryDefaultLimit
	}
	limit := fallback
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maximum {
			return pageQuery{}, StatusError{
				Status:  http.StatusUnprocessableEntity,
				Message: fmt.Sprintf("limit must be between 1 and %d", maximum),
			}
		}
		limit = parsed
	}
	return pageQuery{
		view:    view,
		limit:   limit,
		cursor:  query.Get("cursor"),
		phase:   strings.TrimSpace(query.Get("phase")),
		outcome: strings.TrimSpace(query.Get("outcome")),
		planID:  strings.TrimSpace(query.Get("planId")),
		search:  strings.ToLower(strings.TrimSpace(query.Get("search"))),
	}, nil
}

func (q pageQuery) fingerprint() string {
	encoded, _ := json.Marshal(filterStamp{
		Phase: q.phase, Outcome: q.outcome, PlanID: q.planID, Search: q.search, View: q.view,
	})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func paginate[T any](values []T, key func(T) string, fingerprint, rawCursor string, limit int) ([]T, pageMeta, error) {
	start := 0
	if rawCursor != "" {
		cursor, err := decodeCursor(rawCursor)
		if err != nil || cursor.Version != 1 || cursor.Key == "" {
			return nil, pageMeta{}, StatusError{Status: http.StatusUnprocessableEntity, Message: "malformed opaque cursor"}
		}
		if cursor.Filter != fingerprint {
			return nil, pageMeta{}, StatusError{Status: http.StatusUnprocessableEntity, Message: "cursor does not match the active filters"}
		}
		start = sort.Search(len(values), func(index int) bool { return key(values[index]) > cursor.Key })
	}
	end := start + limit
	if end > len(values) {
		end = len(values)
	}
	meta := pageMeta{PageSize: end - start, Limit: limit, HasPrevious: start > 0}
	if meta.HasPrevious {
		previousStart := start - limit
		if previousStart < 0 {
			previousStart = 0
		}
		if previousStart > 0 {
			meta.PreviousCursor = encodeCursor(key(values[previousStart-1]), fingerprint)
		}
	}
	if end < len(values) {
		meta.NextCursor = encodeCursor(key(values[end-1]), fingerprint)
	}
	page := make([]T, end-start)
	copy(page, values[start:end])
	return page, meta, nil
}

func encodeCursor(key, fingerprint string) string {
	encoded, _ := json.Marshal(cursorBody{Version: 1, Filter: fingerprint, Key: key})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeCursor(raw string) (cursorBody, error) {
	if len(raw) > maxCursorBytes {
		return cursorBody{}, errMalformedCursor
	}
	encoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return cursorBody{}, err
	}
	var cursor cursorBody
	if err := json.Unmarshal(encoded, &cursor); err != nil {
		return cursorBody{}, err
	}
	return cursor, nil
}
