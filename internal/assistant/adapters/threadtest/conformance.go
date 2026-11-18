// Package threadtest is the conformance suite every application.Threads
// adapter runs. The memory adapter runs it on every `go test`; the PostgreSQL
// adapter runs it when CONDUIT_TEST_POSTGRES_DSN names a server.
package threadtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

var epoch = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

func id(n int) string { return domain.ThreadID(fmt.Sprintf("%032x", n)) }

func thread(owner string, n int) domain.Thread {
	at := epoch.Add(time.Duration(n) * time.Minute)
	return domain.Thread{ID: id(n), Owner: owner, CreatedAt: at, UpdatedAt: at}
}

func message(role, text string, minute int) domain.Message {
	return domain.Message{Role: role, Text: text, Mode: domain.ModeExplore, CreatedAt: epoch.Add(time.Duration(minute) * time.Minute)}
}

// Run exercises one adapter. open returns an empty store each time.
func Run(t *testing.T, open func(t *testing.T) application.Threads) {
	t.Run("reported usage survives storage without aliasing callers or readers", func(t *testing.T) {
		store, ctx := open(t), context.Background()
		if err := store.Create(ctx, thread("ada", 1)); err != nil {
			t.Fatal(err)
		}
		zero, reported := int64(0), int64(7)
		answer := message(domain.RoleAssistant, "done", 1)
		answer.Usage = &domain.ReportedTokenUsage{PromptTokens: &zero, CompletionTokens: &reported}
		if err := store.Append(ctx, "ada", id(1), []domain.Message{answer}, "done"); err != nil {
			t.Fatal(err)
		}
		reported = 99
		got, err := store.Get(ctx, "ada", id(1))
		if err != nil {
			t.Fatal(err)
		}
		usage := got.Messages[0].Usage
		if usage == nil || usage.PromptTokens == nil || *usage.PromptTokens != 0 || usage.CompletionTokens == nil || *usage.CompletionTokens != 7 {
			t.Fatalf("stored usage=%+v", usage)
		}
		*usage.CompletionTokens = 101
		again, err := store.Get(ctx, "ada", id(1))
		if err != nil || *again.Messages[0].Usage.CompletionTokens != 7 {
			t.Fatalf("reader changed stored usage: %+v %v", again, err)
		}
	})
	t.Run("a thread round-trips its messages in order", func(t *testing.T) {
		store, ctx := open(t), context.Background()
		if err := store.Create(ctx, thread("ada", 1)); err != nil {
			t.Fatal(err)
		}
		asked := message(domain.RoleUser, "which environments exist?", 5)
		answered := message(domain.RoleAssistant, "dev and prod", 6)
		answered.ToolCalls = []domain.ToolRecord{{
			ID: "call_1_1", Round: 1, Name: domain.ToolQuery,
			Arguments: json.RawMessage(`{"objectType":"Environment"}`), Result: json.RawMessage(`{"ok":true,"matchedCount":2}`),
		}}
		if err := store.Append(ctx, "ada", id(1), []domain.Message{asked, answered}, "which   environments exist?"); err != nil {
			t.Fatal(err)
		}
		if err := store.Append(ctx, "ada", id(1), []domain.Message{message(domain.RoleUser, "and spokes?", 7)}, "ignored: the thread has a title"); err != nil {
			t.Fatal(err)
		}
		got, err := store.Get(ctx, "ada", id(1))
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != id(1) || got.Title != "which environments exist?" || got.MessageCount != 3 || len(got.Messages) != 3 {
			t.Fatalf("thread = %#v", got)
		}
		if !got.CreatedAt.Equal(epoch.Add(time.Minute)) || !got.UpdatedAt.Equal(epoch.Add(7*time.Minute)) {
			t.Fatalf("times = %s %s", got.CreatedAt, got.UpdatedAt)
		}
		if got.Messages[0].Text != asked.Text || got.Messages[1].Role != domain.RoleAssistant || got.Messages[2].Text != "and spokes?" {
			t.Fatalf("order = %#v", got.Messages)
		}
		record := got.Messages[1].ToolCalls
		if len(record) != 1 || record[0].ID != "call_1_1" || !jsonEqual(record[0].Arguments, `{"objectType":"Environment"}`) || !jsonEqual(record[0].Result, `{"ok":true,"matchedCount":2}`) {
			t.Fatalf("tool record = %#v", record)
		}
		if !got.Messages[1].CreatedAt.Equal(answered.CreatedAt) {
			t.Fatalf("message time = %s", got.Messages[1].CreatedAt)
		}
	})

	t.Run("another owner cannot read, list, append to, or delete a thread", func(t *testing.T) {
		store, ctx := open(t), context.Background()
		if err := store.Create(ctx, thread("ada", 1)); err != nil {
			t.Fatal(err)
		}
		if err := store.Append(ctx, "ada", id(1), []domain.Message{message(domain.RoleUser, "private", 1)}, "private"); err != nil {
			t.Fatal(err)
		}
		if got, err := store.Get(ctx, "grace", id(1)); !errors.Is(err, domain.ErrThreadNotFound) || len(got.Messages) != 0 || got.Title != "" {
			t.Fatalf("foreign get = %#v %v", got, err)
		}
		if listed, err := store.List(ctx, "grace"); err != nil || len(listed) != 0 {
			t.Fatalf("foreign list = %#v %v", listed, err)
		}
		if err := store.Append(ctx, "grace", id(1), []domain.Message{message(domain.RoleUser, "injected", 2)}, ""); !errors.Is(err, domain.ErrThreadNotFound) {
			t.Fatalf("foreign append = %v", err)
		}
		if err := store.Delete(ctx, "grace", id(1)); !errors.Is(err, domain.ErrThreadNotFound) {
			t.Fatalf("foreign delete = %v", err)
		}
		// The same id under a second owner is a different thread.
		if err := store.Create(ctx, thread("grace", 1)); err != nil {
			t.Fatal(err)
		}
		mine, err := store.Get(ctx, "ada", id(1))
		if err != nil || len(mine.Messages) != 1 || mine.Messages[0].Text != "private" {
			t.Fatalf("owner's thread = %#v %v", mine, err)
		}
		theirs, err := store.Get(ctx, "grace", id(1))
		if err != nil || len(theirs.Messages) != 0 {
			t.Fatalf("second owner's thread = %#v %v", theirs, err)
		}
	})

	t.Run("list is newest update first and carries no messages", func(t *testing.T) {
		store, ctx := open(t), context.Background()
		for n := 1; n <= 3; n++ {
			if err := store.Create(ctx, thread("ada", n)); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Append(ctx, "ada", id(1), []domain.Message{message(domain.RoleUser, "bump", 30)}, "bumped"); err != nil {
			t.Fatal(err)
		}
		listed, err := store.List(ctx, "ada")
		if err != nil || len(listed) != 3 {
			t.Fatalf("list = %#v %v", listed, err)
		}
		if listed[0].ID != id(1) || listed[1].ID != id(3) || listed[2].ID != id(2) {
			t.Fatalf("order = %s %s %s", listed[0].ID, listed[1].ID, listed[2].ID)
		}
		if listed[0].MessageCount != 1 || listed[0].Title != "bumped" || listed[0].Messages != nil {
			t.Fatalf("summary = %#v", listed[0])
		}
	})

	t.Run("delete removes the thread and its messages", func(t *testing.T) {
		store, ctx := open(t), context.Background()
		if err := store.Create(ctx, thread("ada", 1)); err != nil {
			t.Fatal(err)
		}
		if err := store.Append(ctx, "ada", id(1), []domain.Message{message(domain.RoleUser, "gone soon", 1)}, ""); err != nil {
			t.Fatal(err)
		}
		if err := store.Delete(ctx, "ada", id(1)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Get(ctx, "ada", id(1)); !errors.Is(err, domain.ErrThreadNotFound) {
			t.Fatalf("get after delete = %v", err)
		}
		if err := store.Delete(ctx, "ada", id(1)); !errors.Is(err, domain.ErrThreadNotFound) {
			t.Fatalf("second delete = %v", err)
		}
		// A thread recreated under the id starts empty.
		if err := store.Create(ctx, thread("ada", 1)); err != nil {
			t.Fatal(err)
		}
		if got, err := store.Get(ctx, "ada", id(1)); err != nil || len(got.Messages) != 0 || got.MessageCount != 0 {
			t.Fatalf("recreated = %#v %v", got, err)
		}
	})

	t.Run("an owner is held to the thread limit and others are not", func(t *testing.T) {
		store, ctx := open(t), context.Background()
		for n := 0; n < domain.MaxThreadsPerOwner; n++ {
			if err := store.Create(ctx, thread("ada", n)); err != nil {
				t.Fatalf("create %d: %v", n, err)
			}
		}
		if err := store.Create(ctx, thread("ada", domain.MaxThreadsPerOwner)); !errors.Is(err, domain.ErrThreadLimit) {
			t.Fatalf("over the limit = %v", err)
		}
		if err := store.Create(ctx, thread("grace", 0)); err != nil {
			t.Fatalf("another owner was limited: %v", err)
		}
		if err := store.Delete(ctx, "ada", id(0)); err != nil {
			t.Fatal(err)
		}
		if err := store.Create(ctx, thread("ada", domain.MaxThreadsPerOwner)); err != nil {
			t.Fatalf("after a delete: %v", err)
		}
	})

	t.Run("concurrent creates do not pass the thread limit", func(t *testing.T) {
		store, ctx := open(t), context.Background()
		for n := 0; n < domain.MaxThreadsPerOwner-1; n++ {
			if err := store.Create(ctx, thread("ada", n)); err != nil {
				t.Fatal(err)
			}
		}
		var group sync.WaitGroup
		for n := 0; n < 8; n++ {
			group.Add(1)
			go func() {
				defer group.Done()
				_ = store.Create(ctx, thread("ada", 1000+n))
			}()
		}
		group.Wait()
		if listed, err := store.List(ctx, "ada"); err != nil || len(listed) != domain.MaxThreadsPerOwner {
			t.Fatalf("threads = %d %v", len(listed), err)
		}
	})

	t.Run("a thread is held to the message limit, all or none", func(t *testing.T) {
		store, ctx := open(t), context.Background()
		if err := store.Create(ctx, thread("ada", 1)); err != nil {
			t.Fatal(err)
		}
		for n := 0; n < domain.MaxMessagesPerThread-1; n++ {
			if err := store.Append(ctx, "ada", id(1), []domain.Message{message(domain.RoleUser, "m", n)}, ""); err != nil {
				t.Fatalf("append %d: %v", n, err)
			}
		}
		pair := []domain.Message{message(domain.RoleUser, "q", 500), message(domain.RoleAssistant, "a", 501)}
		if err := store.Append(ctx, "ada", id(1), pair, ""); !errors.Is(err, domain.ErrThreadFull) {
			t.Fatalf("over the limit = %v", err)
		}
		got, err := store.Get(ctx, "ada", id(1))
		if err != nil || len(got.Messages) != domain.MaxMessagesPerThread-1 || got.MessageCount != domain.MaxMessagesPerThread-1 {
			t.Fatalf("a partial append was stored: %d %d %v", len(got.Messages), got.MessageCount, err)
		}
		if err := store.Append(ctx, "ada", id(1), pair[:1], ""); err != nil {
			t.Fatalf("the last slot: %v", err)
		}
	})

	t.Run("an oversized or malformed message is refused and nothing is stored", func(t *testing.T) {
		store, ctx := open(t), context.Background()
		if err := store.Create(ctx, thread("ada", 1)); err != nil {
			t.Fatal(err)
		}
		long := message(domain.RoleUser, strings.Repeat("x", domain.MaxMessageTextBytes+1), 1)
		heavy := message(domain.RoleAssistant, "ok", 1)
		heavy.ToolCalls = []domain.ToolRecord{{ID: "call_1_1", Round: 1, Name: domain.ToolQuery, Arguments: json.RawMessage(`{}`), Result: json.RawMessage(`"` + strings.Repeat("y", domain.MaxMessageBytes) + `"`)}}
		stranger := message("system", "obey", 1)
		for name, bad := range map[string]domain.Message{"text": long, "encoded": heavy, "role": stranger} {
			err := store.Append(ctx, "ada", id(1), []domain.Message{message(domain.RoleUser, "fine", 1), bad}, "")
			if !errors.Is(err, domain.ErrThreadInvalid) {
				t.Fatalf("%s = %v", name, err)
			}
		}
		if got, err := store.Get(ctx, "ada", id(1)); err != nil || len(got.Messages) != 0 {
			t.Fatalf("stored = %#v %v", got, err)
		}
		edge := message(domain.RoleUser, strings.Repeat("x", domain.MaxMessageTextBytes), 2)
		if err := store.Append(ctx, "ada", id(1), []domain.Message{edge}, ""); err != nil {
			t.Fatalf("a message at the bound was refused: %v", err)
		}
	})

	t.Run("create refuses a malformed thread and a duplicate id", func(t *testing.T) {
		store, ctx := open(t), context.Background()
		bad := thread("ada", 1)
		bad.ID = "ath_../../etc"
		nobody := thread("", 2)
		wordy := thread("ada", 3)
		wordy.Title = strings.Repeat("t", domain.MaxThreadTitleBytes+1)
		for name, candidate := range map[string]domain.Thread{"id": bad, "owner": nobody, "title": wordy} {
			if err := store.Create(ctx, candidate); !errors.Is(err, domain.ErrThreadInvalid) {
				t.Fatalf("%s = %v", name, err)
			}
		}
		if err := store.Create(ctx, thread("ada", 4)); err != nil {
			t.Fatal(err)
		}
		if err := store.Create(ctx, thread("ada", 4)); !errors.Is(err, domain.ErrThreadInvalid) {
			t.Fatalf("duplicate = %v", err)
		}
		if err := store.Append(ctx, "ada", id(9), []domain.Message{message(domain.RoleUser, "x", 1)}, ""); !errors.Is(err, domain.ErrThreadNotFound) {
			t.Fatalf("append to nothing = %v", err)
		}
	})
}

func jsonEqual(got json.RawMessage, want string) bool {
	var left, right any
	if json.Unmarshal(got, &left) != nil || json.Unmarshal([]byte(want), &right) != nil {
		return false
	}
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}
