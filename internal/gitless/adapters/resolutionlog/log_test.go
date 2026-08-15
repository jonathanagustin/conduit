package resolutionlog_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/resolutionlog"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

var (
	digestA = "sha256:aa11" + strings.Repeat("0", 60)
	digestB = "sha256:bb22" + strings.Repeat("0", 60)
)

func resolution(superseded string) domain.Resolution {
	return domain.Resolution{
		Environment: "prod",
		Selector: domain.Selector{
			Kind: domain.ByChannel, Repository: "ghcr.io/example/platform-runtime", Channel: "latest-stable",
		},
		Pin:        domain.Pin{Repository: "ghcr.io/example/platform-runtime", Digest: digestA},
		Publisher:  "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v1.2.3",
		Superseded: superseded,
	}
}

func read(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line is not JSON: %q", line)
		}
		records = append(records, record)
	}
	return records
}

func TestARecordSaysWhatTheSelectorMeantAndWhoSignedIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "resolutions.jsonl")
	log, err := resolutionlog.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := log.Record(t.Context(), resolution("")); err != nil {
		t.Fatalf("record: %v", err)
	}
	records := read(t, path)
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}
	record := records[0]
	if record["digest"] != digestA || record["environment"] != "prod" {
		t.Errorf("record: %v", record)
	}
	// The channel has to survive, or "we deployed what the channel said" is
	// uncheckable afterwards.
	if !strings.Contains(record["selector"].(string), "latest-stable") {
		t.Errorf("the record lost the selector: %v", record["selector"])
	}
	if record["moved"] != false {
		t.Errorf("a first resolution was recorded as a move")
	}
	if !strings.Contains(record["publisher"].(string), "platform-images.yml") {
		t.Errorf("the record lost the publisher: %v", record["publisher"])
	}
}

func TestAMoveIsRecordedAsOneWithWhatItReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolutions.jsonl")
	log, _ := resolutionlog.Open(path)
	if err := log.Record(t.Context(), resolution(digestB)); err != nil {
		t.Fatalf("record: %v", err)
	}
	record := read(t, path)[0]
	if record["moved"] != true || record["superseded"] != digestB {
		t.Fatalf("the move was not recorded: %v", record)
	}
}

// Append-only: an earlier record must still be there afterwards, because the
// log's purpose is answering what was deployed and when.
func TestRecordsAccumulateRatherThanReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolutions.jsonl")
	log, _ := resolutionlog.Open(path)
	for range 3 {
		if err := log.Record(t.Context(), resolution(digestB)); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if records := read(t, path); len(records) != 3 {
		t.Fatalf("expected three records, got %d", len(records))
	}
}

// Reopening must not truncate. A Hub restart that erased the deployment history
// would defeat the point of recording it.
func TestReopeningDoesNotTruncate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolutions.jsonl")
	first, _ := resolutionlog.Open(path)
	_ = first.Record(t.Context(), resolution(""))
	second, err := resolutionlog.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = second.Record(t.Context(), resolution(digestB))
	if records := read(t, path); len(records) != 2 {
		t.Fatalf("reopening lost records: %d remain", len(records))
	}
}

func TestAnInvalidResolutionIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolutions.jsonl")
	log, _ := resolutionlog.Open(path)
	broken := resolution("")
	broken.Environment = ""
	if err := log.Record(t.Context(), broken); err == nil {
		t.Fatal("an invalid resolution was recorded")
	}
	if raw, _ := os.ReadFile(path); len(raw) != 0 {
		t.Fatalf("an invalid resolution reached the file: %q", raw)
	}
}

func TestAnUnwritableLocationFailsAtOpen(t *testing.T) {
	if _, err := resolutionlog.Open("/proc/definitely/not/writable/resolutions.jsonl"); err == nil {
		t.Fatal("an unwritable log opened successfully")
	}
}
