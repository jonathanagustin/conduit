package application_test

import (
	"testing"

	"github.com/jonathanagustin/conduit/internal/intelligence/adapters/casetest"
	"github.com/jonathanagustin/conduit/internal/intelligence/application"
)

func TestMemoryCasesConformToTheCaseStorePort(t *testing.T) {
	casetest.Run(t, func(*testing.T) (func() application.CaseStore, bool) {
		store := application.NewMemoryCases()
		return func() application.CaseStore { return store }, false
	})
}
