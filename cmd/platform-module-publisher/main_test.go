package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Module definition small enough to write inline. The publisher's own rules
// are about which files it picks up and in what order, not about the schema,
// so the body only needs to decode.
func moduleYAML(name string) string {
	return "name: " + name + "\n" +
		"displayName: " + name + "\n" +
		"description: A Module used by the publisher tests.\n" +
		"entities:\n" +
		"  - type: helmChart\n" +
		"    helmChart:\n" +
		"      productId: app.example:" + name + "\n" +
		"      entityName: " + name + "\n" +
		"      k8sNamespace: example\n"
}

func writeModules(t *testing.T, files map[string]string) string {
	t.Helper()
	directory := t.TempDir()
	for name, body := range files {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func TestReadDefinitionsSelectsAndOrdersModuleFiles(t *testing.T) {
	t.Parallel()

	t.Run("publishes in a deterministic order regardless of directory order", func(t *testing.T) {
		t.Parallel()
		// The Hub receives one request containing every definition, so the order
		// inside it decides the order releases are recorded. Leaving it to the
		// filesystem would make the same directory publish differently on
		// different machines.
		directory := writeModules(t, map[string]string{
			"zulu.yaml":  moduleYAML("zulu"),
			"alpha.yaml": moduleYAML("alpha"),
			"mike.yaml":  moduleYAML("mike"),
		})
		definitions, err := readDefinitions(directory)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, definition := range definitions {
			names = append(names, definition.Name)
		}
		if strings.Join(names, ",") != "alpha,mike,zulu" {
			t.Errorf("order = %v, want alpha,mike,zulu", names)
		}
	})

	t.Run("takes .yaml and .yml in any case and ignores everything else", func(t *testing.T) {
		t.Parallel()
		directory := writeModules(t, map[string]string{
			"a.yaml":     moduleYAML("a"),
			"b.yml":      moduleYAML("b"),
			"c.YAML":     moduleYAML("c"),
			"README.md":  "# not a Module",
			"d.yaml.bak": moduleYAML("d"),
			"notes.txt":  "scratch",
			"sub/e.yaml": moduleYAML("e"), // a nested directory is not descended into
			// A directory whose own name ends in .yaml clears the extension
			// filter. Only the IsDir check keeps it out; without it the
			// publisher opens a directory and fails the whole publish.
			"bundle.yaml/f.yaml": moduleYAML("f"),
		})
		definitions, err := readDefinitions(directory)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, definition := range definitions {
			names = append(names, definition.Name)
		}
		// "e" is absent: readDefinitions skips directories rather than walking
		// them, so a nested Module is silently not published.
		if strings.Join(names, ",") != "a,b,c" {
			t.Errorf("names = %v, want a,b,c", names)
		}
	})

	t.Run("refuses a filename that disagrees with the Module name", func(t *testing.T) {
		t.Parallel()
		// The filename is what an operator greps for; if it can drift from the
		// name the Hub records, the two stop being the same Module.
		directory := writeModules(t, map[string]string{"payments.yaml": moduleYAML("billing")})
		_, err := readDefinitions(directory)
		if err == nil {
			t.Fatal("a mismatched filename was accepted")
		}
		if !strings.Contains(err.Error(), "billing") || !strings.Contains(err.Error(), "payments.yaml") {
			t.Errorf("error %q should name both the file and the Module", err)
		}
	})

	t.Run("refuses a directory with no Module files", func(t *testing.T) {
		t.Parallel()
		// Publishing an empty set would report success having released nothing,
		// which reads identically to a successful publish.
		for _, contents := range []map[string]string{
			{},
			{"README.md": "# no Modules here"},
		} {
			_, err := readDefinitions(writeModules(t, contents))
			if err == nil {
				t.Errorf("an empty publish set was accepted for %v", contents)
			}
		}
	})

	t.Run("names the file that failed to decode", func(t *testing.T) {
		t.Parallel()
		directory := writeModules(t, map[string]string{
			"good.yaml": moduleYAML("good"),
			"bad.yaml":  "name: [this is not a string\n",
		})
		_, err := readDefinitions(directory)
		if err == nil {
			t.Fatal("an undecodable Module was accepted")
		}
		if !strings.Contains(err.Error(), "bad.yaml") {
			t.Errorf("error %q does not say which file is broken", err)
		}
	})

	t.Run("reports a missing directory rather than publishing nothing", func(t *testing.T) {
		t.Parallel()
		_, err := readDefinitions(filepath.Join(t.TempDir(), "absent"))
		if err == nil {
			t.Fatal("a missing directory was accepted")
		}
		if !strings.Contains(err.Error(), "read Module directory") {
			t.Errorf("error = %q", err)
		}
	})

	t.Run("reads the Modules shipped in the repository", func(t *testing.T) {
		t.Parallel()
		// The default --directory. If the examples stop satisfying the
		// publisher's own rules, the documented first run fails.
		definitions, err := readDefinitions(filepath.Join("..", "..", "examples", "modules"))
		if err != nil {
			t.Fatalf("the shipped examples do not publish: %v", err)
		}
		if len(definitions) == 0 {
			t.Fatal("no shipped example Modules")
		}
	})
}

// The Hub URL was previously concatenated without being parsed at all, so a
// typo became an opaque failure from net/http rather than a stated one, and a
// base path was accepted here while the Spoke agent and operator adapter both
// refuse it.
func TestReleasesURLAgreesWithTheOtherHubClients(t *testing.T) {
	t.Parallel()

	t.Run("appends the publication path to a bare scheme and host", func(t *testing.T) {
		t.Parallel()
		for _, test := range []struct{ given, want string }{
			{"https://hub.example", "https://hub.example/v1/modules/releases"},
			{"https://hub.example/", "https://hub.example/v1/modules/releases"},
			{"http://127.0.0.1:18082", "http://127.0.0.1:18082/v1/modules/releases"},
			{"  https://hub.example  ", "https://hub.example/v1/modules/releases"},
		} {
			got, err := releasesURL(test.given)
			if err != nil {
				t.Errorf("%q: %v", test.given, err)
				continue
			}
			if got != test.want {
				t.Errorf("%q became %q, want %q", test.given, got, test.want)
			}
		}
	})

	t.Run("refuses what the agent and the operator adapter refuse", func(t *testing.T) {
		t.Parallel()
		// Not because joining would misroute -- it would not -- but because a
		// Hub reachable only under a base path cannot run a Spoke at all, and
		// finding that out one component at a time is the bad outcome.
		for _, given := range []string{
			"",
			"   ",
			"https://hub.example/platform",
			"https://hub.example/platform/",
			"https://hub.example?tenant=a",
			"https://hub.example#fragment",
			"ftp://hub.example",
			"hub.example",
			"/v1/modules/releases",
		} {
			if got, err := releasesURL(given); err == nil {
				t.Errorf("%q was accepted as %q", given, got)
			}
		}
	})

	t.Run("says which part of the URL is wrong", func(t *testing.T) {
		t.Parallel()
		for _, test := range []struct{ given, want string }{
			{"", "required"},
			{"ftp://hub.example", "http or https"},
			{"http://", "must name a host"},
			{"https://hub.example/platform", "without a path"},
		} {
			_, err := releasesURL(test.given)
			if err == nil {
				t.Errorf("%q was accepted", test.given)
				continue
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("%q gave %q, which does not mention %q", test.given, err, test.want)
			}
		}
	})
}
