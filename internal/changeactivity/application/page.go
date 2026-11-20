package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"io"
	"strings"
	"time"
)

// CommentPage is the native fixed-size discussion view, not a snapshot.
type CommentPage struct {
	Comments   []domain.Comment `json:"comments"`
	NextCursor string           `json:"nextCursor"`
	ValueTier  string           `json:"valueTier"`
}

// ReadPage preserves native cursor validation before the authorized list read.
// Only the response is bounded: the existing Store.List contract reads history.
func (s *Service) ReadPage(ctx context.Context, id, cursor string) (CommentPage, error) {
	position, err := readPageCursor(cursor, id)
	if err != nil {
		return CommentPage{}, err
	}
	comments, err := s.List(ctx, id)
	if err != nil {
		return CommentPage{}, err
	}
	page := CommentPage{Comments: make([]domain.Comment, 0, domain.MaxPageSize), ValueTier: domain.ValueTier}
	more := false
	for _, c := range comments {
		if !position.At.IsZero() && (c.CreatedAt.Before(position.At) || (c.CreatedAt.Equal(position.At) && c.ID <= position.ID)) {
			continue
		}
		if len(page.Comments) == domain.MaxPageSize {
			more = true
			break
		}
		page.Comments = append(page.Comments, c)
	}
	if more {
		last := page.Comments[len(page.Comments)-1]
		data, _ := json.Marshal(pageCursor{RequestID: id, ID: last.ID, At: last.CreatedAt})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return page, nil
}

type pageCursor struct {
	RequestID string    `json:"request"`
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
}

func readPageCursor(raw, id string) (pageCursor, error) {
	if raw == "" {
		return pageCursor{}, nil
	}
	if len(raw) > 1024 {
		return pageCursor{}, domain.ErrInvalid
	}
	document, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return pageCursor{}, domain.ErrInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(document)))
	decoder.DisallowUnknownFields()
	var c pageCursor
	if err := decoder.Decode(&c); err != nil {
		return pageCursor{}, domain.ErrInvalid
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return pageCursor{}, domain.ErrInvalid
	}
	if c.RequestID != id || !domain.ValidRequestID(c.RequestID) || !domain.ValidCommentID(c.ID) || c.At.IsZero() {
		return pageCursor{}, domain.ErrInvalid
	}
	return c, nil
}
