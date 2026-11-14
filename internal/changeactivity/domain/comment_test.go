package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStrictStoredDiscussionBindsRequestAndCommentIdentity(t *testing.T) {
	c := Comment{ID: "cmt_" + strings.Repeat("1", 32), RequestID: "crq_thread", Author: "author", Actor: "carrier", CreatedAt: time.Now().UTC(), Text: "discussion", ValueTier: ValueTier}
	document, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeComment(c.RequestID, c.ID, document); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		request, id string
		data        []byte
	}{{"crq_other", c.ID, document}, {c.RequestID, "cmt_" + strings.Repeat("2", 32), document}, {c.RequestID, c.ID, append(append([]byte{}, document...), []byte(` {}`)...)}, {c.RequestID, c.ID, []byte(strings.Replace(string(document), `"text":"discussion"`, `"text":"discussion","unknown":true`, 1))}} {
		if _, err := DecodeComment(test.request, test.id, test.data); !errors.Is(err, ErrCorrupt) {
			t.Fatal("corrupt stored record accepted", err)
		}
	}
	if ValidRequestID("../thread") || ValidCommentID("../comment") || ValidText("invalid\x00text") {
		t.Fatal("unsafe identity or text accepted")
	}
	c.Text = strings.Repeat("<", MaxTextBytes)
	c.Author = strings.Repeat("<", 512)
	c.Actor = c.Author
	if err := c.Validate(); err != nil {
		t.Fatal("maximum record rejected", err)
	}
	document, err = json.Marshal(c)
	if err != nil || len(document) > MaxDocumentBytes {
		t.Fatal("maximum record exceeds decoder bound", len(document), err)
	}
}
