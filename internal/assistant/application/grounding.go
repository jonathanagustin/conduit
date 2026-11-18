package application

import (
	"encoding/json"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

// ontologySources uses only summaries already returned by the authorized
// catalog port. It never re-reads the repository or broadens the actor's scope.
func (service Service) ontologySources(result Result) ([]domain.Citation, bool) {
	sources := []domain.Citation{}
	limited := len(result.Entities) > domain.MaxCitationsPerRead || result.MatchedCount > len(result.Entities)
	for _, hit := range result.Entities[:min(len(result.Entities), domain.MaxCitationsPerRead)] {
		quote, err := json.Marshal(hit)
		if err != nil || hit.ID == "" || hit.Type == "" {
			limited = true
			continue
		}
		source, err := domain.NewCitation(domain.Citation{Kind: domain.SourceOntology, Title: hit.Type + " " + hit.Label, Source: hit.Type + ":" + hit.ID, Quote: string(quote), RetrievedAt: service.stamp(), AuditEventID: result.AuditEventID})
		if err != nil {
			limited = true
			continue
		}
		sources = append(sources, source)
	}
	return sources, limited
}
