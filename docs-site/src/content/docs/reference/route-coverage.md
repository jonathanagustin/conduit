---
title: Hub route coverage
description: The shipped route and authorization register, including explicit native contract gaps.
---

Generated from `internal/hub/composition/authorization.go`; this is a route and access-decision inventory, not a substitute for request/response schemas. Compatibility adapter paths are listed by their shipped names; this does not claim a private upstream protocol. The [native API reference](/conduit/reference/native-api/) renders the available schemas. [#391](https://github.com/jonathanagustin/conduit/issues/391) owns the uncontracted remainder.

269 registered patterns; 28 match a native OpenAPI operation. These counts are generated, and include method-not-allowed fallback patterns. Protobuf methods are documented separately and are not counted as additional REST schemas.

| Registered route | Access decision | Native OpenAPI contract |
| --- | --- | --- |
| `/v1/notifications/{path...}` | `public` | No native schema |
| `DELETE /v1/assistant/threads/{threadID}` | `operator-self` | No native schema |
| `DELETE /v1/debug-session-requests/{requestID}` | `operator-grant` | No native schema |
| `DELETE /v1/dev-workspaces/{workspaceID}` | `operator-grant` | No native schema |
| `DELETE /v1/environments/{environmentID}/agent-revocations/{revocationID}` | `operator-grant` | No native schema |
| `DELETE /v1/export-pipelines/{pipelineID}` | `operator-grant` | No native schema |
| `DELETE /v1/labels/{labelID}` | `operator-grant` | No native schema |
| `DELETE /v1/notifications/contacts` | `operator-self` | No native schema |
| `DELETE /v1/suppressions/{suppressionID}` | `operator-grant` | No native schema |
| `GET /api/v0/environments` | `operator-grant` | No native schema |
| `GET /api/v0/environments/{environmentRid}` | `operator-grant` | No native schema |
| `GET /api/v0/environments/{environmentRid}/installations` | `operator-grant` | No native schema |
| `GET /api/v0/environments/{environmentRid}/installations/{installationRid}` | `operator-grant` | No native schema |
| `GET /api/v0/namespaces` | `operator-grant` | No native schema |
| `GET /api/v0/products` | `operator-grant` | No native schema |
| `GET /api/v0/products/{productId}` | `operator-grant` | No native schema |
| `GET /api/v0/products/{productId}/releases` | `operator-grant` | No native schema |
| `GET /api/v0/products/{productId}/releases/{productVersion}` | `operator-grant` | No native schema |
| `GET /api/v2/ontologies` | `operator-filtered` | [docs/operations/ontology-api.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/ontology-api.openapi.yaml) |
| `GET /api/v2/ontologies/{ontology}` | `operator-filtered` | [docs/operations/ontology-api.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/ontology-api.openapi.yaml) |
| `GET /api/v2/ontologies/{ontology}/actionTypes` | `operator-filtered` | [docs/operations/ontology-api.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/ontology-api.openapi.yaml) |
| `GET /api/v2/ontologies/{ontology}/actionTypes/{actionType}` | `operator-filtered` | [docs/operations/ontology-api.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/ontology-api.openapi.yaml) |
| `GET /api/v2/ontologies/{ontology}/fullMetadata` | `operator-filtered` | [docs/operations/ontology-api.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/ontology-api.openapi.yaml) |
| `GET /api/v2/ontologies/{ontology}/interfaceTypes` | `operator-filtered` | [docs/operations/ontology-api.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/ontology-api.openapi.yaml) |
| `GET /api/v2/ontologies/{ontology}/interfaceTypes/{interfaceType}` | `operator-filtered` | [docs/operations/ontology-api.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/ontology-api.openapi.yaml) |
| `GET /api/v2/ontologies/{ontology}/objectTypes` | `operator-filtered` | [docs/operations/ontology-api.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/ontology-api.openapi.yaml) |
| `GET /api/v2/ontologies/{ontology}/objectTypes/{objectType}` | `operator-filtered` | [docs/operations/ontology-api.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/ontology-api.openapi.yaml) |
| `GET /api/v2/ontologies/{ontology}/objectTypes/{objectType}/fullMetadata` | `operator-filtered` | [docs/operations/ontology-api.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/ontology-api.openapi.yaml) |
| `GET /console` | `public` | No native schema |
| `GET /console/` | `public` | No native schema |
| `GET /health/live` | `public` | No native schema |
| `GET /health/ready` | `public` | No native schema |
| `GET /v1/admin/ai/transcripts` | `operator-grant` | No native schema |
| `GET /v1/admin/ai/transcripts/{runID}` | `operator-grant` | No native schema |
| `GET /v1/admin/ai/usage` | `operator-grant` | No native schema |
| `GET /v1/admin/status` | `operator-grant` | [api/admin-status.json](https://github.com/jonathanagustin/conduit/blob/main/api/admin-status.json) |
| `GET /v1/ai/usage` | `operator-self` | No native schema |
| `GET /v1/apollo/graphql/schema` | `public` | No native schema |
| `GET /v1/app-generations` | `operator-self` | No native schema |
| `GET /v1/app-generations/{id}` | `operator-self` | No native schema |
| `GET /v1/approval-policies` | `operator-any` | No native schema |
| `GET /v1/artifact-registries` | `operator-filtered` | No native schema |
| `GET /v1/artifacts/blobs/{digest}` | `operator-grant` | No native schema |
| `GET /v1/assistant/threads` | `operator-self` | No native schema |
| `GET /v1/assistant/threads/{threadID}` | `operator-self` | No native schema |
| `GET /v1/auth/callback` | `public` | No native schema |
| `GET /v1/auth/login` | `public` | No native schema |
| `GET /v1/authorization-audit` | `operator-grant` | No native schema |
| `GET /v1/blackouts` | `operator-grant` | No native schema |
| `GET /v1/bundle-imports` | `operator-grant` | No native schema |
| `GET /v1/bundles` | `operator-grant` | No native schema |
| `GET /v1/bundles/signing-key` | `public` | No native schema |
| `GET /v1/bundles/{bundleID}` | `operator-grant` | No native schema |
| `GET /v1/catalog-retirements` | `operator-grant` | No native schema |
| `GET /v1/change-requests` | `operator-filtered` | No native schema |
| `GET /v1/change-requests/{changeRequestID}` | `operator-filtered` | No native schema |
| `GET /v1/change-requests/{requestID}/comments` | `operator-grant` | No native schema |
| `GET /v1/channel-subscriptions` | `operator-filtered` | No native schema |
| `GET /v1/debug-session-requests` | `operator-filtered` | No native schema |
| `GET /v1/debug-session-requests/{requestID}` | `operator-grant` | No native schema |
| `GET /v1/debug-session-requests/{requestID}/history` | `operator-grant` | No native schema |
| `GET /v1/dev-workspaces` | `operator-filtered` | No native schema |
| `GET /v1/dev-workspaces/{workspaceID}` | `operator-filtered` | No native schema |
| `GET /v1/dev-workspaces/{workspaceID}/acp/sessions` | `operator-filtered` | No native schema |
| `GET /v1/dev-workspaces/{workspaceID}/diff` | `operator-filtered` | No native schema |
| `GET /v1/dev-workspaces/{workspaceID}/download` | `operator-filtered` | No native schema |
| `GET /v1/dev-workspaces/{workspaceID}/file` | `operator-filtered` | No native schema |
| `GET /v1/dev-workspaces/{workspaceID}/history` | `operator-filtered` | No native schema |
| `GET /v1/dev-workspaces/{workspaceID}/logs` | `operator-filtered` | No native schema |
| `GET /v1/dev-workspaces/{workspaceID}/runtime` | `operator-filtered` | No native schema |
| `GET /v1/dev-workspaces/{workspaceID}/tree` | `operator-filtered` | No native schema |
| `GET /v1/environments` | `operator-filtered` | No native schema |
| `GET /v1/environments/{environmentID}` | `operator-grant` | No native schema |
| `GET /v1/environments/{environmentID}/agent-credentials` | `operator-grant` | No native schema |
| `GET /v1/environments/{environmentID}/agent-revocations` | `operator-grant` | No native schema |
| `GET /v1/environments/{environmentID}/commands` | `operator-grant` | No native schema |
| `GET /v1/environments/{environmentID}/entities` | `operator-grant` | No native schema |
| `GET /v1/environments/{environmentID}/health-targets` | `agent` | No native schema |
| `GET /v1/environments/{environmentID}/module-installations` | `operator-grant` | No native schema |
| `GET /v1/environments/{environmentID}/profile` | `operator-grant` | No native schema |
| `GET /v1/environments/{environmentID}/profile/revisions` | `operator-grant` | No native schema |
| `GET /v1/environments/{environmentID}/reported-state` | `operator-grant` | No native schema |
| `GET /v1/environments/{environmentID}/suppressions` | `operator-grant` | No native schema |
| `GET /v1/environments/{environmentID}/target-hub-versions` | `operator-grant` | No native schema |
| `GET /v1/environments/{environmentID}/timeline` | `operator-grant` | No native schema |
| `GET /v1/events` | `operator-grant` | No native schema |
| `GET /v1/export-pipelines` | `operator-grant` | No native schema |
| `GET /v1/export-pipelines/{pipelineID}/schedule-runs` | `operator-grant` | No native schema |
| `GET /v1/fleet/installations` | `operator-filtered` | No native schema |
| `GET /v1/fleet/upgrades` | `operator-filtered` | No native schema |
| `GET /v1/intake-attachments` | `operator-self` | No native schema |
| `GET /v1/intake-attachments/{digest}` | `operator-self` | No native schema |
| `GET /v1/intelligence/evals` | `operator-any` | No native schema |
| `GET /v1/intelligence/events` | `operator-any` | No native schema |
| `GET /v1/intelligence/logic` | `operator-any` | No native schema |
| `GET /v1/labels` | `operator-grant` | No native schema |
| `GET /v1/labels/{labelID}` | `operator-grant` | No native schema |
| `GET /v1/labels/{labelID}/revisions` | `operator-grant` | No native schema |
| `GET /v1/module-installations/{installationID}/update` | `operator-grant` | No native schema |
| `GET /v1/modules` | `operator-grant` | No native schema |
| `GET /v1/modules/{moduleName}/releases` | `operator-grant` | No native schema |
| `GET /v1/modules/{module}/auto-update-policy` | `operator-grant` | No native schema |
| `GET /v1/notifications/contacts` | `operator-self` | No native schema |
| `GET /v1/notifications/events` | `operator-grant` | No native schema |
| `GET /v1/notifications/inbox` | `operator-self` | No native schema |
| `GET /v1/notifications/preferences` | `operator-self` | No native schema |
| `GET /v1/operator-identity` | `public` | No native schema |
| `GET /v1/overview` | `operator-filtered` | No native schema |
| `GET /v1/packaging-runs` | `operator-filtered` | [docs/operations/packaging-runs.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/packaging-runs.openapi.yaml) |
| `GET /v1/packaging-runs/{runID}` | `operator-any` | [docs/operations/packaging-runs.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/packaging-runs.openapi.yaml) |
| `GET /v1/packaging-runs/{runID}/events` | `operator-filtered` | [docs/operations/packaging-runs.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/packaging-runs.openapi.yaml) |
| `GET /v1/packaging-runs/{runID}/feedback` | `operator-any` | No native schema |
| `GET /v1/planning-sessions` | `operator-self` | [api/planning-sessions.yaml](https://github.com/jonathanagustin/conduit/blob/main/api/planning-sessions.yaml) |
| `GET /v1/planning-sessions/{sessionID}` | `operator-self` | [api/planning-sessions.yaml](https://github.com/jonathanagustin/conduit/blob/main/api/planning-sessions.yaml) |
| `GET /v1/planning-sessions/{sessionID}/binding` | `operator-self` | [api/planning-sessions.yaml](https://github.com/jonathanagustin/conduit/blob/main/api/planning-sessions.yaml) |
| `GET /v1/planning-sessions/{sessionID}/events` | `operator-self` | [api/planning-sessions.yaml](https://github.com/jonathanagustin/conduit/blob/main/api/planning-sessions.yaml) |
| `GET /v1/plans` | `operator-filtered` | No native schema |
| `GET /v1/plans/{planID}` | `operator-grant` | No native schema |
| `GET /v1/products` | `operator-filtered` | No native schema |
| `GET /v1/products/{productID}/download-releases` | `operator-grant` | No native schema |
| `GET /v1/products/{productID}/releases/{version}/download-assets` | `operator-grant` | No native schema |
| `GET /v1/products/{product}/dependencies/{version}` | `operator-grant` | No native schema |
| `GET /v1/products/{product}/promotion-stages/{fromChannel}/{toChannel}` | `operator-grant` | No native schema |
| `GET /v1/products/{product}/publications` | `operator-grant` | No native schema |
| `GET /v1/products/{product}/releases` | `operator-grant` | No native schema |
| `GET /v1/products/{product}/releases/{version}` | `operator-grant` | No native schema |
| `GET /v1/products/{product}/releases/{version}/rollout` | `operator-filtered` | No native schema |
| `GET /v1/promotions` | `operator-grant` | No native schema |
| `GET /v1/promotions/{promotionID}` | `operator-grant` | No native schema |
| `GET /v1/resource-grants` | `operator-any` | No native schema |
| `GET /v1/rollouts` | `operator-filtered` | No native schema |
| `GET /v1/session` | `operator-self` | No native schema |
| `GET /v1/spaces` | `operator-filtered` | No native schema |
| `GET /v1/spaces/{spaceID}` | `operator-grant` | No native schema |
| `GET /v1/spaces/{spaceID}/bindings` | `operator-filtered` | No native schema |
| `GET /v1/teams` | `operator-any` | No native schema |
| `GET /v1/teams/{teamName}` | `operator-any` | No native schema |
| `GET /version` | `public` | No native schema |
| `HEAD /v1/artifacts/blobs/{digest}` | `operator-grant` | No native schema |
| `POST /api/v0/products/helmChart/{mavenCoordinate}/publish` | `operator-grant` | No native schema |
| `POST /api/v0/products/service/{mavenCoordinate}/publish` | `operator-grant` | No native schema |
| `POST /api/v0/status/environments/{environmentId}/entities/health` | `agent` | No native schema |
| `POST /api/v0/status/environments/{environmentId}/entities/info` | `agent` | No native schema |
| `POST /api/v2/ontologies/{ontology}/actions/{action}/apply` | `operator-filtered` | [docs/operations/ontology-api.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/ontology-api.openapi.yaml) |
| `POST /api/v2/ontologies/{ontology}/objectSets/loadObjects` | `operator-filtered` | [docs/operations/ontology-api.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/ontology-api.openapi.yaml) |
| `POST /conduit.catalog.v1.ProductReadService/ListProductSummaries` | `operator-filtered` | No native schema |
| `POST /conduit.environments.v1.EnvironmentObservationService/GetEnvironmentReportedState` | `operator-grant` | No native schema |
| `POST /conduit.environments.v1.EnvironmentReadService/ListEnvironmentSummaries` | `operator-filtered` | No native schema |
| `POST /conduit.governance.v1.AuthorizationAuditReadService/ListAuthorizationAuditEvents` | `operator-grant` | No native schema |
| `POST /conduit.notifications.v1.NotificationService/ListNotificationEvents` | `operator-grant` | No native schema |
| `POST /conduit.packaging.v1.PackagingRunService/GetPackagingRun` | `operator-any` | No native schema |
| `POST /conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents` | `operator-filtered` | No native schema |
| `POST /conduit.packaging.v1.PackagingRunService/ListPackagingRuns` | `operator-filtered` | No native schema |
| `POST /conduit.plans.v1.PlanApprovalService/ApprovePlan` | `operator-grant` | No native schema |
| `POST /conduit.plans.v1.PlanReadService/ListPlanSummaries` | `operator-filtered` | No native schema |
| `POST /conduit.reporting.v1.ReportingService/GetOverview` | `operator-filtered` | No native schema |
| `POST /v1/adapted-plans/{adapter}` | `operator-grant` | No native schema |
| `POST /v1/agent-runs` | `operator-self` | No native schema |
| `POST /v1/apollo/environment-settings/validate` | `public` | No native schema |
| `POST /v1/apollo/graphql` | `operator-grant` | No native schema |
| `POST /v1/apollo/product-manifests/validate` | `public` | No native schema |
| `POST /v1/app-generations` | `operator-self` | No native schema |
| `POST /v1/artifact-scans` | `operator-grant` | No native schema |
| `POST /v1/artifacts/blobs` | `operator-grant` | No native schema |
| `POST /v1/assistant/threads` | `operator-self` | No native schema |
| `POST /v1/assistant/threads/{threadID}/fork` | `operator-self` | No native schema |
| `POST /v1/assistant/tools` | `operator-any` | No native schema |
| `POST /v1/assistant/turns` | `operator-any` | No native schema |
| `POST /v1/auth/logout` | `public` | No native schema |
| `POST /v1/blackouts` | `operator-grant` | No native schema |
| `POST /v1/bundle-transfer-confirmations/{confirmationID}/rescind` | `operator-grant` | No native schema |
| `POST /v1/bundles/import` | `operator-grant` | No native schema |
| `POST /v1/bundles/preview` | `operator-grant` | No native schema |
| `POST /v1/bundles/verification` | `operator-grant` | No native schema |
| `POST /v1/bundles/{bundleID}/transfer-confirmations` | `operator-grant` | No native schema |
| `POST /v1/change-requests/{changeRequestID}/approve` | `operator-grant` | No native schema |
| `POST /v1/change-requests/{changeRequestID}/cancel` | `operator-grant` | No native schema |
| `POST /v1/change-requests/{changeRequestID}/reject` | `operator-grant` | No native schema |
| `POST /v1/change-requests/{changeRequestID}/reopen` | `operator-grant` | No native schema |
| `POST /v1/change-requests/{requestID}/comments` | `operator-grant` | No native schema |
| `POST /v1/change-requests/{requestID}/label-mutations` | `operator-grant` | No native schema |
| `POST /v1/commands` | `operator-grant` | No native schema |
| `POST /v1/debug-session-requests` | `operator-grant` | No native schema |
| `POST /v1/debug-session-requests/{requestID}/revoke` | `operator-grant` | No native schema |
| `POST /v1/dev-workspaces` | `operator-grant` | No native schema |
| `POST /v1/dev-workspaces/{workspaceID}/acp` | `operator-grant` | No native schema |
| `POST /v1/dev-workspaces/{workspaceID}/acp/permissions/{requestID}` | `operator-grant` | No native schema |
| `POST /v1/dev-workspaces/{workspaceID}/agent-runs` | `operator-grant` | No native schema |
| `POST /v1/dev-workspaces/{workspaceID}/fork` | `operator-grant` | No native schema |
| `POST /v1/dev-workspaces/{workspaceID}/logs/clear` | `operator-grant` | No native schema |
| `POST /v1/dev-workspaces/{workspaceID}/runtime` | `operator-grant` | No native schema |
| `POST /v1/dev-workspaces/{workspaceID}/runtime/stop` | `operator-grant` | No native schema |
| `POST /v1/dev-workspaces/{workspaceID}/stop` | `operator-grant` | No native schema |
| `POST /v1/dev-workspaces/{workspaceID}/submit` | `operator-grant` | No native schema |
| `POST /v1/dev-workspaces/{workspaceID}/upgrade` | `operator-grant` | No native schema |
| `POST /v1/environments/{environmentID}/agent-revocations` | `operator-grant` | No native schema |
| `POST /v1/environments/{environmentID}/health-observations` | `agent` | No native schema |
| `POST /v1/environments/{environmentID}/leases` | `agent` | No native schema |
| `POST /v1/environments/{environmentID}/maintenance-admission/leases` | `agent` | No native schema |
| `POST /v1/environments/{environmentID}/module-installations` | `operator-grant` | No native schema |
| `POST /v1/environments/{environmentID}/suppressions` | `operator-grant` | No native schema |
| `POST /v1/export-pipelines/{pipelineID}/archive` | `operator-grant` | No native schema |
| `POST /v1/export-pipelines/{pipelineID}/build` | `operator-grant` | No native schema |
| `POST /v1/export-pipelines/{pipelineID}/preview` | `operator-grant` | No native schema |
| `POST /v1/export-pipelines/{pipelineID}/unarchive` | `operator-grant` | No native schema |
| `POST /v1/fleet/actions` | `operator-grant` | No native schema |
| `POST /v1/fleet/propose-fix` | `operator-grant` | No native schema |
| `POST /v1/intake-attachments` | `operator-self` | No native schema |
| `POST /v1/intelligence/courses` | `operator-any` | No native schema |
| `POST /v1/intelligence/courses/select` | `operator-grant` | No native schema |
| `POST /v1/intelligence/evals/cases` | `operator-any` | No native schema |
| `POST /v1/intelligence/evals/run` | `operator-any` | No native schema |
| `POST /v1/mcp` | `operator-any` | No native schema |
| `POST /v1/module-installations/{installationID}/pause` | `operator-grant` | No native schema |
| `POST /v1/module-installations/{installationID}/resume` | `operator-grant` | No native schema |
| `POST /v1/module-installations/{installationID}/unlink` | `operator-grant` | No native schema |
| `POST /v1/module-installations/{installationID}/update` | `operator-grant` | No native schema |
| `POST /v1/module-installations/{installationID}/update/automatic` | `operator-grant` | No native schema |
| `POST /v1/module-override-proposals` | `operator-grant` | No native schema |
| `POST /v1/module-override-proposals/{requestID}/apply` | `operator-grant` | No native schema |
| `POST /v1/modules/lock` | `operator-grant` | No native schema |
| `POST /v1/modules/releases` | `operator-grant` | No native schema |
| `POST /v1/modules/{moduleName}/releases/{version}/promote` | `operator-grant` | No native schema |
| `POST /v1/modules/{moduleName}/releases/{version}/recall` | `operator-grant` | No native schema |
| `POST /v1/offline-bundles` | `operator-grant` | No native schema |
| `POST /v1/packaging-runs/{runID}/cancel` | `operator-grant` | [docs/operations/packaging-runs.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/packaging-runs.openapi.yaml) |
| `POST /v1/packaging-runs/{runID}/feedback` | `operator-any` | No native schema |
| `POST /v1/packaging-runs/{runID}/model-proposal` | `operator-any` | No native schema |
| `POST /v1/packaging-runs/{runID}/rerun` | `operator-grant` | [docs/operations/packaging-runs.openapi.yaml](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/packaging-runs.openapi.yaml) |
| `POST /v1/planning-sessions` | `operator-self` | [api/planning-sessions.yaml](https://github.com/jonathanagustin/conduit/blob/main/api/planning-sessions.yaml) |
| `POST /v1/planning-sessions/{sessionID}/accept` | `operator-self` | [api/planning-sessions.yaml](https://github.com/jonathanagustin/conduit/blob/main/api/planning-sessions.yaml) |
| `POST /v1/planning-sessions/{sessionID}/answers` | `operator-self` | [api/planning-sessions.yaml](https://github.com/jonathanagustin/conduit/blob/main/api/planning-sessions.yaml) |
| `POST /v1/planning-sessions/{sessionID}/compute` | `operator-self` | [api/planning-sessions.yaml](https://github.com/jonathanagustin/conduit/blob/main/api/planning-sessions.yaml) |
| `POST /v1/planning-sessions/{sessionID}/turns` | `operator-self` | [api/planning-sessions.yaml](https://github.com/jonathanagustin/conduit/blob/main/api/planning-sessions.yaml) |
| `POST /v1/planning-sessions/{sessionID}/turns/{runID}/cancel` | `operator-self` | [api/planning-sessions.yaml](https://github.com/jonathanagustin/conduit/blob/main/api/planning-sessions.yaml) |
| `POST /v1/plans` | `operator-grant` | No native schema |
| `POST /v1/plans/{planID}/approvals` | `operator-grant` | No native schema |
| `POST /v1/plans/{planID}/cancel` | `operator-grant` | No native schema |
| `POST /v1/plans/{planID}/finish` | `agent` | No native schema |
| `POST /v1/plans/{planID}/heartbeat` | `agent` | No native schema |
| `POST /v1/plans/{planID}/maintenance-admission/start` | `agent` | No native schema |
| `POST /v1/plans/{planID}/start` | `agent` | No native schema |
| `POST /v1/plans/{planID}/undo-unmanage` | `operator-grant` | No native schema |
| `POST /v1/product-publications/download-assets` | `operator-grant` | No native schema |
| `POST /v1/product-publications/helm-chart` | `operator-grant` | No native schema |
| `POST /v1/product-publications/terraform` | `operator-grant` | No native schema |
| `POST /v1/products/{product}/promotion-stages/{fromChannel}/{toChannel}/pause` | `operator-grant` | No native schema |
| `POST /v1/products/{product}/promotion-stages/{fromChannel}/{toChannel}/resume` | `operator-grant` | No native schema |
| `POST /v1/products/{product}/releases/{version}/retire` | `operator-grant` | No native schema |
| `POST /v1/products/{product}/retire` | `operator-grant` | No native schema |
| `POST /v1/promotions/evaluations` | `operator-grant` | No native schema |
| `POST /v1/resource-grants` | `operator-grant` | No native schema |
| `POST /v1/rollouts` | `operator-grant` | No native schema |
| `POST /v1/sites/check-in` | `operator-any` | No native schema |
| `POST /v1/spaces` | `operator-grant` | No native schema |
| `POST /v1/temporary-maintenance-overrides` | `operator-grant` | No native schema |
| `POST /v1/validate` | `public` | No native schema |
| `PUT /v1/approval-policies/{policyID}` | `operator-grant` | No native schema |
| `PUT /v1/artifacts/blobs/{digest}` | `operator-grant` | No native schema |
| `PUT /v1/change-requests/{changeRequestID}/labels` | `operator-grant` | No native schema |
| `PUT /v1/environments/{environmentID}/profile` | `operator-grant` | No native schema |
| `PUT /v1/export-pipelines/{pipelineID}` | `operator-grant` | No native schema |
| `PUT /v1/export-pipelines/{pipelineID}/target-hub-versions/{targetHub}` | `operator-grant` | No native schema |
| `PUT /v1/labels/{labelID}` | `operator-grant` | No native schema |
| `PUT /v1/modules/{module}/auto-update-policy` | `operator-grant` | No native schema |
| `PUT /v1/notifications/contacts` | `operator-self` | No native schema |
| `PUT /v1/notifications/preferences` | `operator-self` | No native schema |
| `PUT /v1/spaces/{spaceID}/bindings` | `operator-grant` | No native schema |
| `PUT /v1/teams/{teamName}` | `operator-grant` | No native schema |
