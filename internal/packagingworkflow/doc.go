// Package packagingworkflow is the localhost proof that Temporal can orchestrate
// application packaging without becoming a second approval database or a second
// deployment lease loop.
//
// Temporal owns the packaging run: assessment, proposal, validation, the wait
// for review, and a crash-safe handoff attempt. Conduit governance owns
// approval. The delivery kernel owns Plan execution, leases, rollout and
// observed state. A Signal or Update is a correlation the workflow checks
// against a governance record; it is not itself permission to deploy.
//
// This module is not a production Temporal deployment and it is not an offline-bundle
// claim. The development server it runs against is the upstream CLI's
// single-process server, which upstream documents as not production-grade.
package packagingworkflow
