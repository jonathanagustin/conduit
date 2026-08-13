// Package controlplane is the delivery Hub: Platform's model of what should be
// running, and the authority on whether a change to it is allowed.
//
// It owns the domain -- Products, Releases, Environments, Installations,
// Modules, Plans and the Bundles that carry them -- along with the validation
// those objects must satisfy, the lifecycle that moves an Installation between
// states, and the governance that decides who may move it. It also serves the
// Apollo-compatible public API, whose routes are generated from a pinned
// snapshot rather than written here.
//
// Nothing in this package reaches into a cluster. Applying a decision is the
// Spoke's job; this package decides, records, and answers questions about what
// it decided.
package controlplane
