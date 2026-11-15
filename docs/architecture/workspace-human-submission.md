# Workspace human-review submissions

The existing developer-workspace submit command now calls the existing `ProposeHumanChange` kernel command. An automatic policy cannot persist an approved request and only then be rejected by the workspace service. Submission remains a review proposal; it does not approve, publish, deploy or create a Plan. Policy rejection is still honored by the kernel.

The native caller retains service-account status and one-hop represented identity from the authenticated principal. Those fields reach the stored author so the existing actor/represented-person self-review rule remains effective. Client request bodies cannot set them. Workspace ownership remains the existing actual-caller rule.

Request target identity now binds the credential/delegation, environment, workspace, revision and source digest. Exact retries reuse the identity, while a different represented author or scope cannot reuse another request. The readable workspace/revision prefix is retained. This is registered as `workspace.ScopedHumanSubmission`; previous stored requests are not rewritten.

The revision and source digest in the request are the workspace's recorded coordinates. This does not make the current recorded store into a branch-backed editor, attest file content or execute a metadata upgrade. Those wider backend/editor tasks remain separate. This fix supports the studio controls in #558 without adding a second approval path.
