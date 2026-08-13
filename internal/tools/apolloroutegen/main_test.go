package main

import (
	"strings"
	"testing"
)

func document(paths map[string]map[string]string) openAPIDocument {
	parsed := openAPIDocument{Paths: map[string]map[string]struct {
		OperationID string `yaml:"operationId"`
	}{}}
	for path, operations := range paths {
		parsed.Paths[path] = map[string]struct {
			OperationID string `yaml:"operationId"`
		}{}
		for method, id := range operations {
			parsed.Paths[path][method] = struct {
				OperationID string `yaml:"operationId"`
			}{OperationID: id}
		}
	}
	return parsed
}

func TestCollectRoutes(t *testing.T) {
	t.Parallel()

	t.Run("orders routes independently of map iteration", func(t *testing.T) {
		t.Parallel()
		// Go randomises map iteration, so without the sort this generator
		// emits a different file on every run and the regenerate-and-diff gate
		// fails on an unchanged snapshot. Running it repeatedly is the only
		// way to see that.
		source := document(map[string]map[string]string{
			"/v0/zulu":  {"get": "zulu"},
			"/v0/alpha": {"get": "alpha", "post": "mike"},
			"/v0/bravo": {"delete": "bravo"},
		})
		var first string
		for attempt := range 20 {
			routes, err := collectRoutes(source)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, item := range routes {
				names = append(names, item.Operation)
			}
			joined := strings.Join(names, ",")
			if attempt == 0 {
				first = joined
				if joined != "alpha,bravo,mike,zulu" {
					t.Fatalf("order = %q, want alpha,bravo,mike,zulu", joined)
				}
				continue
			}
			if joined != first {
				t.Fatalf("run %d gave %q, run 0 gave %q", attempt, joined, first)
			}
		}
	})

	t.Run("the pattern is the pinned path verbatim", func(t *testing.T) {
		t.Parallel()
		// A Go 1.22 ServeMux wildcard is spelled exactly like an OpenAPI path
		// template. Rewriting anything here would serve a route the snapshot
		// does not describe.
		routes, err := collectRoutes(document(map[string]map[string]string{
			"/api/v0/environments/{environmentRid}/installations/{installationRid}": {"get": "getInstallation"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		want := "GET /api/v0/environments/{environmentRid}/installations/{installationRid}"
		if routes[0].Pattern != want {
			t.Errorf("pattern = %q, want %q", routes[0].Pattern, want)
		}
	})

	t.Run("recognises every method the adapter serves, in upper case", func(t *testing.T) {
		t.Parallel()
		for method, want := range map[string]string{
			"get": "GET", "post": "POST", "put": "PUT", "patch": "PATCH", "delete": "DELETE",
			"GET": "GET", "Post": "POST",
		} {
			routes, err := collectRoutes(document(map[string]map[string]string{"/p": {method: "op"}}))
			if err != nil {
				t.Fatalf("%s: %v", method, err)
			}
			if len(routes) != 1 || !strings.HasPrefix(routes[0].Pattern, want+" ") {
				t.Errorf("%s gave %#v, want a %s route", method, routes, want)
			}
		}
	})

	t.Run("ignores spec entries that are not operations", func(t *testing.T) {
		t.Parallel()
		// parameters and servers sit beside the methods in a path item; they
		// are not routes and must not become handlers.
		routes, err := collectRoutes(document(map[string]map[string]string{
			"/p": {"get": "real", "parameters": "", "servers": ""},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if len(routes) != 1 || routes[0].Operation != "real" {
			t.Errorf("routes = %#v, want only the GET", routes)
		}
	})

	t.Run("refuses a snapshot it cannot bind", func(t *testing.T) {
		t.Parallel()
		for name, test := range map[string]struct {
			given openAPIDocument
			want  string
		}{
			"no paths at all": {openAPIDocument{}, "no paths"},
			// An operation with no operationId has no handler name, so the
			// generated file would silently omit a route the contract declares.
			"no operationId": {document(map[string]map[string]string{"/p": {"get": "  "}}), "operationId"},
			// Two paths claiming one operationId means one handler serving both
			// or one route silently lost, depending on map order.
			"duplicate operationId": {document(map[string]map[string]string{
				"/first": {"get": "shared"}, "/second": {"post": "shared"},
			}), "claimed by both"},
		} {
			_, err := collectRoutes(test.given)
			if err == nil {
				t.Errorf("%s was accepted", name)
				continue
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("%s: error %q does not mention %q", name, err, test.want)
			}
		}
	})

	t.Run("a duplicate names both claimants", func(t *testing.T) {
		t.Parallel()
		_, err := collectRoutes(document(map[string]map[string]string{
			"/first": {"get": "shared"}, "/second": {"post": "shared"},
		}))
		if err == nil {
			t.Fatal("a duplicate operationId was accepted")
		}
		// Naming only one leaves the reader grepping the snapshot for the other.
		for _, fragment := range []string{"/first", "/second", "shared"} {
			if !strings.Contains(err.Error(), fragment) {
				t.Errorf("error %q omits %q", err, fragment)
			}
		}
	})
}

// provenance decides what the generated header records. It must name the
// snapshot, not the machine: an absolute path makes a local run and a CI run
// produce different files, which breaks the regenerate-and-diff gate for
// reasons that have nothing to do with the contract.
func TestProvenanceNamesTheSnapshotNotThePath(t *testing.T) {
	t.Parallel()
	const snapshot = "apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml"
	for name, given := range map[string]string{
		"absolute": "/workspaces/platform/docs/apollo/snapshots/" + snapshot,
		"relative": "docs/apollo/snapshots/" + snapshot,
		"unclean":  "docs/apollo/../apollo/snapshots/" + snapshot,
		"trailing": "docs/apollo/snapshots/" + snapshot + "/",
	} {
		// No Windows case: filepath.ToSlash is a no-op on anything but Windows,
		// so asserting backslash handling here would assert a behaviour this
		// platform does not have.
		if got := provenance(given); got != snapshot {
			t.Errorf("%s: provenance(%q) = %q, want %q", name, given, got, snapshot)
		}
	}
	// Too short to have a parent directory: keep what there is rather than panic.
	if got := provenance("api-definition.yml"); got != "api-definition.yml" {
		t.Errorf("bare filename gave %q", got)
	}
}

func TestRenderProducesCompilableSortedSource(t *testing.T) {
	t.Parallel()
	routes := []route{
		{Operation: "getProduct", Pattern: "GET /api/v0/products/{productId}"},
		{Operation: "listProducts", Pattern: "GET /api/v0/products"},
	}
	rendered, err := render("controlplane", "snapshots/pinned-abc/api-definition.yml", routes)
	if err != nil {
		t.Fatal(err)
	}
	source := string(rendered)
	for _, want := range []string{
		// The header has to name the snapshot, or a regenerated file gives no
		// clue which contract it came from.
		"generated by apolloroutegen from pinned-abc/api-definition.yml",
		"DO NOT EDIT",
		"package controlplane",
		`{Operation: "getProduct", Pattern: "GET /api/v0/products/{productId}"}`,
		`{Operation: "listProducts", Pattern: "GET /api/v0/products"}`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("rendered source is missing %q:\n%s", want, source)
		}
	}
	// format.Source already ran; if it did not, the output would not be
	// gofmt-stable and the committed file would fail the formatting gate.
	if strings.Contains(source, "\t{Operation:") == false {
		t.Error("rendered source is not gofmt-indented")
	}
}
