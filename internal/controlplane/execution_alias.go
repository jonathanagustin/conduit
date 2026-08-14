package controlplane

import "github.com/jonathanagustin/conduit/internal/execution"

// PlanLease keeps legacy control-plane call sites stable while lease rules live
// in internal/execution.
type PlanLease = execution.Lease
