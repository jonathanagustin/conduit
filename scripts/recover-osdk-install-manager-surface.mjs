// Recover the public Apollo install-manager / agent API surface from osdk-ts.
//
// The com.palantir.apollo backend jars are not fetchable from the tenant, but
// the PUBLIC, Apache-licensed osdk-ts repository generates conjure-lite clients
// for the marketplace install-manager and apolloagent APIs. Those clients carry
// the real route path, HTTP method, and untruncated request/response types - the
// wire surface this project otherwise marked Unknown. The recovery already
// treats osdk-ts as Primary for the published client surface (H57/A17); this
// captures the install-manager slice of it.
//
// Fidelity: Primary for the PUBLISHED client (a pinned public commit, verified
// by fetching that exact SHA). It is the published surface, not a claim about
// Apollo's private server IR.
//
//   node recover-osdk-install-manager-surface.mjs --out docs/evidence/osdk-install-manager-surface.json
import { writeFileSync } from "node:fs";

const COMMIT = "c40ca076c1459019a9327d79cf0581b6daf978a4";
const REPO = "palantir/osdk-ts";
const ROOT = `packages/client.unstable/src/generated/marketplace`;

const outIndex = process.argv.indexOf("--out");
const outPath = outIndex > -1 ? process.argv[outIndex + 1] : null;

async function ghJson(url) {
  const res = await fetch(url, { headers: { accept: "application/vnd.github+json" } });
  if (!res.ok) throw new Error(`${url} -> HTTP ${res.status}`);
  return res.json();
}
async function raw(path) {
  const res = await fetch(`https://raw.githubusercontent.com/${REPO}/${COMMIT}/${path}`);
  if (!res.ok) throw new Error(`${path} -> HTTP ${res.status}`);
  return res.text();
}

// One tree read locates every endpoint file under the install-manager and
// apolloagent services, so the set is discovered, not hard-coded.
const tree = await ghJson(
  `https://api.github.com/repos/${REPO}/git/trees/${COMMIT}?recursive=1`,
);
const endpointFiles = tree.tree
  .filter((t) => t.type === "blob")
  .map((t) => t.path)
  .filter((p) => p.startsWith(ROOT))
  .filter((p) => /\/(InstallManager(Admin)?Service|ApolloPublisherService)\/[a-zA-Z]+\.ts$/.test(p));

console.error(`${endpointFiles.length} endpoint files under install-manager / apollo-publisher`);

const services = {};
for (const path of endpointFiles.sort()) {
  const [, service, endpoint] = path.match(/\/([A-Za-z]+Service)\/([a-zA-Z]+)\.ts$/);
  const source = await raw(path);
  const method = (source.match(/"(GET|POST|PUT|DELETE|PATCH)"/) || [])[1] ?? "?";
  // The route is the template literal argument to conjureFetch; normalize
  // ${var} to {var} so it reads as an OpenAPI-style path template.
  const routeMatch = source.match(/conjureFetch\(\s*ctx,\s*`([^`]*)`/);
  const route = routeMatch ? routeMatch[1].replace(/\$\{([^}]+)\}/g, "{$1}") : "?";
  // The response type is the Promise<...> return; the request is the last typed arg.
  const responseType = (source.match(/Promise<\s*([A-Za-z_][\w.]*)/) || [])[1] ?? "?";
  (services[service] ??= []).push({ endpoint, method, route, responseType });
}

const evidence = {
  apiVersion: "apollo.platform.example/osdk-install-manager-surface/v1alpha1",
  kind: "OsdkInstallManagerSurface",
  source: {
    repo: REPO,
    commit: COMMIT,
    path: ROOT,
    license: "Apache-2.0 (public osdk-ts)",
    fidelity: "Primary — the published conjure-lite client at a pinned public commit, "
      + "fetched by exact SHA. It is the published client surface, not Apollo's private server IR.",
  },
  counts: {
    services: Object.keys(services).length,
    endpoints: Object.values(services).reduce((n, e) => n + e.length, 0),
  },
  services,
  hypothesisLinks: {
    "install-manager": "H16 — the public Install Manager boundary; this adds its full route surface.",
    apolloagent: "H18 — the Apollo Agent types are generated alongside these clients (apolloagent/api).",
  },
  boundary:
    "This is the published osdk-ts client for the marketplace install-manager API. It recovers real "
    + "routes, methods, and types for the install-manager surface. It does not recover the Apollo Hub's "
    + "own deployment GraphQL (H38) or the private Hub-to-Agent control wire (H18), which remain blocked.",
};

const serialized = JSON.stringify(evidence, null, 2) + "\n";
if (outPath) {
  writeFileSync(outPath, serialized);
  console.error(`wrote ${outPath}`);
} else {
  process.stdout.write(serialized);
}
console.error(
  `services=${evidence.counts.services} endpoints=${evidence.counts.endpoints}`,
);
