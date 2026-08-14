// Package apollo is Conduit's bounded integration context for the recovered
// Apollo contract surface.
//
// The generated routes, roles, operations and workflows here are supported by
// pinned public contracts or explicitly scoped, sanitized tenant evidence.
// They establish only the profile named by that evidence. They do not establish
// Apollo's private Hub-to-Agent protocol or make Conduit an unqualified superset
// of Apollo.
//
// Nothing here decides what Platform does with any of it. A role's operations
// are Apollo's own composition; the authority Platform attaches to that role is
// a separate question answered elsewhere, and keeping the two apart is what
// stops a captured vocabulary from quietly becoming a grant.
//
// It does not import Conduit domain or application packages. The generated HTTP
// transport depends only on the standard library and its pinned OpenAPI runtime,
// so native contexts can depend on the contract without importing the legacy
// control-plane facade.
//
// This context owns Apollo vocabulary, conformance rules, and translation into
// narrow Conduit application ports. Conduit's delivery, governance, execution,
// and persistence policy remain native contexts rather than becoming guessed
// Apollo internals.
//
// The files beside this one are generated. Edit the pinned oapi-codegen
// configuration, the role generator, and the evidence they read rather than
// the output.
package apollo
