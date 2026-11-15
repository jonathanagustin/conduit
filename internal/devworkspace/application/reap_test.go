package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"github.com/jonathanagustin/conduit/internal/devworkspace/ports"
)

type reaperRuntime struct {
	ports.Runtime
	err    error
	status string
	calls  int
}

func (r *reaperRuntime) Stop(context.Context, string) (domain.Runtime, error) {
	r.calls++
	return domain.Runtime{Status: r.status}, r.err
}

type reaperPods struct {
	err   error
	calls int
}

func (p *reaperPods) Create(context.Context, domain.Workspace) error { return nil }
func (p *reaperPods) Delete(context.Context, string) error           { p.calls++; return p.err }

type reapAudit struct {
	failAction string
	entries    []reapEntry
}
type reapEntry struct{ subject, action, resource, decision, reason string }

func (a *reapAudit) Record(_ context.Context, subject, action, resource, decision, reason string) error {
	if action == a.failAction {
		return errReapTest
	}
	a.entries = append(a.entries, reapEntry{subject, action, resource, decision, reason})
	return nil
}

var errReapTest = errors.New("injected cleanup failure")

type reapCatalog struct {
	ports.Catalog
	fail bool
}

func (c *reapCatalog) Save(ctx context.Context, w domain.Workspace) error {
	if c.fail {
		return errReapTest
	}
	return c.Catalog.Save(ctx, w)
}

func TestReapFailuresRemainRetryableAndPreserveFiles(t *testing.T) {
	for _, mode := range []string{"runtime", "running-result", "pod", "cluster-unavailable", "audit-request", "audit-complete", "save", "missing-runtime", "missing-pods", "missing-audit"} {
		t.Run(mode, func(t *testing.T) {
			service, store, _ := newService(t)
			ctx := context.Background()
			now := service.now()
			row := domain.Workspace{ID: "wsp_reap", Owner: "ada", Status: domain.StatusRunning, LastUsed: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
			if err := store.Save(ctx, row); err != nil {
				t.Fatal(err)
			}
			if err := store.PutFile(row.ID, "work.txt", []byte("unsaved work")); err != nil {
				t.Fatal(err)
			}
			rt := &reaperRuntime{Runtime: store, status: domain.StatusStopped}
			pods := &reaperPods{}
			audit := &reapAudit{}
			catalog := &reapCatalog{Catalog: store}
			service.runtime = rt
			service.pods = pods
			service.audit = audit
			service.records = catalog
			switch mode {
			case "runtime":
				rt.err = errReapTest
			case "running-result":
				rt.status = domain.StatusRunning
			case "pod":
				pods.err = errReapTest
			case "cluster-unavailable":
				pods.err = domain.ErrNoCluster
			case "missing-audit":
				service.audit = nil
			case "audit-request":
				audit.failAction = "workspace.reap_requested"
			case "audit-complete":
				audit.failAction = "workspace.compute_reclaimed"
			case "save":
				catalog.fail = true
			case "missing-runtime":
				service.runtime = nil
			case "missing-pods":
				service.pods = nil
			}
			reaped, err := service.Reap(ctx, now)
			if err == nil || len(reaped) != 0 {
				t.Fatalf("false success: %v %v", reaped, err)
			}
			saved, _ := store.Get(ctx, row.ID)
			if saved.Status != domain.StatusRunning || !saved.UpdatedAt.Equal(row.UpdatedAt) {
				t.Fatalf("record changed: %#v", saved)
			}
			file, err := store.Read(ctx, row.ID, "work.txt", "")
			if err != nil || file.Content != "unsaved work" {
				t.Fatalf("working copy lost: %#v %v", file, err)
			}
			if (mode == "audit-request" || mode == "missing-audit") && (rt.calls != 0 || pods.calls != 0) {
				t.Fatal("cleanup ran before durable intent")
			}
			if (mode == "runtime" || mode == "running-result") && pods.calls != 0 {
				t.Fatal("pod deleted after runtime failure")
			}
			rt.err = nil
			rt.status = domain.StatusStopped
			pods.err = nil
			audit.failAction = ""
			catalog.fail = false
			service.runtime = rt
			service.pods = pods
			service.audit = audit
			reaped, err = service.Reap(ctx, now)
			if err != nil || len(reaped) != 1 || reaped[0] != row.ID {
				t.Fatalf("retry: %v %v", reaped, err)
			}
			saved, _ = store.Get(ctx, row.ID)
			if saved.Status != domain.StatusStopped {
				t.Fatal("successful cleanup not persisted")
			}
			found := false
			for _, entry := range audit.entries {
				if entry.action == "workspace.compute_reclaimed" && entry.decision == "ALLOW" {
					found = true
					if entry.subject != "system:workspace-reaper" || entry.resource != "workspace/"+row.ID || !strings.Contains(entry.reason, "owner=ada") || !strings.Contains(entry.reason, "files retained") {
						t.Fatalf("incomplete receipt: %#v", entry)
					}
				}
			}
			if !found {
				t.Fatal("missing success receipt")
			}
		})
	}
}
func TestReapMissingComputeIsIdempotentAndCancelledScanDoesNothing(t *testing.T) {
	service, store, _ := newService(t)
	ctx := context.Background()
	row := domain.Workspace{ID: "wsp_gone", Owner: "ada", Status: domain.StatusStarting, LastUsed: service.now().Add(-time.Hour)}
	if err := store.Save(ctx, row); err != nil {
		t.Fatal(err)
	}
	rt := &reaperRuntime{Runtime: store, err: domain.ErrNotFound}
	pods := &reaperPods{err: domain.ErrNotFound}
	service.runtime = rt
	service.pods = pods
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.Reap(cancelled, service.now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	if rt.calls+pods.calls != 0 {
		t.Fatal("cleanup after cancellation")
	}
	ids, err := service.Reap(ctx, service.now())
	if err != nil || len(ids) != 1 {
		t.Fatalf("already absent: %v %v", ids, err)
	}
	ids, err = service.Reap(ctx, service.now())
	if err != nil || len(ids) != 0 || rt.calls != 1 || pods.calls != 1 {
		t.Fatalf("not idempotent: %v %v", ids, err)
	}
}

func TestReapIdleBoundaryAndPartialProgress(t *testing.T) {
	service, store, _ := newService(t)
	ctx := context.Background()
	now := service.now()
	for _, row := range []domain.Workspace{
		{ID: "a_due", Owner: "ada", Status: domain.StatusRunning, LastUsed: now.Add(-service.idle)},
		{ID: "b_due", Owner: "ada", Status: domain.StatusStarting, LastUsed: now.Add(-time.Hour)},
		{ID: "fresh", Owner: "ada", Status: domain.StatusRunning, LastUsed: now.Add(-service.idle + time.Nanosecond)},
		{ID: "future", Owner: "ada", Status: domain.StatusRunning, LastUsed: now.Add(time.Hour)},
		{ID: "zero", Owner: "ada", Status: domain.StatusRunning},
		{ID: "stopped", Owner: "ada", Status: domain.StatusStopped, LastUsed: now.Add(-time.Hour)},
	} {
		if err := store.Save(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	service.runtime = &selectiveReapRuntime{Runtime: store}
	service.pods = &reaperPods{}
	ids, err := service.Reap(ctx, now)
	if !errors.Is(err, errReapTest) || len(ids) != 1 || ids[0] != "a_due" {
		t.Fatalf("partial result lost: %v %v", ids, err)
	}
	for _, id := range []string{"fresh", "future", "zero"} {
		row, _ := store.Get(ctx, id)
		if row.Status != domain.StatusRunning {
			t.Fatalf("not due: %s", id)
		}
	}
}

type selectiveReapRuntime struct{ ports.Runtime }

func (r *selectiveReapRuntime) Stop(ctx context.Context, id string) (domain.Runtime, error) {
	if id == "b_due" {
		return domain.Runtime{}, errReapTest
	}
	return r.Runtime.Stop(ctx, id)
}
