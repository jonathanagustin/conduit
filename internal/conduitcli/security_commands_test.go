package conduitcli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/spf13/cobra"
)

// limitedWriter caps what a helm subprocess can spend memory on, but it sits in
// the middle of exec's copy loop, so how it reports a write matters as much as
// how much it keeps.
func TestLimitedWriterTruncatesWithoutFailingTheWriter(t *testing.T) {
	t.Parallel()

	t.Run("reports the full length so the producer never sees a short write", func(t *testing.T) {
		t.Parallel()
		// io.Copy treats n < len(p) as ErrShortWrite and stops. Returning the
		// number of bytes actually kept would abort a helm run that is doing
		// nothing wrong -- the point of the bound is to stop reading output,
		// not to fail the command producing it.
		writer := &limitedWriter{writer: &bytes.Buffer{}, remaining: 4}
		for _, size := range []int{2, 10, 7} {
			written, err := writer.Write(bytes.Repeat([]byte("x"), size))
			if err != nil {
				t.Fatalf("writing %d bytes: %v", size, err)
			}
			if written != size {
				t.Errorf("Write of %d reported %d; io.Copy reads that as a short write", size, written)
			}
		}
	})

	t.Run("keeps exactly the budget across several writes", func(t *testing.T) {
		t.Parallel()
		var sink bytes.Buffer
		writer := &limitedWriter{writer: &sink, remaining: 10}
		for _, chunk := range []string{"abcd", "efgh", "ijkl", "mnop"} {
			if _, err := writer.Write([]byte(chunk)); err != nil {
				t.Fatal(err)
			}
		}
		// Not "about 10": the boundary is the whole contract, and a partial
		// chunk has to be cut mid-chunk rather than dropped or kept whole.
		if sink.String() != "abcdefghij" {
			t.Errorf("kept %q, want %q", sink.String(), "abcdefghij")
		}
	})

	t.Run("a write landing exactly on the budget is kept whole", func(t *testing.T) {
		t.Parallel()
		var sink bytes.Buffer
		writer := &limitedWriter{writer: &sink, remaining: 5}
		if _, err := writer.Write([]byte("abcde")); err != nil {
			t.Fatal(err)
		}
		if sink.String() != "abcde" {
			t.Errorf("kept %q, want the whole write", sink.String())
		}
		if _, err := writer.Write([]byte("f")); err != nil {
			t.Fatal(err)
		}
		if sink.String() != "abcde" {
			t.Errorf("kept %q after the budget was spent", sink.String())
		}
	})

	t.Run("stops writing once the budget is spent", func(t *testing.T) {
		t.Parallel()
		// Past the bound the underlying writer must not be touched at all,
		// otherwise a hostile subprocess still costs one allocation per write.
		counting := &countingWriter{}
		writer := &limitedWriter{writer: counting, remaining: 3}
		for range 5 {
			if _, err := writer.Write([]byte("abc")); err != nil {
				t.Fatal(err)
			}
		}
		if counting.calls != 1 {
			t.Errorf("underlying writer called %d times, want 1", counting.calls)
		}
	})

	t.Run("surfaces the underlying writer's error", func(t *testing.T) {
		t.Parallel()
		writer := &limitedWriter{writer: failingWriter{}, remaining: maxDocumentBytes}
		written, err := writer.Write([]byte("abc"))
		if err == nil {
			t.Fatal("a failing sink was reported as a successful write")
		}
		// Still the full length: the count and the error are independent, and
		// callers key off the error.
		if written != 3 {
			t.Errorf("written = %d, want 3", written)
		}
	})

	t.Run("a zero budget discards everything", func(t *testing.T) {
		t.Parallel()
		counting := &countingWriter{}
		writer := &limitedWriter{writer: counting, remaining: 0}
		written, err := writer.Write([]byte("abc"))
		if err != nil || written != 3 {
			t.Fatalf("written = %d, err = %v", written, err)
		}
		if counting.calls != 0 {
			t.Errorf("underlying writer called %d times with no budget", counting.calls)
		}
	})
}

// The bound the security commands actually run with. A helm template of a large
// chart is well under this; a runaway one is well over.
func TestLimitedWriterUsesTheDocumentBound(t *testing.T) {
	t.Parallel()
	var sink bytes.Buffer
	writer := &limitedWriter{writer: &sink, remaining: maxDocumentBytes}
	if _, err := writer.Write(bytes.Repeat([]byte("y"), maxDocumentBytes+4096)); err != nil {
		t.Fatal(err)
	}
	if sink.Len() != maxDocumentBytes {
		t.Errorf("kept %d bytes, want %d", sink.Len(), maxDocumentBytes)
	}
	if strings.Trim(sink.String(), "y") != "" {
		t.Error("kept bytes were altered")
	}
}

type countingWriter struct{ calls int }

func (writer *countingWriter) Write(value []byte) (int, error) {
	writer.calls++
	return len(value), nil
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("sink refused the write") }

// runBoundedCommand is how every helm invocation in the security commands is
// made. What it does with a failure decides whether an operator sees helm's
// complaint or just an exit code.
func TestRunBoundedCommandReportsWhatTheToolSaid(t *testing.T) {
	t.Parallel()

	command := &cobra.Command{}
	command.SetContext(context.Background())

	t.Run("returns stdout on success", func(t *testing.T) {
		t.Parallel()
		output, err := runBoundedCommand(command, "sh", "-c", "printf 'rendered'")
		if err != nil {
			t.Fatal(err)
		}
		if output != "rendered" {
			t.Errorf("output = %q", output)
		}
	})

	t.Run("quotes stderr rather than the exit status", func(t *testing.T) {
		t.Parallel()
		// "exit status 1" tells an operator nothing. helm's own message is the
		// only thing that says which chart or value was wrong.
		_, err := runBoundedCommand(command, "sh", "-c", "printf 'chart is missing a version' >&2; exit 1")
		if err == nil {
			t.Fatal("a failing command was reported as success")
		}
		if !strings.Contains(err.Error(), "chart is missing a version") {
			t.Errorf("error %q drops what the tool said", err)
		}
		// Named here rather than in the missing-executable case below: there the
		// exec error already carries the name, so the prefix is redundant and a
		// test against it proves nothing.
		if !strings.Contains(err.Error(), "sh ") {
			t.Errorf("error %q does not say which tool failed", err)
		}
	})

	t.Run("falls back to the exit error when the tool said nothing", func(t *testing.T) {
		t.Parallel()
		// Silent failures still have to say something, or the message reads as
		// "sh validation failed: " and the exit code is lost entirely.
		_, err := runBoundedCommand(command, "sh", "-c", "exit 3")
		if err == nil {
			t.Fatal("a failing command was reported as success")
		}
		if !strings.Contains(err.Error(), "exit status 3") {
			t.Errorf("error %q drops the exit status, leaving no reason at all", err)
		}
	})

	t.Run("names the executable that failed", func(t *testing.T) {
		t.Parallel()
		_, err := runBoundedCommand(command, "definitely-not-on-path-9f3a")
		if err == nil {
			t.Fatal("a missing executable was reported as success")
		}
		if !strings.Contains(err.Error(), "definitely-not-on-path-9f3a") {
			t.Errorf("error %q does not say which tool is missing", err)
		}
	})
}

func TestVulnerabilityReportCSVRendersSummaryAndDetail(t *testing.T) {
	t.Parallel()
	result := controlplane.ArtifactScanResult{
		VulnerabilityAssessment: &controlplane.VulnerabilityAssessment{
			Status: "COMPLETE", Outcome: "PASS", ActiveVulnerabilities: 2, SuppressedFindings: 1,
			ActiveFindings: []controlplane.VulnerabilityFinding{
				{ID: "CVE-2026-1", Severity: "HIGH", CVSS: 7.5},
				{ID: "CVE-2026-2", Severity: "LOW"},
			},
		},
		MalwareAssessment: &controlplane.MalwareAssessment{Status: "COMPLETE", Outcome: "PASS", Findings: 0},
	}

	summary, err := vulnerabilityReportCSV{product: "p", version: "1.0", result: result}.MarshalCSV()
	if err != nil {
		t.Fatal(err)
	}
	// header + one vulnerability summary row + one malware summary row.
	if len(summary) != 3 {
		t.Fatalf("summary rows = %d: %v", len(summary), summary)
	}
	if summary[1][2] != "vulnerability" || summary[1][5] != "2" || summary[1][6] != "1" {
		t.Errorf("vulnerability summary row = %v", summary[1])
	}
	if summary[2][2] != "malware" || summary[2][5] != "0" {
		t.Errorf("malware summary row = %v", summary[2])
	}

	detail, err := vulnerabilityReportCSV{product: "p", version: "1.0", details: true, result: result}.MarshalCSV()
	if err != nil {
		t.Fatal(err)
	}
	// header + one row per active finding.
	if len(detail) != 3 {
		t.Fatalf("detail rows = %d: %v", len(detail), detail)
	}
	if detail[1][3] != "CVE-2026-1" || detail[1][4] != "HIGH" || detail[1][5] != "7.5" {
		t.Errorf("first finding row = %v", detail[1])
	}
	// A finding with no CVSS renders an explicit 0, not a blank a reader could
	// mistake for a missing column.
	if detail[2][5] != "0" {
		t.Errorf("zero-CVSS finding rendered cvss = %q, want \"0\"", detail[2][5])
	}
}
