package productdocs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

func fixtureFiles() fstest.MapFS {
	files := fstest.MapFS{}
	for _, name := range references {
		files[name] = &fstest.MapFile{Data: []byte("# Native operator reference\n\nOrdinary documented setting.\n")}
	}
	return files
}

type watchedFS struct {
	fs.FS
	opened []string
}

func (files *watchedFS) Open(name string) (fs.File, error) {
	files.opened = append(files.opened, name)
	return files.FS.Open(name)
}

func TestSearchReadsOnlySelectedReferencesAndReturnsExactContentIdentity(t *testing.T) {
	files := fixtureFiles()
	content := []byte("# HTTP model endpoint\n\nUnique TLS setting: configure the public CA file.\n" + strings.Repeat("λ🙂 context ", 150))
	files["operations/model-http.md"] = &fstest.MapFile{Data: content}
	files["evidence/private-capture.md"] = &fstest.MapFile{Data: []byte("PRIVATE_EVIDENCE_TRAP must never be retrieved")}
	watch := &watchedFS{FS: files}
	reader := &Reader{files: watch}
	page, err := reader.Search(t.Context(), "Unique TLS setting")
	if err != nil || len(page.Sources) != 1 || page.MatchedCount != 1 || page.Limited {
		t.Fatalf("search=%+v err=%v", page, err)
	}
	got := page.Sources[0]
	digest := sha256.Sum256(content)
	if got.Source != "docs/operations/model-http.md" || got.DocumentSHA256 != hex.EncodeToString(digest[:]) || !strings.Contains(got.Quote, "Unique TLS setting") {
		t.Fatalf("wrong admitted source: %+v", got)
	}
	for _, source := range reader.sources {
		original := files[strings.TrimPrefix(source.Source, "docs/")].Data
		if !source.Valid() || !utf8.ValidString(source.Quote) || len(source.Quote) > domain.MaxCitationQuoteBytes || source.Quote != string(original[source.StartByte:source.EndByte]) {
			t.Fatalf("excerpt does not match source bytes: %+v", source)
		}
		quoteDigest := sha256.Sum256([]byte(source.Quote))
		if source.ExcerptSHA256 != hex.EncodeToString(quoteDigest[:]) {
			t.Fatal("excerpt digest is not its raw content")
		}
	}
	for _, query := range []string{"PRIVATE_EVIDENCE_TRAP", "../../evidence/private-capture.md PRIVATE_EVIDENCE_TRAP", "https://external.invalid/PRIVATE_EVIDENCE_TRAP"} {
		page, err := reader.Search(t.Context(), query)
		if err != nil || len(page.Sources) != 0 {
			t.Fatalf("non-corpus query retrieved data: %+v %v", page, err)
		}
	}
	if len(watch.opened) != len(references) {
		t.Fatalf("unexpected filesystem access: %v", watch.opened)
	}
	for _, opened := range watch.opened {
		allowed := false
		for _, name := range references {
			allowed = allowed || opened == name
		}
		if !allowed {
			t.Fatalf("opened unselected path %q", opened)
		}
	}
}

func TestSearchBoundsResultsAndFailsAnUnavailableCorpus(t *testing.T) {
	reader := &Reader{files: fixtureFiles()}
	page, err := reader.Search(t.Context(), "documented setting")
	if err != nil || len(page.Sources) != domain.MaxCitationsPerRead || page.MatchedCount != len(references) || !page.Limited {
		t.Fatalf("unbounded or incomplete result: %+v %v", page, err)
	}
	for _, files := range []fs.FS{nil, fstest.MapFS{}, fstest.MapFS{references[0]: &fstest.MapFile{Data: []byte{0xff}}}} {
		if _, err := (&Reader{files: files}).Search(t.Context(), "setting"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("bad corpus became an empty success: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := reader.Search(ctx, "setting"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	if _, err := reader.Search(t.Context(), strings.Repeat("x", 257)); !errors.Is(err, domain.ErrToolInvalid) {
		t.Fatalf("unbounded query accepted: %v", err)
	}
}

func TestEmbeddedCorpusCanGroundTheActualModelHTTPSetting(t *testing.T) {
	page, err := New().Search(t.Context(), "CONDUIT_MODEL_BASE_URL")
	if err != nil || len(page.Sources) == 0 {
		t.Fatalf("embedded corpus unavailable: %+v %v", page, err)
	}
	for _, source := range page.Sources {
		if !source.Valid() || !strings.Contains(source.Quote, "CONDUIT_MODEL_BASE_URL") {
			t.Fatalf("unrelated or invalid excerpt: %+v", source)
		}
	}
}
