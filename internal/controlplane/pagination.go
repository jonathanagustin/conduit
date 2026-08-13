package controlplane

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

var errMalformedOpaqueCursor = errors.New("malformed opaque cursor")

const (
	defaultPageLimit     = 100
	maxPageLimit         = 200
	fullCatalogPageLimit = 20
	maxCursorBytes       = 2048
)

type PageMetadata struct {
	PageSize       int    `json:"pageSize"`
	Limit          int    `json:"limit"`
	NextCursor     string `json:"nextCursor,omitempty"`
	PreviousCursor string `json:"previousCursor,omitempty"`
	HasPrevious    bool   `json:"hasPrevious"`
}

func parsePageQuery(query url.Values, defaultLimit, maximumLimit int) (string, int, error) {
	limit := defaultLimit
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maximumLimit {
			return "", 0, fmt.Errorf("limit must be between 1 and %d", maximumLimit)
		}
		limit = parsed
	}
	return query.Get("cursor"), limit, nil
}

func normalizedSearch(raw string) string { return strings.ToLower(strings.TrimSpace(raw)) }

type opaqueSeekCursor struct {
	Version int    `json:"v"`
	Filter  string `json:"f"`
	Key     string `json:"k"`
}

// paginateOrdered applies a stateless strict seek to values that are already
// authorization-filtered and sorted by a unique key. The filter fingerprint
// prevents a continuation token from silently changing its result set.
func paginateOrdered[T any](
	values []T, key func(T) string, filterFingerprint, rawCursor string, limit int, invalidCursor error,
) ([]T, PageMetadata, error) {
	start := 0
	if rawCursor != "" {
		cursor, err := decodeOpaqueSeekCursor(rawCursor)
		if err != nil || cursor.Version != 1 || cursor.Filter != filterFingerprint || cursor.Key == "" {
			return nil, PageMetadata{}, invalidCursor
		}
		start = sort.Search(len(values), func(index int) bool { return key(values[index]) > cursor.Key })
	}
	end := start + limit
	if end > len(values) {
		end = len(values)
	}
	metadata := PageMetadata{PageSize: end - start, Limit: limit, HasPrevious: start > 0}
	if metadata.HasPrevious {
		previousStart := start - limit
		if previousStart < 0 {
			previousStart = 0
		}
		if previousStart > 0 {
			metadata.PreviousCursor = encodeOpaqueSeekCursor(key(values[previousStart-1]), filterFingerprint)
		}
	}
	if end < len(values) {
		metadata.NextCursor = encodeOpaqueSeekCursor(key(values[end-1]), filterFingerprint)
	}
	page := make([]T, end-start)
	copy(page, values[start:end])
	return page, metadata, nil
}

func fingerprintJSON(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func encodeOpaqueSeekCursor(key, filterFingerprint string) string {
	encoded, _ := json.Marshal(opaqueSeekCursor{Version: 1, Filter: filterFingerprint, Key: key})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeOpaqueSeekCursor(raw string) (opaqueSeekCursor, error) {
	if len(raw) > maxCursorBytes {
		return opaqueSeekCursor{}, errMalformedOpaqueCursor
	}
	encoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return opaqueSeekCursor{}, err
	}
	var cursor opaqueSeekCursor
	if err := json.Unmarshal(encoded, &cursor); err != nil {
		return opaqueSeekCursor{}, err
	}
	return cursor, nil
}
