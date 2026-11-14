package application

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

type auditLog struct {
	events []string
}

func (log *auditLog) Record(_ context.Context, _, action, _, decision, _ string) error {
	log.events = append(log.events, action+" "+decision)
	return nil
}

func (log *auditLog) has(action, decision string) bool {
	for _, event := range log.events {
		if event == action+" "+decision {
			return true
		}
	}
	return false
}

type submitter struct {
	receipt domain.Receipt
	err     error
	calls   int
}

func (fake *submitter) Submit(context.Context, domain.Caller, domain.Submission) (domain.Receipt, error) {
	fake.calls++
	return fake.receipt, fake.err
}

func newService(t *testing.T) (*Service, *recorded.Store, *auditLog) {
	t.Helper()
	store := recorded.New()
	log := &auditLog{}
	service := New(Dependencies{
		Records: store, Files: store.Files(), Runtime: store, Pods: store.Pods(),
		Submitter: &submitter{receipt: domain.Receipt{ChangeRequestID: "crq_test", Status: "PENDING_APPROVALS"}},
		Auditor:   log, Now: func() time.Time { return time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC) },
		Idle: time.Minute, PodsEnabled: true,
	})
	return service, store, log
}

func caller() domain.Caller { return domain.Caller{Username: "ada", Teams: []string{"Developer"}} }

func TestTreeRejectsEscapeMissingAndEmpty(t *testing.T) {
	service, store, _ := newService(t)
	created, err := service.Create(context.Background(), caller(), "profile", "dev", "Sample")
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.Tree(context.Background(), caller(), created.ID, "", "", 10, nil)
	if err != nil || len(page.Entries) != 0 {
		t.Fatalf("empty tree = %#v %v", page, err)
	}
	if _, err := service.Tree(context.Background(), caller(), "missing", "", "", 10, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing workspace: %v", err)
	}
	if _, err := service.Tree(context.Background(), caller(), created.ID, "../etc/passwd", "", 10, nil); !errors.Is(err, domain.ErrPath) {
		t.Fatalf("escape: %v", err)
	}
	if err := store.PutFile(created.ID, "src/main.txt", []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 60; index++ {
		if err := store.PutFile(created.ID, "n/file-"+strconv.Itoa(index)+".txt", []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	first, err := service.Tree(context.Background(), caller(), created.ID, "n", "", 20, nil)
	if err != nil || len(first.Entries) != 20 || first.NextCursor == "" {
		t.Fatalf("page = %#v %v", first, err)
	}
	second, err := service.Tree(context.Background(), caller(), created.ID, "n", first.NextCursor, 20, nil)
	if err != nil || len(second.Entries) == 0 || second.Entries[0].Path == first.Entries[0].Path {
		t.Fatalf("next page = %#v %v", second, err)
	}
}

func TestFileRedactsBinaryOversizeAndStaleRevision(t *testing.T) {
	service, store, log := newService(t)
	created, err := service.Create(context.Background(), caller(), "profile", "dev", "Sample")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutFile(created.ID, "secret.txt", []byte("password=hunter2\n")); err != nil {
		t.Fatal(err)
	}
	view, err := service.File(context.Background(), caller(), created.ID, "secret.txt", "", nil)
	if err != nil || !view.Redacted || strings.Contains(view.Content, "hunter2") {
		t.Fatalf("redaction = %#v %v", view, err)
	}
	listed, err := service.Tree(context.Background(), caller(), created.ID, "", "", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	var digest string
	for _, entry := range listed.Entries {
		if entry.Path == "secret.txt" {
			digest = entry.Digest
		}
	}
	if err := store.PutFile(created.ID, "secret.txt", []byte("password=changed\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.File(context.Background(), caller(), created.ID, "secret.txt", digest, nil); !errors.Is(err, domain.ErrStale) {
		t.Fatalf("stale read: %v", err)
	}
	if err := store.PutFile(created.ID, "blob.bin", []byte{0, 1, 2}); err != nil {
		t.Fatal(err)
	}
	binary, err := service.File(context.Background(), caller(), created.ID, "blob.bin", "", nil)
	if err != nil || !binary.Binary || binary.Content != "" || binary.DownloadPath == "" {
		t.Fatalf("binary = %#v %v", binary, err)
	}
	if err := store.PutFile(created.ID, "big.txt", bytesOf(domain.MaxFileBytes+1)); err != nil {
		t.Fatal(err)
	}
	large, err := service.File(context.Background(), caller(), created.ID, "big.txt", "", nil)
	if err != nil || !large.TooLarge || large.Content != "" {
		t.Fatalf("large = %#v %v", large, err)
	}
	if _, err := service.File(context.Background(), caller(), created.ID, "missing.txt", "", nil); !errors.Is(err, domain.ErrMissing) {
		t.Fatalf("missing file: %v", err)
	}
	if _, err := service.File(context.Background(), caller(), created.ID, "/etc/passwd", "", nil); !errors.Is(err, domain.ErrPath) {
		t.Fatalf("denied path: %v", err)
	}
	if log.has("plan.approved", "ALLOW") {
		t.Fatal("file read recorded an approval")
	}
}

func TestDiffShowsChangesAndRefusesStaleWithoutApproval(t *testing.T) {
	service, store, log := newService(t)
	created, err := service.Create(context.Background(), caller(), "profile", "dev", "Sample")
	if err != nil {
		t.Fatal(err)
	}
	created.ProposalRevision = "rev-2"
	created.SourceDigest = "sha256:abc"
	created.ArtifactDigest = "sha256:def"
	if err := store.Save(context.Background(), created); err != nil {
		t.Fatal(err)
	}
	if err := store.PutBase(created.ID, "keep.txt", []byte("same\n")); err != nil {
		t.Fatal(err)
	}
	if err := store.PutFile(created.ID, "keep.txt", []byte("same\n")); err != nil {
		t.Fatal(err)
	}
	if err := store.PutBase(created.ID, "gone.txt", []byte("old\n")); err != nil {
		t.Fatal(err)
	}
	if err := store.PutFile(created.ID, "new.txt", []byte("fresh\n")); err != nil {
		t.Fatal(err)
	}
	if err := store.PutBase(created.ID, "edit.txt", []byte("before\n")); err != nil {
		t.Fatal(err)
	}
	if err := store.PutFile(created.ID, "edit.txt", []byte("after\n")); err != nil {
		t.Fatal(err)
	}
	diff, err := service.Diff(context.Background(), caller(), created.ID, "rev-2", nil)
	if err != nil || !diff.Current || diff.ProposalRevision != "rev-2" || diff.SourceDigest != "sha256:abc" {
		t.Fatalf("diff identity = %#v %v", diff, err)
	}
	changes := map[string]string{}
	for _, file := range diff.Files {
		changes[file.Path] = file.Change
	}
	if changes["new.txt"] != "added" || changes["gone.txt"] != "removed" || changes["edit.txt"] != "changed" || changes["keep.txt"] != "" {
		t.Fatalf("changes = %#v", changes)
	}
	if _, err := service.Diff(context.Background(), caller(), created.ID, "rev-1", nil); !errors.Is(err, domain.ErrStale) {
		t.Fatalf("stale diff: %v", err)
	}
	empty, store2, log2 := newService(t)
	blank, err := empty.Create(context.Background(), caller(), "profile", "dev", "Empty")
	if err != nil {
		t.Fatal(err)
	}
	none, err := empty.Diff(context.Background(), caller(), blank.ID, blank.ProposalRevision, nil)
	if err != nil || len(none.Files) != 0 || !none.Current {
		t.Fatalf("empty diff = %#v %v", none, err)
	}
	if log.has("plan.approved", "ALLOW") || log2.has("plan.approved", "ALLOW") {
		t.Fatal("diff recorded an approval")
	}
	_ = store2
}

func TestRuntimeStartStopAndLogs(t *testing.T) {
	service, store, log := newService(t)
	created, err := service.Create(context.Background(), caller(), "profile", "dev", "Sample")
	if err != nil {
		t.Fatal(err)
	}
	other := domain.Caller{Username: "bea"}
	if _, err := service.StartRuntime(context.Background(), other, created.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("cross user start: %v", err)
	}
	if _, err := service.StartRuntime(context.Background(), domain.Caller{}, created.ID); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("anonymous start: %v", err)
	}
	store.FailNextStart()
	if _, err := service.StartRuntime(context.Background(), caller(), created.ID); err == nil {
		t.Fatal("failed start was accepted")
	}
	first, err := service.StartRuntime(context.Background(), caller(), created.ID)
	if err != nil || first.Status != domain.StatusRunning || first.Origin != domain.OriginLocalRuntime {
		t.Fatalf("start = %#v %v", first, err)
	}
	second, err := service.StartRuntime(context.Background(), caller(), created.ID)
	if err != nil || second.ID != first.ID || store.Starts() != 2 {
		t.Fatalf("double start id=%s starts=%d err=%v", second.ID, store.Starts(), err)
	}
	store.AppendLog(created.ID, "password=hunter2")
	for index := 0; index < 80; index++ {
		store.AppendLog(created.ID, strings.Repeat("x", 1000))
	}
	page, err := service.Logs(context.Background(), caller(), created.ID, "", 0, false)
	if err != nil || page.Origin != domain.OriginLocalRuntime {
		t.Fatalf("logs = %#v %v", page, err)
	}
	joined := strings.Join(page.Lines, "\n")
	if strings.Contains(joined, "hunter2") || !strings.Contains(joined, "[truncated]") {
		t.Fatalf("log bound missing truncation, lines=%d", len(page.Lines))
	}
	if _, err := service.Logs(context.Background(), other, created.ID, "", 0, false); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("cross user logs: %v", err)
	}
	stopped, err := service.StopRuntime(context.Background(), caller(), created.ID)
	if err != nil || stopped.Status != domain.StatusStopped {
		t.Fatalf("stop = %#v %v", stopped, err)
	}
	before := len(page.Lines)
	store.AppendLog(created.ID, "after stop")
	after, err := service.Logs(context.Background(), caller(), created.ID, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(after.Lines, "\n") != strings.Join(page.Lines[:min(before, len(after.Lines))], "\n") && containsLine(after.Lines, "after stop") {
		t.Fatalf("logs grew after stop: %v", after.Lines)
	}
	if !log.has("workspace.runtime_started", "ALLOW") || !log.has("workspace.runtime_stopped", "ALLOW") {
		t.Fatalf("audit = %#v", log.events)
	}
}

func TestForkUpgradeDownloadAndSubmit(t *testing.T) {
	service, store, log := newService(t)
	created, err := service.Create(context.Background(), caller(), "profile", "dev", "Sample")
	if err != nil {
		t.Fatal(err)
	}
	created.ApprovalID = "appr-1"
	created.ValidationOutcome = "pass"
	created.SourceDigest = "sha256:old"
	created.SignatureStatus = domain.SignatureFailed
	if err := store.Save(context.Background(), created); err != nil {
		t.Fatal(err)
	}
	if err := store.PutFile(created.ID, "README.md", []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	child, err := service.Fork(context.Background(), caller(), created.ID, "Copy")
	if err != nil || child.ParentID != created.ID || child.ApprovalID != "" || child.ID == created.ID {
		t.Fatalf("fork = %#v %v", child, err)
	}
	parent, err := service.Get(context.Background(), caller(), created.ID, nil)
	if err != nil || parent.Title != "Sample" || parent.ApprovalID != "appr-1" {
		t.Fatalf("parent changed: %#v %v", parent, err)
	}
	upgraded, err := service.Upgrade(context.Background(), caller(), created.ID, "sha256:new", "2", "tools-2")
	if err != nil || upgraded.ApprovalID != "" || upgraded.ValidationOutcome != "" || upgraded.SourceDigest != "sha256:new" || upgraded.ProposalRevision == "rev-1" {
		t.Fatalf("upgrade = %#v %v", upgraded, err)
	}
	same, err := service.Upgrade(context.Background(), caller(), created.ID, "sha256:new", "2", "tools-2")
	if err != nil || same.ProposalRevision != upgraded.ProposalRevision {
		t.Fatalf("repeat upgrade = %#v %v", same, err)
	}
	body, err := service.Download(context.Background(), caller(), created.ID, nil)
	if err != nil || body.SignatureValid || body.SignatureStatus != domain.SignatureFailed {
		t.Fatalf("download = %#v %v", body, err)
	}
	if _, err := service.Download(context.Background(), caller(), "missing", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing download: %v", err)
	}
	receipt, err := service.Submit(context.Background(), caller(), created.ID, "Review", "please", false)
	if err != nil || receipt.ChangeRequestID == "" || receipt.Status == "APPROVED" {
		t.Fatalf("submit = %#v %v", receipt, err)
	}
	if _, err := service.Submit(context.Background(), caller(), created.ID, "Review", "please", true); !errors.Is(err, domain.ErrSelfApproval) {
		t.Fatalf("self approval: %v", err)
	}
	if log.has("plan.approved", "ALLOW") {
		t.Fatal("workspace action approved a plan")
	}
}

func TestIdlePodIsReaped(t *testing.T) {
	service, store, _ := newService(t)
	service.idle = time.Millisecond
	created, err := service.Create(context.Background(), caller(), "profile", "dev", "Sample")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRuntime(context.Background(), caller(), created.ID); err != nil {
		t.Fatal(err)
	}
	row, err := store.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	row.LastUsed = time.Date(2026, 11, 14, 11, 0, 0, 0, time.UTC)
	row.Status = domain.StatusRunning
	if err := store.Save(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	reaped, err := service.Reap(context.Background(), service.now())
	if err != nil || len(reaped) != 1 || reaped[0] != created.ID {
		t.Fatalf("reaped = %#v %v", reaped, err)
	}
}

func TestUnconfiguredRuntimeFailsClosed(t *testing.T) {
	store := recorded.New()
	service := New(Dependencies{Records: store, Files: store.Files(), Auditor: &auditLog{}, Now: time.Now})
	created, err := service.Create(context.Background(), caller(), "profile", "dev", "Sample")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRuntime(context.Background(), caller(), created.ID); !errors.Is(err, domain.ErrDisabled) {
		t.Fatalf("unconfigured start: %v", err)
	}
}

func bytesOf(n int) []byte {
	return make([]byte, n)
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
