# Console components

Two layers. The base layer does not import the domain layer, and it does not import Hub data.

The assistant-ui thread is not a base component and is not restyled.

## Variants

`base/variants.ts` maps a small typed set of axes onto Blueprint props. Domain components pass those axes. They do not set colors or one-off class names for intent.

| Axis | Values | Blueprint |
| --- | --- | --- |
| intent | none, primary, success, warning, danger | `intent` |
| size | sm, md, lg | `size` small / medium / large. Tag has no small size, so sm stays medium. |
| density | compact, comfortable | `compact` on Card, HTMLTable, Section |
| emphasis | minimal, outlined, filled | Button `minimal` or `outlined`. Tag has no outlined prop, so outlined uses `minimal`. Filled is the default solid tag or button. Panel elevation is 0 for minimal and outlined, 1 for filled. |
| state | ready, loading, empty, error, disabled-with-reason | `Spinner`, `NonIdealState`, `Callout`, `Tooltip` + disabled `Button` |

Presets: GO is success filled, CAUTION is warning outlined, NO-GO is danger filled.

`StatCard` adds `trend` (up, down, flat) as a Blueprint `Tag` with an icon.

## Which Blueprint primitive each base component wraps

| Component | Blueprint |
| --- | --- |
| StatusTag | Tag, Icon |
| ActionButton | Button |
| DisabledWithReason | Tooltip, Button |
| StatCard | Card, EntityTitle, Tag, Icon |
| KpiStrip | layout only; children are StatCards |
| HealthBar | a proportional bar; Blueprint has no segmented progress |
| PipelineGraph | Button, Icon |
| Panel | Section, SectionCard |
| PageHeader, ObjectHeader, PropertyCard, EntityCard | EntityTitle, Card |
| SummaryBar | KpiStrip |
| TabBar | Tabs, Tab |
| FilterBar | FormGroup, HTMLSelect |
| DataTable, DenseTable | HTMLTable (`@blueprintjs/table` is not used; the dense index is HTMLTable) |
| SidebarNav | Menu, MenuItem |
| NavTree | Tree |
| AppShell | Navbar plus a three-pane layout Blueprint does not ship |
| Checklist | StatusTag |
| AuditRail, ActivityTimeline, InboxList | Card, Menu, MenuItem, NonIdealState |
| Kanban | Card, EntityTitle. No Blueprint kanban. |
| EmptyState | NonIdealState |
| ErrorState, ConstraintRow, OutputPane | Callout |
| LoadingState | Spinner |
| Surface | the state components above |
| SplitListDetail, SplitView | layout only |
| ReviewChecklist, RequirementChecklist | Checklist |
| ReviewHistory | ActivityTimeline |
| DiffPane | PropertyCard |
| ImpactSummary | Card, EntityTitle, Menu |
| StepTimeline | Card, ProgressBar, Tag |
| TaskList | Menu, MenuItem, NonIdealState |
| NotePopover | Popover, Card, Button |
| SideDrawer | Drawer |
| PromptDialog | Dialog |
| TextField | FormGroup, InputGroup |

## Domain

Domain components pick variants and compose only base components. Hub calls stay in `../data`.

## Width

The shell keeps three columns from 1024px up. Between 1024px and 1239px the rails narrow. Below 1024px the navigation and the assistant are drawers opened from the navbar, and split panes stack. Tables sit in a horizontal scroller.

## Restored and left out

Overview, environments, fleet, the software catalog, packaging runs, plans, approvals, change requests, and packaging audit events are mounted on the Hub routes that already exist. Settings, a search index, the globe, and the notification preference editor stay out: the Hub does not expose those surfaces here. The right rail is the stock assistant thread.

## Gallery

In `vite dev`, `/console/dev/components` renders every base component in its variants, with a control for the dark and light token pair. The production bundle does not mount that route.
