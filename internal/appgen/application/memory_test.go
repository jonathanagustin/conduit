package application_test

import (
	"testing"

	"github.com/jonathanagustin/conduit/internal/appgen/adapters/storetest"
	"github.com/jonathanagustin/conduit/internal/appgen/application"
)

func TestMemoryMeetsTheStoreContract(t *testing.T) {
	storetest.Run(t, func(*testing.T) application.Store { return &application.Memory{} })
}
