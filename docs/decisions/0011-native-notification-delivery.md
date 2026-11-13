# ADR 0011: Native SMTP and Slack notification delivery

Status: accepted (2026-11-13), issue #119.

The existing HTTPS relay delivers to one operator endpoint and cannot itself
submit mail or Slack messages to a selected recipient. Retain that mode and add
native SMTP and Slack adapters under the bounded notification context, sharing
the existing PostgreSQL delivery queue and dispatcher.

Native routing must honor the current stored preference's selected contact ID,
owner and channel. It may not substitute another contact when that selection is
missing or incompatible. Credentials come from mounted files, SMTP requires
verified TLS, and Slack uses its fixed public API with an explicit successful
acknowledgement. Both transports remain opt-in. Submission is at least once;
provider acceptance is not evidence of human receipt.

`SLACK` is a registered Conduit preference extension. No private Apollo protocol
claim, PagerDuty delivery, publication, or deployment is part of this decision.
See `../operations/native-notification-delivery.md` for configuration, pinned
public sources, upgrade constraints and executable acceptance evidence.
