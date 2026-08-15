// Package gitless is the bounded context for Gitless GitOps: deploying from
// exact, signed bytes in a registry, with no Git credential anywhere near a
// cluster.
//
// It has no code of its own. The layers are:
//
//	domain/       what may be deployed and on whose authority
//	ports/        what the use case needs: resolve, verify, converge, record
//	application/  the order those happen in, and what stops when one fails
//	adapters/fluxconverge/     convergence delegated to Flux
//	adapters/conduitconverge/  convergence owned by Conduit's own reconciler
//
// The two convergers are the point. Conduit can realise Gitless GitOps either
// way -- ADR 0001 chose Flux for the runtime, ADR 0003 kept Conduit's own
// reconciler for tenant delivery -- and expressing both behind one port means
// the choice is a deployment decision rather than a security one. Admission
// lives in the domain precisely so neither adapter can widen what the other
// would refuse, and equivalence_test.go holds them to it.
package gitless
