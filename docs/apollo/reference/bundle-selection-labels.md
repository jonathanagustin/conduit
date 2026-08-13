# Export Pipeline label requirements

Source identity is recorded in `review-ledger.tsv`. This note captures the
agent-useful contract without mirroring the upstream page.

- Required and excluded label rules are scoped to the resource types declared
  by each rule. A rule with no values tests label presence; one or more values
  use OR semantics within that rule.
- Multiple required rules use AND semantics. Exclusion takes precedence over
  inclusion when both match.
- Platform supports this contract for its label-bearing `ENVIRONMENT` and
  `PRODUCT_RELEASE` Bundle resources. Environment labels are explicit desired
  state; Product Release filtering uses effective labels, including the
  scanner-owned vulnerability labels derived from exact assessment evidence.
- A dependency added during Product Release closure must also satisfy the
  applicable label rules or the build fails. This preserves both the filtering
  contract and an installable dependency closure.

Other private Apollo resource/label applicability metadata is not public and
is not inferred. Adding another label-bearing Platform resource type requires
an explicit typed label field and tests before it can be named in a rule.

Executable evidence:

```text
go test ./internal/controlplane -run TestBundleLabelRequirements
```
