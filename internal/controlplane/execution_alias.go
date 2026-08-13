package controlplane

import "github.com/jonathanagustin/conduit/internal/execution"

// The lease and its rules now live in internal/execution. The alias keeps this
// package's call sites spelled as they were.
type PlanLease = execution.Lease
