# Exact CLI numbers in YAML

CLI JSON and YAML output share the command result's JSON field names and custom
JSON serialization. YAML normalization decodes JSON numbers as `json.Number`,
then wraps their original literals with numeric YAML scalar tags. It does not
convert them through `float64`, or emit them as quoted strings.

For example, `conduit plan list --read-transport connect --output yaml` retains
`revision: 9007199254740993` exactly. REST reads use the same output boundary.
Signed 64-bit minima, unsigned 64-bit maxima, decimal/exponent literals and
negative zero remain numeric. A source string such as `"9007199254740993"` remains
a string. The consumer must choose a numeric type capable of representing the
literal; the CLI does not clamp it to a machine float's precision.

JSON, CSV and table output are unchanged. YAML field naming and stable mapping
ordering still come from the existing JSON normalization and YAML encoder.

Verification covers the original float-normalization failure, independent YAML
scalar tags/literals, and actual CLI REST/Connect reads against a native
in-memory Hub with revisions above 2^53 and at the uint64 maximum. Negative
controls restore float normalization, quote numbers, or omit array traversal;
all must fail the regression checks. No production Plan approval is involved.
