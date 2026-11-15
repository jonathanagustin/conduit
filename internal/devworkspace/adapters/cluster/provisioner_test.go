package cluster

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"testing"
)

type provisionerStub struct {
	err     error
	deletes int
}

func (p *provisionerStub) Create(context.Context, domain.Workspace) error { return p.err }
func (p *provisionerStub) Delete(context.Context, string) error           { p.deletes++; return p.err }
func TestUnavailablePrimaryDoesNotClaimRecordedDeletion(t *testing.T) {
	primary := &provisionerStub{err: domain.ErrNoCluster}
	secondary := &provisionerStub{}
	fallback := Fallback{Primary: primary, Secondary: secondary}
	if err := fallback.Create(context.Background(), domain.Workspace{}); err != nil {
		t.Fatal(err)
	}
	if err := fallback.Delete(context.Background(), "workspace"); !errors.Is(err, domain.ErrNoCluster) {
		t.Fatalf("unavailable cluster reported success: %v", err)
	}
	if secondary.deletes != 0 {
		t.Fatal("recorded fallback cannot prove cluster deletion")
	}
	primary.err = nil
	if err := fallback.Delete(context.Background(), "workspace"); err != nil {
		t.Fatal(err)
	}
	if primary.deletes != 2 || secondary.deletes != 0 {
		t.Fatal("delete authority changed")
	}
}
