# Assumptions ledger — load-bearing claims and their verification status

A standing record of **load-bearing assumptions**. The rule: a claim that gates work — "X is
impossible", "Y is unimplemented", "Z requires a write", "the tool can't do this" — must be **tested
before it is trusted or propagated**, and the result recorded here. An assumption that lives only in a
code comment or a chat turn gets copied forward unverified; this ledger is where it gets a status and a
test.

Status: **ASSUMED** (stated, not tested — treat as suspect) · **CONFIRMED** (tested/observed) ·
**REFUTED** (tested false). A REFUTED row must name every place the false claim propagated so they can
all be fixed.

| # | assumption | status | how verified | where it propagated |
|---|---|---|---|---|
| A1 | _the load-bearing claim_ | **ASSUMED** | _the cheap direct test you ran (compile it, probe it, parse it)_ | _every comment/doc/commit that relied on it_ |

## How to use this

- Before relying on a "can't / impossible / unimplemented / requires-grant" claim, check here. If it's
  not listed, it's ASSUMED until you test it — add the row when you do.
- When you REFUTE a claim, grep for everywhere it was stated and fix each; list them in the last column
  so the refutation is complete, not local.
- Prefer a cheap direct test over inheriting a prior conclusion. One unverified "impossible" can defer
  real work for a whole session.
