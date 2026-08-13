package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// run is the whole binary: it reads one scan request from stdin, bounds it,
// and refuses anything ambiguous before it ever invokes Grype. Not parallel --
// it reads process environment.
func TestRunRefusesMalformedRequestsBeforeScanning(t *testing.T) {
	// A valid-shaped request is enough to get past decoding; these cases all
	// fail earlier than the scan, so Grype is never invoked.
	const validRequest = `{"apiVersion":"platform.delivery/v1alpha2","kind":"ArtifactScan",` +
		`"product":"com.example:api","version":"1.2.3",` +
		`"chart":{"repository":"registry.example/charts/api","tag":"1.2.3","digest":"sha256:` +
		`aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"images":[]}`

	for name, test := range map[string]struct{ input, want string }{
		"a request past the size bound": {
			input: `{"apiVersion":"` + strings.Repeat("x", maximumRequestSize) + `"}`,
			want:  "exceeds 1 MiB",
		},
		// An unknown field is how a request written against a newer schema
		// arrives: acting on the parts that happen to parse would scan
		// something other than what was asked for.
		"an unknown field": {
			input: `{"apiVersion":"platform.delivery/v1alpha2","kind":"ArtifactScan","unknown":1}`,
			want:  "decode artifact scan request",
		},
		"a second JSON value": {
			input: validRequest + validRequest,
			want:  "exactly one JSON value",
		},
		"not JSON at all": {
			input: `nonsense`,
			want:  "decode artifact scan request",
		},
	} {
		var output bytes.Buffer
		err := run(context.Background(), strings.NewReader(test.input), &output)
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error %q does not mention %q", name, err, test.want)
		}
		if output.Len() != 0 {
			t.Errorf("%s: wrote %q to stdout despite failing", name, output.String())
		}
	}
}

// The timeout bounds a Grype invocation. Outside the accepted range it must be
// refused rather than clamped, because a scan silently running with a
// different timeout than configured is the thing worth knowing about.
func TestRunBoundsTheScanTimeout(t *testing.T) {
	t.Setenv("GRYPE_BIN", "/nonexistent/grype")
	t.Setenv("GRYPE_CONFIG_FILE", "/nonexistent/grype.yaml")

	for name, test := range map[string]struct {
		value    string
		rejected bool
	}{
		"below the floor":  {value: "999ms", rejected: true},
		"at the floor":     {value: "1s"},
		"at the ceiling":   {value: "30m"},
		"past the ceiling": {value: "30m1s", rejected: true},
		"unparseable":      {value: "soon", rejected: true},
		"negative":         {value: "-5s", rejected: true},
	} {
		t.Setenv("GRYPE_SCAN_TIMEOUT", test.value)
		var output bytes.Buffer
		err := run(context.Background(), strings.NewReader(`{"apiVersion":"x"}`), &output)
		mentionsTimeout := err != nil && strings.Contains(err.Error(), "GRYPE_SCAN_TIMEOUT")
		if mentionsTimeout != test.rejected {
			t.Errorf("%s (%q): timeout rejected=%v, want %v (err=%v)",
				name, test.value, mentionsTimeout, test.rejected, err)
		}
	}
}
