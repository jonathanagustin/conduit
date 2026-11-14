#!/usr/bin/env node
// Proves the route-authorization gate rejects each thing it claims to reject.
//
// A gate whose refusals have stopped working looks exactly like a gate whose
// refusals never come up, and this one is unusually exposed to that: the tree
// it guards has been correct since the register was written, so a regex that
// matched nothing at all would print the same PASS as a regex that matched
// every route. The floor inside the gate catches the total collapse; this
// catches everything narrower.
//
// So this builds a minimal Hub in a temporary directory -- a control-plane
// facade, a generated Apollo transport, a notification adapter and a register
// -- confirms the gate passes on it, then plants one violation per rule and
// confirms each is caught. The fixture is used rather than the repository tree
// because planting a route with no decision in internal/controlplane would mean
// editing the package the line ceiling is measured against, and a self-test
// must not be able to leave a modified Hub behind.
//
// The fixture carries 120 routes rather than three. The gate's floor is 100, so
// a smaller fixture would trip it on every case and every planted violation
// would "pass" for the wrong reason -- which is itself one of the rules below.
//
// Two negative controls run alongside, because both are things a stricter
// version of this gate would get wrong:
//
//   - a route registered in a _test.go file must NOT need a register entry, or
//     every fixture in the repository becomes a build failure;
//   - the Apollo transport writes its patterns as http.MethodGet+" "+base+path
//     and the notification adapter writes a method-less wildcard, so the
//     fixture spells both, and a gate that only read plain literals would
//     report them as unmounted register rows.
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const SOURCE_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const GATE = "scripts/verify-route-authorization.mjs";
const REGISTER = "internal/hub/composition/authorization.go";
const FACADE = "internal/controlplane/http.go";
const fixture = mkdtempSync(join(tmpdir(), "conduit-route-authorization-"));

const write = (path, contents) => {
  const target = join(fixture, path);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, contents);
};
const read = (path) => readFileSync(join(fixture, path), "utf8");
const run = () =>
  execFileSync(process.execPath, [join(fixture, GATE)], { encoding: "utf8", stdio: "pipe" });

// 120 bulk routes so the fixture clears the gate's floor of 100. They are
// otherwise uninteresting: the rules are exercised by the handful of named
// routes below them.
const bulk = Array.from({ length: 120 }, (_, index) => `GET /v1/thing-${index}`);

const facadeRoutes = [...bulk, "GET /health/live", "POST /v1/plans"];
const apolloRoutes = ["/api/v0/products", "/api/v0/environments/{environmentRid}"];
const notificationRoutes = ["GET /v1/notifications/inbox"];
const spaceRoutes = ["GET /v1/spaces"];
const assetRoutes = ["POST /v1/product-publications/download-assets"];
const activityRoutes=["POST /v1/change-requests/{requestID}/comments"];
const wildcardRoute = "/v1/notifications/{path...}";

const facade = () =>
  "package controlplane\n\nfunc register(mux *http.ServeMux) {\n" +
  facadeRoutes.map((pattern) => `\tmux.HandleFunc(${JSON.stringify(pattern)}, nil)\n`).join("") +
  "}\n";

const apollo = () =>
  "package apollo\n\nfunc HandlerWithOptions(si ServerInterface, options StdHTTPServerOptions) http.Handler {\n" +
  apolloRoutes
    .map((path) => `\tm.HandleFunc(http.MethodGet+" "+options.BaseURL+${JSON.stringify(path)}, wrapper.X)\n`)
    .join("") +
  "\treturn m\n}\n";

const notifications = () =>
  "package httpapi\n\nfunc Register(mux *http.ServeMux) error {\n" +
  notificationRoutes.map((pattern) => `\tmux.HandleFunc(${JSON.stringify(pattern)}, nil)\n`).join("") +
  `\tmux.HandleFunc(${JSON.stringify(wildcardRoute)}, nil)\n` +
  "\treturn nil\n}\n";

const row = (pattern, decision, detail) =>
  `\t\t{Pattern: ${JSON.stringify(pattern)},\n\t\t\tDecision: ${decision}, Detail: ${JSON.stringify(detail)}},\n`;

const register = (rows) =>
  "package composition\n\ntype Decision string\n\nconst (\n" +
  "\tDecisionPublic Decision = \"public\"\n" +
  "\tDecisionOperatorGrant Decision = \"operator-grant\"\n" +
  "\tDecisionOperatorFiltered Decision = \"operator-filtered\"\n" +
  ")\n\nfunc HubRouteAuthorization() []RouteAuthorization {\n\treturn []RouteAuthorization{\n" +
  rows.join("") +
  "\t}\n}\n";

const baselineRows = () => [
  ...bulk.map((pattern) => row(pattern, "DecisionOperatorGrant", "requireOperatorPrincipal + requireResourceOperation")),
  row("GET /health/live", "DecisionPublic", "liveness for a load balancer; it reads no repository state"),
  row("POST /v1/plans", "DecisionOperatorGrant", "requireOperatorPrincipal + requireResourceOperation"),
  ...apolloRoutes.map((path) => row(`GET ${path}`, "DecisionOperatorGrant", "requireApolloIdentity + authorizeOperator")),
  ...notificationRoutes.map((pattern) => row(pattern, "DecisionOperatorGrant", "operatorPrincipal + resourceOperation")),
  ...spaceRoutes.map((pattern) => row(pattern, "DecisionOperatorFiltered", "caller-bound Space and resource visibility")),
  ...assetRoutes.map((pattern) => row(pattern, "DecisionOperatorGrant", "caller-bound publication authority")),
 ...activityRoutes.map((pattern)=>row(pattern,"DecisionOperatorGrant","request annotation authority")),
  row(wildcardRoute, "DecisionPublic", "the method-not-allowed responder; it resolves nobody and reads nothing"),
];

function build() {
  mkdirSync(join(fixture, "scripts"), { recursive: true });
  copyFileSync(join(SOURCE_ROOT, GATE), join(fixture, GATE));
  write(FACADE, facade());
  write("internal/apollo/openapi.gen.go", apollo());
  write("internal/notifications/adapters/httpapi/httpapi.go", notifications());
  write("internal/spaces/adapters/httpapi/httpapi.go", 'package httpapi\nfunc register(mux *http.ServeMux) { mux.HandleFunc("GET /v1/spaces", nil) }\n');
  write("internal/productassets/adapters/httpapi/httpapi.go", 'package httpapi\nfunc register(mux *http.ServeMux) { mux.HandleFunc("POST /v1/product-publications/download-assets", nil) }\n');
  write("internal/installoverride/adapters/httpapi/httpapi.go",'package httpapi\nfunc register(mux *http.ServeMux) {}\n');
  write("internal/changeactivity/adapters/httpapi/httpapi.go",'package httpapi\nfunc register(mux *http.ServeMux) { mux.HandleFunc("POST /v1/change-requests/{requestID}/comments", nil) }\n');
  write("internal/packagingworkflow/httpapi/httpapi.go", "package httpapi\n");
  // The negative control: a route mounted in a test file. It must not be
  // demanded of the register.
  write(
    "internal/controlplane/http_test.go",
    'package controlplane\n\nfunc TestX(t *testing.T) {\n\tmux.HandleFunc("GET /v1/only-in-a-test", nil)\n}\n',
  );
  write(REGISTER, register(baselineRows()));
}

const failures = [];
const expectPass = (what) => {
  try {
    const output = run();
    if (!output.includes("PASS")) failures.push(`${what}: gate did not print PASS: ${output}`);
    return output;
  } catch (error) {
    failures.push(`${what}: gate failed on a tree it should accept:\n${error.stdout}${error.stderr}`);
    return "";
  }
};
const expectFail = (what, needle) => {
  try {
    run();
    failures.push(`${what}: gate passed a tree it should reject`);
  } catch (error) {
    const output = `${error.stdout}${error.stderr}`;
    if (!output.includes(needle)) {
      failures.push(`${what}: gate failed, but not for the stated reason (wanted ${needle}):\n${output}`);
    }
  }
};

// --- the baseline -----------------------------------------------------------
build();
const baseline = expectPass("baseline");
if (baseline && !/129 mounted routes/.test(baseline)) {
  failures.push(`baseline: gate counted the wrong number of routes: ${baseline.trim()}`);
}
// The negative control, stated as an assertion rather than left implicit.
if (baseline.includes("only-in-a-test")) {
  failures.push("baseline: a route registered in a _test.go was demanded of the register");
}

// --- rule 1: a mounted route with no registered decision --------------------
// This is the finding. A route is added, nobody says what it requires, and
// before the guard existed it served its body to anybody.
build();
write(
  FACADE,
  facade().replace("}\n", '\tmux.HandleFunc("GET /v1/tenant-secrets", nil)\n}\n'),
);
expectFail("a mounted route with no registered decision", "GET /v1/tenant-secrets is mounted in");

// --- rule 2: a registered decision for a route nobody mounts ----------------
build();
write(REGISTER, register([...baselineRows(), row("GET /v1/deleted-last-year", "DecisionOperatorGrant", "requireOperatorPrincipal")]));
expectFail("a register row for an unmounted route", "which no Go source mounts");

// --- rule 3: a decision the register does not declare -----------------------
build();
write(REGISTER, read(REGISTER).replace('"POST /v1/plans",\n\t\t\tDecision: DecisionOperatorGrant', '"POST /v1/plans",\n\t\t\tDecision: DecisionSomebodyInvented'));
expectFail("an undeclared decision", "which internal/hub/composition/authorization.go does not declare");

// --- rule 4: a decision with nothing behind it ------------------------------
build();
write(REGISTER, read(REGISTER).replace('Detail: "requireOperatorPrincipal + requireResourceOperation"},\n\t\t{Pattern: "GET /v1/thing-1"', 'Detail: ""},\n\t\t{Pattern: "GET /v1/thing-1"'));
expectFail("an empty detail", "names a decision with an empty detail");

// --- rule 5: public with no stated reason -----------------------------------
// The shape a hole takes when somebody needed the gate to pass.
build();
write(REGISTER, read(REGISTER).replace('Detail: "liveness for a load balancer; it reads no repository state"', 'Detail: "public"'));
expectFail("a public row with no reason", "does not say why that is safe");

// --- rule 6: the same pattern registered twice ------------------------------
// The guard builds a ServeMux from these; a duplicate would panic the Hub at
// assembly, which is a worse way to find out.
build();
write(REGISTER, register([...baselineRows(), row("POST /v1/plans", "DecisionOperatorGrant", "requireOperatorPrincipal")]));
expectFail("a duplicated register pattern", "names POST /v1/plans twice");

// --- rule 7: the floor, which is what stops a broken regex passing ----------
build();
write(FACADE, 'package controlplane\n\nfunc register(mux *http.ServeMux) {\n\tmux.HandleFunc("GET /health/live", nil)\n}\n');
expectFail("a tree with too few routes to be the Hub", "below the floor of 100");

// --- rule 8: an emptied register --------------------------------------------
build();
write(REGISTER, "package composition\n\nfunc HubRouteAuthorization() []RouteAuthorization {\n\treturn nil\n}\n");
expectFail("an emptied register", "yielded no rows");

rmSync(fixture, { recursive: true, force: true });
if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}
console.log("PASS route-authorization gate self-test: 8 planted violations, each rejected");

