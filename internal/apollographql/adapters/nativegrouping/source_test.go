package nativegrouping

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollographql/ports"
)

func TestBundleAuthorityPrecedesNativeRead(t *testing.T) {
	for _, canRead := range []func() bool{nil, func() bool { return false }} {
		called := false
		ids, err := (Source{CanReadRoot: canRead, ReadBundleIDs: func(context.Context) ([]string, error) { called = true; return []string{"private"}, nil }}).BundleIDs(context.Background())
		if !errors.Is(err, ports.ErrNotAuthorized) || ids != nil || called {
			t.Fatalf("unauthorized native read: ids=%v err=%v called=%v", ids, err, called)
		}
	}
}

func TestBundleReadPreservesContextAndOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var order []string
	want := []string{"bundle-z", "bundle-a"}
	source := Source{CanReadRoot: func() bool { order = append(order, "authorize"); return true }, ReadBundleIDs: func(got context.Context) ([]string, error) {
		if got != ctx {
			t.Fatal("context changed")
		}
		order = append(order, "read")
		return want, nil
	}}
	got, err := source.BundleIDs(ctx)
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(order, []string{"authorize", "read"}) {
		t.Fatalf("native read changed: %v %v order=%v", got, err, order)
	}
}

func TestBundleReadFailureIsNotAnEmptySuccess(t *testing.T) {
	for _, read := range []func(context.Context) ([]string, error){nil, func(context.Context) ([]string, error) {
		return []string{"partial"}, errors.New("private repository diagnostics")
	}} {
		ids, err := (Source{CanReadRoot: func() bool { return true }, ReadBundleIDs: read}).BundleIDs(context.Background())
		if ids != nil || err == nil || err.Error() != "native Bundle data could not be read" {
			t.Fatalf("failure disclosed or became absence: %v %v", ids, err)
		}
	}
}

func TestSpaceLookupIsBoundAndUnavailableIsExplicit(t *testing.T) {
	if id, err := (Source{}).SpaceID(context.Background(), "Environment", "edge-a"); id != "" || err == nil || err.Error() != "native Space data is not configured" {
		t.Fatalf("missing Space source: %q %v", id, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sentinel := errors.New("lookup refused")
	source := Source{LookupSpaceID: func(got context.Context, kind, id string) (string, error) {
		if got != ctx || kind != "Environment" || id != "edge-a" {
			t.Fatalf("binding changed: %v %s %s", got, kind, id)
		}
		return "space-a", sentinel
	}}
	id, err := source.SpaceID(ctx, "Environment", "edge-a")
	if id != "space-a" || err != sentinel {
		t.Fatalf("lookup result changed: %q %v", id, err)
	}
}
