# Reporting mutation refresh

Measured on 2026-11-17 with Go 1.27.4 and Gremlins 0.6.0:

```sh
gremlins unleash --workers 1 --output raw.json --threshold-efficacy 0 ./internal/reporting/domain
```

The exact-installation filter added in `55952899` changed `fleet.go` after its prior source-bound equivalence review. This rerun killed 58 mutants, retained 14 survivors, and recorded zero timeouts. The two added non-equivalent mutations were killed. Raw efficacy is 80.56%; the existing separately reported non-equivalent efficacy remains 100%.

Every reviewed equivalent still names the identical source expression and has the same rationale. Their positions moved by one line before the new filter and four lines after it. The fresh raw report confirms all 14 remain LIVED. The manifest now binds the current source hash and positions; no additional survivor is classified equivalent. Other packages and the oldest sweep timestamp are unchanged.
