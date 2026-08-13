package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// durationEnvironment and requiredEnvironment end the process rather than
// returning an error, so the fatal paths are exercised by re-running this test
// binary as a child and inspecting how it died. The alternative -- asserting
// only the paths that return -- would leave "an unparseable duration is fatal"
// untested, and that is the property worth having: silently falling back to a
// default means an Agent polls at a cadence nobody configured.
const crashVariable = "PLATFORM_SPOKE_AGENT_TEST_CRASHER"

func TestEnvironmentHelpersReturnOrExit(t *testing.T) {
	if name := os.Getenv(crashVariable); name != "" {
		switch name {
		case "missing-required":
			_ = os.Unsetenv("PLATFORM_TEST_REQUIRED")
			requiredEnvironment("PLATFORM_TEST_REQUIRED")
		case "unparseable-duration":
			t.Setenv("PLATFORM_TEST_DURATION", "soon")
			durationEnvironment("PLATFORM_TEST_DURATION", time.Minute)
		case "zero-duration":
			t.Setenv("PLATFORM_TEST_DURATION", "0s")
			durationEnvironment("PLATFORM_TEST_DURATION", time.Minute)
		case "negative-duration":
			t.Setenv("PLATFORM_TEST_DURATION", "-1s")
			durationEnvironment("PLATFORM_TEST_DURATION", time.Minute)
		}
		return
	}

	t.Run("values that are present are returned", func(t *testing.T) {
		t.Setenv("PLATFORM_TEST_REQUIRED", "present")
		if got := requiredEnvironment("PLATFORM_TEST_REQUIRED"); got != "present" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("an unset duration falls back", func(t *testing.T) {
		t.Setenv("PLATFORM_TEST_DURATION", "")
		if got := durationEnvironment("PLATFORM_TEST_DURATION", 42*time.Second); got != 42*time.Second {
			t.Errorf("got %v, want the fallback", got)
		}
	})

	t.Run("a valid duration overrides the fallback", func(t *testing.T) {
		t.Setenv("PLATFORM_TEST_DURATION", "90s")
		if got := durationEnvironment("PLATFORM_TEST_DURATION", time.Minute); got != 90*time.Second {
			t.Errorf("got %v, want 90s", got)
		}
	})

	// Each of these must end the process. A duration that is zero or negative
	// is as unusable as one that will not parse: an Agent cannot poll every
	// zero seconds, and falling back would hide the misconfiguration.
	for _, name := range []string{
		"missing-required", "unparseable-duration", "zero-duration", "negative-duration",
	} {
		t.Run(name+" is fatal", func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=TestEnvironmentHelpersReturnOrExit")
			command.Env = append(os.Environ(), crashVariable+"="+name)
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("the child exited cleanly; %s did not end the process:\n%s", name, output)
			}
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
				t.Fatalf("child failed as %v, want exit status 1:\n%s", err, output)
			}
			if !strings.Contains(string(output), "PLATFORM_TEST_") {
				t.Errorf("the child died without naming the variable:\n%s", output)
			}
		})
	}
}
