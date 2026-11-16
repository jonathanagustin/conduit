package composition

import (
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/repoclone"
)

func hubPlanningClone(_ string) planner.CloneFunc {
	return repoclone.Clone
}
