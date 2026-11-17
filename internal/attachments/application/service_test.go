package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/attachments/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/attachments/application"
	"github.com/jonathanagustin/conduit/internal/attachments/domain"
)

type capture struct{ events []application.Audit }

func (c *capture) Record(_ context.Context, event application.Audit) error {
	c.events = append(c.events, event)
	return nil
}

func newService() (application.Service, *capture, *recorded.Store) {
	audit, store := &capture{}, recorded.New()
	return application.Service{Store: store, Audit: audit}, audit, store
}

func TestAddStoresByDigestAndAudits(t *testing.T) {
	service, audit, _ := newService()
	ctx := context.Background()
	item, err := service.Add(ctx, "ada", "values.yaml", []byte("memory: 512Mi\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !domain.ValidDigest(item.SHA256) || item.SHA256 != domain.Digest([]byte("memory: 512Mi\n")) {
		t.Fatalf("digest %q", item.SHA256)
	}
	if len(audit.events) != 1 || audit.events[0].SHA256 != item.SHA256 || audit.events[0].Outcome != application.OutcomeStored {
		t.Fatalf("audit %+v", audit.events)
	}
	again, err := service.Add(ctx, "ada", "values.yaml", []byte("memory: 512Mi\n"))
	if err != nil || again.SHA256 != item.SHA256 {
		t.Fatalf("repeat: %+v %v", again, err)
	}
	if len(audit.events) != 1 {
		t.Fatal("a repeat upload must not add a second audit record")
	}
	rows, _ := service.List(ctx, "ada")
	if len(rows) != 1 {
		t.Fatalf("rows %d", len(rows))
	}
	stored, err := service.Get(ctx, "ada", item.SHA256)
	if err != nil || stored.Content != "memory: 512Mi\n" {
		t.Fatalf("get %+v %v", stored, err)
	}
}

func TestRejectedUploadIsNotStored(t *testing.T) {
	service, audit, _ := newService()
	ctx := context.Background()
	for name, body := range map[string]string{"a.png": "x", "values.yaml": "password=hunter2\n"} {
		_, err := service.Add(ctx, "ada", name, []byte(body))
		if !errors.Is(err, domain.ErrRejected) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	rows, _ := service.List(ctx, "ada")
	if len(rows) != 0 {
		t.Fatalf("rejected files were stored: %+v", rows)
	}
	for _, event := range audit.events {
		if event.Outcome != application.OutcomeRejected || event.SHA256 != "" {
			t.Fatalf("audit %+v", event)
		}
	}
}

func TestCountAndTotalCaps(t *testing.T) {
	service, _, _ := newService()
	ctx := context.Background()
	for i := 0; i < domain.MaxCount; i++ {
		if _, err := service.Add(ctx, "ada", fmt.Sprintf("n%d.txt", i), []byte(fmt.Sprintf("note %d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Add(ctx, "ada", "extra.txt", []byte("one more")); !errors.Is(err, domain.ErrTooLarge) {
		t.Fatalf("count cap: %v", err)
	}
	if _, err := service.Add(ctx, "ada", "n0.txt", []byte("note 0")); err != nil {
		t.Fatalf("a known digest stays addable: %v", err)
	}

	small, _, _ := newService()
	chunk := strings.Repeat("a", domain.MaxFileBytes)
	for i := 0; i < 4; i++ {
		if _, err := small.Add(ctx, "ada", fmt.Sprintf("c%d.txt", i), []byte(chunk[:len(chunk)-i])); err != nil {
			t.Fatalf("chunk %d: %v", i, err)
		}
	}
	if _, err := small.Add(ctx, "ada", "c4.txt", []byte(chunk[:len(chunk)-9])); !errors.Is(err, domain.ErrTooLarge) {
		t.Fatalf("total cap: %v", err)
	}
}

func TestAnotherOperatorCannotReadAnAttachment(t *testing.T) {
	service, audit, store := newService()
	ctx := context.Background()
	item, err := service.Add(ctx, "ada", "notes.txt", []byte("private note\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(ctx, domain.Stored{Attachment: domain.Attachment{Name: "old.txt", Kind: domain.KindText, Size: 3, SHA256: domain.Digest([]byte("old"))}, Content: "old"}); err != nil {
		t.Fatal(err)
	}
	legacy := domain.Digest([]byte("old"))
	for _, actor := range []string{"", "ada", "bea"} {
		rows, err := service.List(ctx, actor)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.SHA256 == legacy {
				t.Fatalf("%s listed an unowned attachment", actor)
			}
		}
		if _, err := service.Get(ctx, actor, legacy); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("%s legacy get: %v", actor, err)
		}
	}
	if _, err := service.Get(ctx, "bea", item.SHA256); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("bea get: %v", err)
	}
	rows, err := service.List(ctx, "bea")
	if err != nil || len(rows) != 0 {
		t.Fatalf("bea list %+v %v", rows, err)
	}
	if _, err := service.Resolve(ctx, "bea", []string{item.SHA256}); !errors.Is(err, domain.ErrRejected) {
		t.Fatalf("bea resolve: %v", err)
	}
	got, err := service.Get(ctx, "ada", item.SHA256)
	if err != nil || got.Content != "private note\n" {
		t.Fatalf("ada get %+v %v", got, err)
	}
	var denied bool
	for _, event := range audit.events {
		if event.Action == "intake.read" && event.Actor == "bea" && event.SHA256 == item.SHA256 && event.Outcome == application.OutcomeDenied {
			denied = true
		}
	}
	if !denied {
		t.Fatalf("audit %+v", audit.events)
	}
}

func TestResolveBindsStoredBytesOnly(t *testing.T) {
	service, _, _ := newService()
	ctx := context.Background()
	item, _ := service.Add(ctx, "ada", "Dockerfile", []byte("FROM scratch\n"))
	got, err := service.Resolve(ctx, "ada", []string{item.SHA256, item.SHA256})
	if err != nil || len(got) != 1 || got[0].Content != "FROM scratch\n" {
		t.Fatalf("resolve %+v %v", got, err)
	}
	for _, bad := range []string{strings.ToUpper(item.SHA256), "abc", strings.Repeat("0", 64)} {
		if _, err := service.Resolve(ctx, "ada", []string{bad}); !errors.Is(err, domain.ErrRejected) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}
