// Package productdocs searches an explicit embedded native reference corpus.
// Queries never become filesystem paths, network requests or commands.
package productdocs

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	embeddedrefs "github.com/jonathanagustin/conduit/docs"
	"github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

var references = []string{
	"operations/operator-sign-in.md", "operations/ai-usage-and-transcripts.md",
	"operations/model-http.md", "operations/route-authorization.md", "architecture/agent-runs.md",
}

// ErrUnavailable refuses a read whose selected corpus could not be loaded.
var ErrUnavailable = errors.New("native product documentation is unavailable")

// Reader is a concurrency-safe index of bounded, content-identified excerpts.
type Reader struct {
	files   fs.FS
	once    sync.Once
	sources []domain.Citation
	err     error
}

// New searches only the operator references embedded with this binary.
func New() *Reader { return &Reader{files: embeddedrefs.AssistantReferences()} }

func (reader *Reader) load() {
	if reader.files == nil {
		reader.err = ErrUnavailable
		return
	}
	for _, name := range references {
		content, err := fs.ReadFile(reader.files, name)
		if err != nil || len(content) == 0 || len(content) > domain.MaxDocumentationBytes || !utf8.Valid(content) {
			reader.err = ErrUnavailable
			return
		}
		title := strings.TrimSpace(strings.TrimLeft(strings.SplitN(string(content), "\n", 2)[0], "#"))
		if title == "" || len(title) > 256 {
			reader.err = ErrUnavailable
			return
		}
		digest := domain.Digest(content)
		for start := 0; start < len(content); {
			end := min(start+domain.MaxCitationQuoteBytes, len(content))
			for end < len(content) && !utf8.RuneStart(content[end]) {
				end--
			}
			quote := string(content[start:end])
			if strings.TrimSpace(quote) != "" {
				source, err := domain.NewCitation(domain.Citation{Kind: domain.SourceDocumentation, Title: title, Source: "docs/" + name, Quote: quote, DocumentSHA256: digest, FirstLine: 1 + bytes.Count(content[:start], []byte("\n")), LastLine: 1 + bytes.Count(content[:end-1], []byte("\n")), StartByte: start, EndByte: end})
				if err != nil {
					reader.err = ErrUnavailable
					return
				}
				reader.sources = append(reader.sources, source)
			}
			if end == len(content) {
				break
			}
			start = end - 96
			for !utf8.RuneStart(content[start]) {
				start++
			}
		}
	}
}

func terms(query string) ([]string, error) {
	if len(query) > 256 || strings.TrimSpace(query) == "" {
		return nil, domain.ErrToolInvalid
	}
	stop := map[string]bool{"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true, "by": true, "for": true, "from": true, "how": true, "i": true, "in": true, "is": true, "it": true, "of": true, "on": true, "or": true, "the": true, "to": true, "what": true, "with": true}
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' })
	seen := map[string]bool{}
	out := []string{}
	for _, word := range words {
		if stop[word] || seen[word] {
			continue
		}
		if len(word) > 64 || len(out) >= 16 {
			return nil, domain.ErrToolInvalid
		}
		seen[word] = true
		out = append(out, word)
	}
	return out, nil
}

// Search matches every meaningful query term and returns at most one excerpt
// per document and three documents per query. Omitted matches are explicit.
func (reader *Reader) Search(ctx context.Context, query string) (application.DocumentationPage, error) {
	if err := ctx.Err(); err != nil {
		return application.DocumentationPage{}, err
	}
	words, err := terms(query)
	if err != nil {
		return application.DocumentationPage{}, err
	}
	reader.once.Do(reader.load)
	if reader.err != nil {
		return application.DocumentationPage{}, reader.err
	}
	type match struct {
		source domain.Citation
		score  int
	}
	best := map[string]match{}
	for _, source := range reader.sources {
		if err := ctx.Err(); err != nil {
			return application.DocumentationPage{}, err
		}
		body := strings.ToLower(source.Quote)
		score := 0
		for _, word := range words {
			if strings.Contains(body, word) {
				score++
			}
		}
		old, found := best[source.Source]
		if score > 0 && score == len(words) && (!found || score > old.score || (score == old.score && source.StartByte < old.source.StartByte)) {
			best[source.Source] = match{source, score}
		}
	}
	matched := make([]match, 0, len(best))
	for _, hit := range best {
		matched = append(matched, hit)
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].score != matched[j].score {
			return matched[i].score > matched[j].score
		}
		return matched[i].source.Source < matched[j].source.Source
	})
	page := application.DocumentationPage{Sources: []domain.Citation{}, MatchedCount: len(matched), Limited: len(matched) > 3}
	for _, hit := range matched[:min(len(matched), 3)] {
		page.Sources = append(page.Sources, hit.source)
	}
	return page, nil
}
