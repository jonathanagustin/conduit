import { releaseWavesMarkup } from "./release-waves.js";
import { releaseCalendarMarkup } from "./release-calendar.js";
import {
  maintenanceMarkup,
  selectedEnvironment,
} from "./release-environment-hub.js";
/* Release-gate screens. Recalls and the proposal trace read the Hub. */
import { recordedGateRows, renderLiveProposal } from "./live-hub.js";

function esc(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function compoundTag(left, right, tone = "blue") {
  const bg =
    {
      blue: "background:rgba(24,48,82,0.6);color:#7cb4ff",
      green: "background:rgba(20,62,43,0.6);color:#34d399",
      amber: "background:rgba(68,45,16,0.6);color:#fbbf24",
      red: "background:rgba(68,23,27,0.6);color:#f87171",
      violet: "background:rgba(46,33,82,0.6);color:#b4a2f8",
      muted: "background:#151d24;color:#dce4ee",
    }[tone] || "background:#151d24;color:#dce4ee";
  return `<span class="bp6-compound-tag"><span class="bp6-compound-left">${esc(left)}</span><span class="bp6-compound-right" style="${bg}">${esc(right)}</span></span>`;
}

function segmentedCtrl(options, currentVal, onSelectTpl) {
  return `<div role="radiogroup" class="bp6-segmented">${options
    .map(([val, lbl]) => {
      const js = onSelectTpl.split("__VAL__").join(val);
      const on = currentVal === val;
      return `<button type="button" role="radio" aria-checked="${on ? "true" : "false"}" data-onclick="${js}" class="bp6-seg-btn${on ? " is-selected" : ""}">${lbl}</button>`;
    })
    .join("")}</div>`;
}

function keyCombo(combo) {
  const label = combo
    .replaceAll("mod+", "&#8984;")
    .replaceAll("shift+", "&#8679;")
    .replaceAll("enter", "&#9166;");
  return `<kbd class="bp6-key-combo" aria-label="${esc(combo)}">${label}</kbd>`;
}

function entityTitle(title, subtitle, tags = "") {
  return `<div class="bp6-entity">
    <div class="bp6-entity-title">${title}</div>
    ${subtitle ? `<div class="bp6-entity-sub">${subtitle}</div>` : ""}
    ${tags ? `<div class="bp6-entity-tags">${tags}</div>` : ""}
  </div>`;
}

function sectionCard(title, body, opts = {}) {
  const subtitle = opts.subtitle
    ? `<div class="bp6-section-sub">${opts.subtitle}</div>`
    : "";
  const right = opts.right
    ? `<div class="bp6-section-right">${opts.right}</div>`
    : "";
  return `<section class="bp6-section so-panel ${opts.className || ""}">
    <header class="bp6-section-head">
      <div class="min-w-0">${title ? `<div class="bp6-section-title">${title}</div>` : ""}${subtitle}</div>
      ${right}
    </header>
    <div class="bp6-section-card">${body}</div>
  </section>`;
}

function slaClock(left, total) {
  const pct = Math.max(0, Math.min(100, Math.round((left / total) * 100)));
  const color = left <= 14 ? "#e76a6e" : left <= 30 ? "#eab854" : "#34d399";
  return `<span class="bp6-sla" title="${left} of ${total} days remaining">
    <span class="bp6-sla-track"><span class="bp6-sla-fill" style="width:${pct}%;background:${color}"></span></span>
    <span class="tnum">${left}d left</span>
  </span>`;
}

function recallExpression(mode, start, end) {
  if (mode === "SINGLE") return `== ${start}`;
  if (mode === "OPEN_ENDED") return `>= ${start} (blocks later publishes)`;
  if (mode === "CLOSED_RANGE") return `>= ${start}, < ${end}`;
  return `>= ${start} (excl. ${end})`;
}

export function createReleaseGates(ctx) {
  const state = ctx.state;

  function paint() {
    ctx.render();
  }

  function issueRecall() {
    const start = state.recallStart.trim() || "0.31.0";
    const end = state.recallEnd.trim() || "0.31.2";
    const expr = recallExpression(state.recallMode, start, end);
    const nextId = `RCL-2026-0${20 + state.recalls.length}`;
    state.recalls.unshift({
      id: nextId,
      prod: state.recallProduct,
      mode: state.recallMode,
      expr,
      why: state.recallNote || "Operator recall from the vulnerability gate",
      envs: 4,
      status: "Active Roll-Off",
      tone: "amber",
    });
    state.recallBanner = `Issued ${nextId} (${expr}) on ${state.recallProduct}. Example only: open upgrade requests would be canceled and roll-off plans prioritized.`;
    ctx.recordAudit("recall.issued");
    paint();
  }

  function toggleRecall(idx) {
    const row = state.recalls[idx];
    if (!row) return;
    const reverted = row.status === "Reverted";
    row.status = reverted ? "Active Roll-Off" : "Reverted";
    row.tone = reverted ? "amber" : "muted";
    ctx.recordAudit("recall.toggled");
    paint();
  }

  function toggleSuppression(idx) {
    const row = state.suppressions[idx];
    if (!row) return;
    row.approved = !row.approved;
    ctx.recordAudit(
      row.approved ? "suppression.approved" : "suppression.revoked",
    );
    paint();
  }

  function setRampWindow(val) {
    const prev = state.rampWindow;
    if (prev === val) return;
    state.rampWindow = val;
    state.stagedEdits.unshift({
      object: "Release channel",
      key: "edge-cohorts",
      field: "rampWindow",
      from: prev,
      to: val,
    });
    if (val === "off") {
      state.rampCohorts.forEach((row) => {
        if (row.st === "Rate Limited") {
          row.st = "Upgrading";
          row.tone = "blue";
          row.note = "Ramp off. The limit is cleared.";
        }
      });
    }
    ctx.recordAudit("rollout.ramp_window_updated");
    paint();
  }

  function bypassRamp(id) {
    const row = state.rampCohorts.find((item) => item.id === id);
    if (!row || row.st !== "Rate Limited") return;
    row.st = "Bypassed (Immediate)";
    row.tone = "green";
    row.note = "Operator command skipped the ramp limit";
    state.stagedEdits.unshift({
      object: "Command",
      key: id,
      field: "forceUpgradeNow",
      from: "Rate Limited",
      to: "Immediate",
    });
    ctx.recordAudit("rollout.ramp_bypassed");
    paint();
  }

  function setTraceBlock(id) {
    state.traceBlock = id;
    paint();
  }

  function runProposalTrace() {
    state.traceRuns += 1;
    const nextId = `PRP-${901 + state.traceRuns}`;
    const made = {
      oom: {
        rule: "Manual run · triage_failed_release_plan",
        target: "edge-west-02 · ingress-gateway",
        action: "Stage CR-310 (limits.memory: 1Gi) and link PLN-2026-0418",
        conf: "98%",
        log: "Trace saw exit 137 and 3.8GiB of worker headroom. The change is staged for review.",
      },
      cidr: {
        rule: "Manual run · reconcile_network_drift",
        target: "edge-west-02 · egress-relay-lock",
        action: "Stage a network policy /24 lockstep change",
        conf: "96%",
        log: "Detected a /16 drift against the restricted baseline and staged a change request.",
      },
      frame: {
        rule: "Manual run · resize_transfer_frames",
        target: "spoke-diode-hall-b · RUN-8844",
        action: "Slice the archive into 16 MB frames with a digest per chunk",
        conf: "95%",
        log: "The one-way path dropped a 32 MB frame. A 16 MB chunk size is staged.",
      },
    }[state.traceFixture] || {
      rule: "Manual run",
      target: "edge-west-02",
      action: "Stage a reviewed change",
      conf: "90%",
      log: "Staged for review.",
    };
    state.proposals.unshift({
      id: nextId,
      status: "Pending Review",
      ...made,
    });
    state.traceBanner = `Staged ${nextId} for review. No live change was applied.`;
    ctx.recordAudit("proposal.staged");
    paint();
  }

  function setProposalStatus(id, status) {
    const row = state.proposals.find((item) => item.id === id);
    if (!row) return;
    row.status = status;
    ctx.recordAudit(
      status === "Applied" ? "proposal.accepted" : "proposal.rejected",
    );
    paint();
  }

  function switchTerminalProfile(val) {
    state.terminalProfile = val;
    state.terminalLines.push(`[Switched terminal profile -> ${val}]`);
    paint();
  }

  function approveTimedAccess() {
    state.accessStatus = "Granted (30m)";
    state.terminalLines.push(
      "[Grant JITA-204 approved by peer admin M. Chen. Pod restart permission is active.]",
    );
    ctx.recordAudit("access.peer_approved");
    paint();
  }

  function revokeTimedAccess() {
    state.accessStatus = "Revoked";
    state.terminalLines.push(
      "[Grant JITA-204 revoked. Terminal returned to read-only.]",
    );
    ctx.recordAudit("access.revoked");
    paint();
  }

  function runTerminalCommand(preset) {
    const command = String(preset || state.terminalDraft || "").trim();
    if (!command) return;
    let out = "ok: diagnostic query ran under the read-only session.";
    if (command.includes("top")) {
      out =
        "POD                               CPU(cores)   MEMORY(bytes)\ningress-gateway-7d9c4b8f-x92lm    412m         764Mi / 768Mi (99.4%)";
    } else if (command.includes("describe")) {
      out =
        "Last State: Terminated (Reason: OOMKilled, Exit Code: 137)\nEvents: Warning BackOff  restarting failed container ingress-proxy";
    } else if (command.includes("delete")) {
      if (state.accessStatus !== "Granted (30m)") {
        out =
          "Error (Forbidden): the base terminal session is read-only. Ask a peer approver for the 30-minute pod restart grant.";
      } else {
        out =
          'pod "ingress-gateway-7d9c4b8f-x92lm" deleted (grant JITA-204 recorded)';
        ctx.recordAudit("terminal.pod_deleted");
      }
    }
    state.terminalLines.push(`$ ${command}`, out);
    state.terminalDraft = "";
    paint();
  }

  function renderRecalls(root) {
    if (state.liveHub) {
      state.liveHub.renderRecall(root);
      return;
    }
    const endDisabled =
      state.recallMode === "SINGLE" || state.recallMode === "OPEN_ENDED";
    const endLabel =
      state.recallMode === "RANGE_WITH_EXCLUSION"
        ? "Excluded backport"
        : "Fixed upper release";
    const registries = [
      {
        uri: "registry.example.internal/uds-core",
        scope: "Online and staging (26 environments)",
        probe: "Available",
        tone: "green",
        detail: "4/4 digests verified (48ms)",
      },
      {
        uri: "spoke-west-mirror.local:5000/uds-core",
        scope: "Store-and-forward (14 environments)",
        probe: "Available",
        tone: "green",
        detail: "Mirrored via spoke-west-relay",
      },
      state.skipDisconnectedProbe
        ? {
            uri: "hall-b-diode.local/uds-core",
            scope: "Disconnected (6 environments)",
            probe: "Unchecked",
            tone: "blue",
            detail:
              "Probe skipped. A disconnected site is not expected to answer the Hub.",
          }
        : {
            uri: "hall-b-diode.local/uds-core",
            scope: "Disconnected (6 environments)",
            probe: "Unavailable",
            tone: "red",
            detail: "Probe timed out. The one-way path blocks the check.",
          },
    ];
    const products = [
      "ingress-gateway",
      "uds-core",
      "telemetry-collector",
      "runtime-admission",
    ];

    root.innerHTML = `
      <div class="space-y-2.5 flex flex-col">
        ${ctx.rmApiStrip("EXAMPLE", "Recall configurator, suppression clock, registry probes", "ConduitArtifact")}
        ${state.recallBanner ? `<div class="so-warn-card p-2.5 text-[11.5px] text-[#fbbf24]">${esc(state.recallBanner)}</div>` : ""}
        ${sectionCard(
          "Issue a scoped recall",
          `
            ${entityTitle(
              "Roll-off configurator",
              "Cancels open upgrade requests and schedules roll-off ahead of ordinary upgrades.",
              `${ctx.pill("Supersedes promotion soak", "red")} ${ctx.ontChip("ConduitWorkProfile", 4, 1, "rm06-promotion-pipe")}`,
            )}
            <div class="so-grid-4 gap-2.5 mt-3">
              <label class="space-y-1">
                <span class="text-[10px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Product</span>
                <select data-onchange="state.recallProduct=this.value" class="so-select code-id w-full">
                  ${products.map((name) => `<option value="${name}" ${state.recallProduct === name ? "selected" : ""}>${name}</option>`).join("")}
                </select>
              </label>
              <label class="space-y-1">
                <span class="text-[10px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Range mode</span>
                <select data-onchange="state.recallMode=this.value; renderMainWorkspace()" class="so-select w-full">
                  <option value="SINGLE" ${state.recallMode === "SINGLE" ? "selected" : ""}>Single release</option>
                  <option value="CLOSED_RANGE" ${state.recallMode === "CLOSED_RANGE" ? "selected" : ""}>Closed range [start, fixed)</option>
                  <option value="OPEN_ENDED" ${state.recallMode === "OPEN_ENDED" ? "selected" : ""}>Open-ended (start and later)</option>
                  <option value="RANGE_WITH_EXCLUSION" ${state.recallMode === "RANGE_WITH_EXCLUSION" ? "selected" : ""}>Range with a backport excluded</option>
                </select>
              </label>
              <label class="space-y-1">
                <span class="text-[10px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Starting version</span>
                <input type="text" value="${esc(state.recallStart)}" data-oninput="state.recallStart=this.value" class="so-input code-id w-full" />
              </label>
              <label class="space-y-1">
                <span class="text-[10px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">${endLabel}</span>
                <input type="text" value="${esc(state.recallEnd)}" ${endDisabled ? "disabled" : ""} data-oninput="state.recallEnd=this.value" class="so-input code-id w-full ${endDisabled ? "opacity-40" : ""}" />
              </label>
            </div>
            <label class="space-y-1 block mt-2.5">
              <span class="text-[10px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Reason</span>
              <input type="text" value="${esc(state.recallNote)}" data-oninput="state.recallNote=this.value" class="so-input w-full" />
            </label>
            <div class="mt-3">${ctx.governedBtn({
              label: "Issue recall and prioritize roll-off",
              role: "Release editor",
              auditEvent: "recall.issued",
              onClickJs: "issueRecall()",
              cls: "so-btn so-btn-danger",
              iconName: "recall",
            })}</div>
          `,
          {
            subtitle: "Product, range mode, and version bounds. Example data.",
          },
        )}
        ${sectionCard(
          `Active recalls (${state.recalls.length})`,
          `<div class="so-table-scroll -mx-3.5 -mb-3"><table class="so-table">
            <thead><tr><th>Recall</th><th>Product</th><th>Range mode</th><th>Version expression</th><th>Reason</th><th style="text-align:right">Roll-off envs</th><th>Status</th><th></th></tr></thead>
            <tbody>
              ${state.recalls
                .map(
                  (row, idx) => `<tr>
                <td class="code-id font-semibold text-[#f87171]">${esc(row.id)}</td>
                <td class="code-id text-white">${esc(row.prod)}</td>
                <td><span class="so-pill code-id">${esc(row.mode)}</span></td>
                <td class="code-id text-[#eab854]">${esc(row.expr)}</td>
                <td class="text-[#9aa5b4]">${esc(row.why)}</td>
                <td class="tnum font-semibold" style="text-align:right">${row.envs}</td>
                <td>${ctx.pill(row.status, row.tone)}</td>
                <td><button type="button" data-onclick="toggleRecall(${idx})" class="so-btn so-btn-sm">${row.status === "Reverted" ? "Re-activate" : "Revert"}</button></td>
              </tr>`,
                )
                .join("")}
            </tbody>
          </table></div>`,
          {
            subtitle:
              "Roll-off plans skip the maintenance window unless that window is required.",
          },
        )}
        <div class="so-grid-2 gap-3">
          ${sectionCard(
            "Vulnerability suppressions",
            `<div class="space-y-2">
              ${state.suppressions
                .map(
                  (row, idx) => `<div class="so-card p-2.5 space-y-1.5">
                <div class="flex flex-wrap items-center justify-between gap-1.5">
                  <span class="flex flex-wrap items-center gap-1.5"><strong class="code-id text-white">${esc(row.cve)}</strong>${ctx.pill(row.sev, row.sTone)}<span class="code-id text-[10.5px] text-[#60a5fa]">${esc(row.pkg)}</span></span>
                  ${slaClock(row.slaLeft, row.slaTotal)}
                </div>
                <div class="text-[11px] text-[#9aa5b4]">${esc(row.why)} Clock ${row.slaLeft}d of ${row.slaTotal}d.</div>
                <div class="flex flex-wrap items-center justify-between gap-2 pt-1 border-t border-[#1b242d]">
                  ${ctx.pill(row.approved ? "Approved" : "Pending approval", row.approved ? "green" : "amber")}
                  <button type="button" data-onclick="toggleSuppression(${idx})" class="so-btn so-btn-sm">${row.approved ? "Revoke" : "Approve"}</button>
                </div>
              </div>`,
                )
                .join("")}
            </div>`,
            { right: ctx.pill("60d medium / 14d high", "blue") },
          )}
          ${sectionCard(
            "Registry availability",
            `<label class="inline-flex items-center gap-1.5 text-[11px] text-[#8b96a5] cursor-pointer mb-2">
              <input type="checkbox" ${state.skipDisconnectedProbe ? "checked" : ""} data-onchange="state.skipDisconnectedProbe=this.checked; renderMainWorkspace()" />
              <span>Disconnected site: registry probe not expected</span>
            </label>
            <div class="space-y-2">
              ${registries
                .map(
                  (row) => `<div class="so-card p-2.5 space-y-1">
                <div class="flex flex-wrap items-center justify-between gap-1.5">
                  <span class="code-id text-[11.5px] font-semibold text-white">${esc(row.uri)}</span>
                  ${ctx.pill(row.probe, row.tone)}
                </div>
                <div class="flex flex-wrap justify-between gap-1 text-[11px] text-[#8b96a5]">
                  <span>${esc(row.scope)}</span>
                  <span class="code-id">${esc(row.detail)}</span>
                </div>
              </div>`,
                )
                .join("")}
            </div>`,
          )}
        </div>
      </div>
    `;
  }

  function renderRamp(root) {
    const environment = selectedEnvironment(
      state.hub?.environments,
      state.releaseEnvironmentId,
    );
    root.innerHTML =
      releaseWavesMarkup(state.hub?.rollouts, state.hub?.rolloutNote) +
      releaseCalendarMarkup(environment?.environment?.maintenance?.overrides) +
      (environment
        ? `<section class="so-panel p-3"><h3>Selected environment: ${esc(environment.environment.id)}</h3></section>` +
          maintenanceMarkup(environment.environment.maintenance)
        : "<p>No selected environment maintenance evidence is available.</p>");
  }

  function renderTerminal(root) {
    root.innerHTML = `<div class="so-view-enter"><div class="so-panel p-4 text-[13px] text-white">session brokering not implemented</div></div>`;
    return;
    const granted = state.accessStatus === "Granted (30m)";
    const profiles = [
      ["conduit-default-shell:6.2", "conduit-default-shell:6.2 (built in)"],
      [
        "conduit-sre-netshoot:1.4.0",
        "conduit-sre-netshoot:1.4.0 (spoke-west-mirror)",
      ],
    ];
    root.innerHTML = `
      <div class="space-y-2.5 flex flex-col min-w-0">
        ${ctx.rmApiStrip("EXAMPLE", "Time-limited terminal and peer-approved access", "ConduitEnvironment")}
        ${sectionCard(
          "Debug terminal · edge-west-02",
          `
            ${entityTitle(
              "Session",
              "Hub-relayed diagnostic session. The base role is read-only.",
              `${compoundTag("TTL", "03h 58m", "green")} ${compoundTag("access", granted ? "Pod restart (30m)" : "Read-only", granted ? "amber" : "blue")}`,
            )}
            <div class="flex flex-wrap items-center gap-2 mt-3">
              <span class="text-[11px] text-[#8b96a5]">Terminal profile</span>
              <select data-onchange="switchTerminalProfile(this.value)" class="so-select code-id min-w-0 max-w-full">
                ${profiles.map(([val, label]) => `<option value="${val}" ${state.terminalProfile === val ? "selected" : ""}>${label}</option>`).join("")}
                <option value="disconnected-forensics:2.0" disabled>disconnected-forensics:2.0 (registry not installed here)</option>
              </select>
            </div>
            <div class="flex flex-wrap gap-1.5 mt-3">
              <button type="button" data-onclick="runTerminalCommand('kubectl top pod -n conduit-system')" class="so-btn so-btn-sm">kubectl top pod</button>
              <button type="button" data-onclick="runTerminalCommand('kubectl describe pod ingress-gateway-7d9c4b8f-x92lm')" class="so-btn so-btn-sm">kubectl describe pod</button>
              <button type="button" data-onclick="runTerminalCommand('kubectl delete pod ingress-gateway-7d9c4b8f-x92lm -n conduit-system')" class="so-btn so-btn-sm ${granted ? "so-btn-danger" : "so-btn-outline-danger"}">kubectl delete pod</button>
            </div>
            <pre class="m-0 mt-3 p-3 rounded bg-[#060a0d] border border-[#1c2630] code-id text-[11px] text-[#dce4ee] overflow-auto leading-relaxed max-h-[240px]">${state.terminalLines.map(esc).join("\n")}</pre>
            <div class="flex flex-wrap sm:flex-nowrap gap-2 mt-3">
              <input type="text" value="${esc(state.terminalDraft)}" data-oninput="state.terminalDraft=this.value" data-onkeydown="if(event.key==='Enter'){runTerminalCommand();}" placeholder="Diagnostic command for edge-west-02" class="so-input code-id flex-1 min-w-0" />
              <button type="button" data-onclick="runTerminalCommand()" class="so-btn so-btn-blue shrink-0">Run</button>
            </div>
          `,
          {
            subtitle:
              "Four-hour session. Elevated commands need a separate grant.",
            right: keyCombo("enter"),
          },
        )}
        ${sectionCard(
          "Timed access · JITA-204",
          `
            ${entityTitle(
              "Peer approval required",
              "Requested by J. Alvarez for policy k8s-pod-restart-30m. The requester cannot approve it.",
              ctx.pill(
                state.accessStatus,
                granted
                  ? "green"
                  : state.accessStatus === "Revoked"
                    ? "red"
                    : "amber",
              ),
            )}
            <div class="flex flex-wrap gap-2 mt-3">
              ${ctx.governedBtn({
                label: "Self-approve (J. Alvarez)",
                role: "Terminal user",
                auditEvent: "access.self_approve_blocked",
                onClickJs: "",
                cls: "so-btn so-btn-sm",
                iconName: "lock",
                blockedReason:
                  "The requester cannot approve their own timed access grant",
              })}
              ${
                granted
                  ? `<button type="button" data-onclick="revokeTimedAccess()" class="so-btn so-btn-sm so-btn-danger">Revoke grant</button>`
                  : `<button type="button" data-onclick="approveTimedAccess()" class="so-btn so-btn-sm so-btn-emerald">Approve as peer (M. Chen)</button>`
              }
            </div>
          `,
          { subtitle: "Grant lasts 30 minutes and reaches the open terminal." },
        )}
      </div>
    `;
  }

  function renderTrace(root) {
    if (renderLiveProposal(root, state)) return;
    const blocks = [
      {
        id: "input",
        step: "1. Input",
        badge: "Plan",
        desc: "failedPlan PLN-2026-0416 · environment edge-west-02",
      },
      {
        id: "query",
        step: "2. Reads",
        badge: "Reported state",
        desc: "Reported memory, active holds, and worker headroom",
      },
      {
        id: "llm",
        step: "3. Assistant step",
        badge: "ops-standard-32k",
        desc: "Tools: read_reported_state, check_recalls, stage_change_request",
      },
      {
        id: "action",
        step: "4. Staged output",
        badge: state.traceStaged ? "Held for review" : "Direct edit",
        desc: "create_remediation_change_request",
      },
    ];
    const traces = {
      input:
        "[Input] failedPlan = PLN-2026-0416 (FAILED, exit 137)\n[Input] environment = edge-west-02 (store-and-forward, spoke-west-relay)",
      query:
        '[Read] reported state edge-west-02/ingress-gateway\n  limits.memory = "768Mi", peakRss = "764Mi", restarts = 4\n[Read] override OVR-117 holds upgrades after the failure',
      llm: "[Step 1] ingress-proxy exited 137 during replay (764Mi / 768Mi).\n[Tool] check_cluster_headroom(edge-west-02) -> allocatable 3.8GiB, minimum 2.4GiB, pass\n[Tool] stage_change_request(limits.memory = 1Gi)\n[Check] shape valid, disconnected path untouched, 612 tokens, 840ms",
      action: `Action: create_remediation_change_request\n  mode: ${state.traceScope}\n  staged: ${state.traceStaged}\n  patch:\n    resources.requests.memory: 512Mi\n    resources.limits.memory: 1Gi\n  human review required (2 approvers)`,
    };
    root.innerHTML = `
      <div class="space-y-2.5 h-full flex flex-col">
        <div class="so-warn-card p-2 text-[12px] text-[#eab854]">Example data</div>
        ${ctx.rmApiStrip("EXAMPLE", "Step-by-step tool trace for a proposed change", "ConduitChangeRequest")}
        ${
          state.traceBanner
            ? `<div class="so-opt-card p-2.5 flex flex-wrap items-center justify-between gap-2 text-[11.5px]">
          <span class="code-id text-[#34d399]">${esc(state.traceBanner)}</span>
          <button type="button" data-onclick="state.proposalTab='proposals'; switchScreen('pt02-proposals-evals')" class="so-btn so-btn-sm so-btn-emerald">Review in Proposals</button>
        </div>`
            : ""
        }
        <div class="so-tri-layout so-resp-stack flex-1 min-h-0 flex gap-3 items-stretch">
          <div class="so-resp-full so-panel w-[268px] shrink-0 flex flex-col overflow-hidden">
            <div class="px-3 py-2.5 border-b border-[#1c242c] flex items-center justify-between gap-2 bg-[#0e1418]">
              <span class="text-[12px] font-semibold text-white">1. Steps</span>
              <label class="inline-flex items-center gap-1.5 text-[10.5px] text-[#34d399] cursor-pointer">
                <input type="checkbox" ${state.traceStaged ? "checked" : ""} data-onchange="state.traceStaged=this.checked; renderMainWorkspace()" />
                <span>Stage writes</span>
              </label>
            </div>
            <div class="p-2.5 space-y-2 overflow-y-auto flex-1">
              ${blocks
                .map(
                  (
                    block,
                  ) => `<div data-onclick="setTraceBlock('${block.id}')" class="so-card so-card-interactive p-2.5 cursor-pointer space-y-1 ${state.traceBlock === block.id ? "!border-[#5c9ce6] bg-[#15202b]" : ""}">
                <div class="flex flex-wrap items-center justify-between gap-1">
                  <span class="font-semibold text-[11.5px] text-white">${block.step}</span>
                  ${ctx.pill(block.badge, block.id === "action" ? "green" : "blue")}
                </div>
                <div class="code-id text-[10.5px] text-[#8b96a5]">${block.desc}</div>
              </div>`,
                )
                .join("")}
            </div>
          </div>
          <div class="so-panel flex-1 min-w-0 flex flex-col overflow-hidden">
            <div class="px-3.5 py-2.5 border-b border-[#1c242c] flex flex-wrap items-center justify-between gap-2 bg-[#0e1418]">
              ${entityTitle("2. Tool trace", "", `${ctx.pill("612 tok · 840ms", "violet")} ${keyCombo("mod+enter")}`)}
              <select data-onchange="state.traceScope=this.value; renderMainWorkspace()" aria-label="Execution mode" class="so-select !h-[23px] text-[11px]">
                <option value="Project" ${state.traceScope === "Project" ? "selected" : ""}>Project</option>
                <option value="User" ${state.traceScope === "User" ? "selected" : ""}>User</option>
              </select>
            </div>
            <div class="p-3 space-y-3 overflow-y-auto flex-1">
              <div class="flex flex-wrap gap-1.5">
                ${ctx.ontChip("ConduitPlan", 1, 0, "rm03-plan-detail")}
                ${ctx.toolChip("check_cluster_headroom", 1, 0)}
                ${ctx.agentChip("stage_change_request", 1, 1)}
              </div>
              <pre class="m-0 p-3 rounded bg-[#080c0f] border border-[#1c242c] code-id text-[11px] text-[#dce4ee] overflow-x-auto leading-relaxed">${esc(traces[state.traceBlock] || traces.llm)}</pre>
              <div class="so-opt-card p-3 space-y-1">
                <div class="font-semibold text-[11px] text-[#34d399]">Guardrail check</div>
                <div class="text-[11.5px] text-[#dce4ee]">The assistant cannot run kubectl or change the cluster. The output is checked against the action spec and held for a person to review.</div>
              </div>
            </div>
          </div>
          <div class="so-resp-full so-panel w-[262px] shrink-0 p-3 flex flex-col justify-between gap-3">
            <div class="space-y-2.5">
              ${entityTitle("3. Run", "Stages a proposal. It does not apply it.")}
              <label class="space-y-1 block">
                <span class="text-[10px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Fixture</span>
                <select data-onchange="state.traceFixture=this.value; renderMainWorkspace()" class="so-select w-full">
                  <option value="oom" ${state.traceFixture === "oom" ? "selected" : ""}>PLN-2026-0416 (exit 137)</option>
                  <option value="cidr" ${state.traceFixture === "cidr" ? "selected" : ""}>edge-west-02 (CIDR drift)</option>
                  <option value="frame" ${state.traceFixture === "frame" ? "selected" : ""}>spoke-diode-hall-b (frame lag)</option>
                </select>
              </label>
              <div class="so-card p-2.5 space-y-1 text-[11px] text-[#8b96a5]">
                <div>Version <strong class="code-id text-white">v2.3</strong></div>
                <div>Eval pass <strong class="tnum text-[#34d399]">99.0% (24/24)</strong></div>
                <div>Automation <strong class="code-id text-[#60a5fa]">auto-oom-triage</strong></div>
              </div>
            </div>
            <div class="space-y-2 pt-2.5 border-t border-[#1c242c]">
              <button type="button" data-onclick="runProposalTrace()" class="so-btn so-btn-blue w-full">${ctx.ic("play")} <span>Run trace and stage proposal</span></button>
              <button type="button" data-onclick="state.proposalTab='evals'; switchScreen('pt02-proposals-evals')" class="so-btn w-full">${ctx.ic("flask")} <span>Open evals</span></button>
            </div>
          </div>
        </div>
      </div>
    `;
  }

  function renderProposals(root) {
    const pending = state.proposals.filter(
      (row) => row.status === "Pending Review",
    ).length;
    const applied = state.proposals.filter(
      (row) => row.status === "Applied",
    ).length;
    const shown = state.proposals.filter(
      (row) =>
        state.proposalFilter === "ALL" || row.status === state.proposalFilter,
    );
    const cases = [
      [
        "EV-01",
        "Memory remediation stays inside node headroom",
        "allocatable >= 2.4GiB",
        "100%",
        "840ms",
        "Pass",
        "green",
      ],
      [
        "EV-02",
        "No direct push to a one-way spoke",
        "mode == one-way",
        "100%",
        "610ms",
        "Pass",
        "green",
      ],
      [
        "EV-03",
        "Recall roll-off comes before a soak hold",
        "Recall supersedes hold",
        state.evalBoost ? "100%" : "96%",
        "920ms",
        "Pass",
        "green",
      ],
      [
        "EV-04",
        "A short quorum stages a proposal instead of applying it",
        "Review required",
        "100%",
        "530ms",
        "Pass",
        "green",
      ],
    ];
    const models = [
      [
        "ops-standard-32k (active)",
        "99.0%",
        "810ms",
        "$0.42 / 100 runs",
        "Recommended",
        "green",
      ],
      [
        "ops-fast-8k",
        "94.5%",
        "340ms",
        "$0.11 / 100 runs",
        "Fast triage",
        "blue",
      ],
      [
        "local-edge-14b (disconnected site)",
        "96.8%",
        "690ms",
        "$0.00 (on site)",
        "Disconnected",
        "violet",
      ],
    ];
    const recorded = recordedGateRows(state);
    const recordedTable = recorded.length
      ? `<div class="so-panel p-3 space-y-2">
          <div class="text-[13px] font-semibold text-white">Recorded gate results</div>
          <p class="text-[12px] text-[#8b96a5]">Recorded by the packaging assessment. No model call.</p>
          <table class="so-table"><thead><tr><th>Run</th><th>Gate</th><th>Outcome</th><th>Reason</th></tr></thead><tbody>
            ${recorded.map((row) => `<tr><td class="code-id">${esc(row.run)}</td><td>${esc(row.gate)}</td><td>${esc(row.outcome)}</td><td class="text-[#8b96a5]">${esc(row.reason)}</td></tr>`).join("")}
          </tbody></table>
        </div>`
      : "";
    root.innerHTML = `
      <div class="space-y-3 flex flex-col">
        ${recordedTable}
        <div class="so-warn-card p-2 text-[12px] text-[#eab854]">Example data</div>
        ${ctx.rmApiStrip("EXAMPLE", "Staged proposals awaiting review, and eval results", "ConduitPlan")}
        <div class="so-panel p-3 flex flex-wrap items-center justify-between gap-2">
          ${segmentedCtrl(
            [
              ["proposals", `Proposals (${pending} pending)`],
              ["evals", "Evals"],
            ],
            state.proposalTab,
            "state.proposalTab='__VAL__'; renderMainWorkspace()",
          )}
          <div class="flex flex-wrap items-center gap-2">
            <button type="button" data-onclick="switchScreen('pt01-proposal-trace')" class="so-btn so-btn-sm">Back to Proposal Trace</button>
            ${state.proposalTab === "evals" ? `<button type="button" data-onclick="state.evalBoost=true; recordAudit('evals.suite_ran'); renderMainWorkspace()" class="so-btn so-btn-sm so-btn-emerald">${ctx.ic("play")} <span>Run eval suite</span></button>` : ""}
          </div>
        </div>
        ${
          state.proposalTab === "proposals"
            ? `
          <div class="so-grid-3 gap-2.5">
            <div class="so-card p-3"><div class="text-[10px] uppercase text-[#6d7885]">Watchers</div><div class="text-[15px] font-bold text-white tnum mt-1">3</div><div class="text-[11px] text-[#34d399] mt-0.5">Effect: stage for review</div></div>
            <div class="so-warn-card p-3"><div class="text-[10px] uppercase text-[#eab854]">Pending review</div><div class="text-[15px] font-bold text-[#eab854] tnum mt-1">${pending}</div><div class="text-[11px] text-[#8b96a5] mt-0.5">No unreviewed writes</div></div>
            <div class="so-opt-card p-3"><div class="text-[10px] uppercase text-[#34d399]">Applied</div><div class="text-[15px] font-bold text-[#34d399] tnum mt-1">${applied}</div><div class="text-[11px] text-[#8b96a5] mt-0.5">Each decision is recorded</div></div>
          </div>
          ${sectionCard(
            "Proposals awaiting review",
            `<div class="flex flex-wrap gap-1.5 mb-3">
              ${["ALL", "Pending Review", "Applied", "Rejected"].map((name) => `<button type="button" data-onclick="state.proposalFilter='${name}'; renderMainWorkspace()" class="so-btn so-btn-sm ${state.proposalFilter === name ? "so-btn-blue" : ""}">${name}</button>`).join("")}
            </div>
            <div class="space-y-2.5">
              ${shown
                .map(
                  (
                    row,
                  ) => `<div class="${row.status === "Pending Review" ? "so-warn-card" : row.status === "Applied" ? "so-opt-card" : "so-card"} p-3 space-y-2">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="code-id text-[13px] font-bold text-white">${esc(row.id)}</span>
                    <span class="font-semibold text-[12.5px] text-[#dce4ee]">${esc(row.target)}</span>
                    ${ctx.pill(row.status, row.status === "Applied" ? "green" : row.status === "Rejected" ? "red" : "amber")}
                    ${compoundTag("confidence", row.conf, "blue")}
                  </div>
                  <span class="code-id text-[10.5px] text-[#60a5fa]">${esc(row.rule)}</span>
                </div>
                <div class="text-[12px] font-medium text-white">${esc(row.action)}</div>
                <div class="p-2 rounded bg-[#080c0f] border border-[#1c242c] code-id text-[10.5px] text-[#9aa5b4]">Decision log: ${esc(row.log)}</div>
                ${
                  row.status === "Pending Review"
                    ? `<div class="flex flex-wrap justify-end gap-2">
                  <button type="button" data-onclick="switchScreen('pt01-proposal-trace')" class="so-btn so-btn-sm">Open trace</button>
                  <button type="button" data-onclick="setProposalStatus('${esc(row.id)}','Rejected')" class="so-btn so-btn-sm so-btn-outline-danger">Reject</button>
                  <button type="button" data-onclick="setProposalStatus('${esc(row.id)}','Applied')" class="so-btn so-btn-sm so-btn-emerald">Accept</button>
                </div>`
                    : ""
                }
              </div>`,
                )
                .join("")}
            </div>`,
          )}
        `
            : `
          ${sectionCard(
            `Eval results · triage_failed_release_plan`,
            `<div class="so-table-scroll -mx-3.5 -mb-3"><table class="so-table">
              <thead><tr><th>Case</th><th>Check</th><th>Guardrail</th><th style="text-align:right">Score</th><th style="text-align:right">Latency</th><th>Verdict</th><th></th></tr></thead>
              <tbody>
                ${cases
                  .map(
                    (row) => `<tr>
                  <td class="code-id font-semibold text-[#60a5fa]">${row[0]}</td>
                  <td class="text-white">${row[1]}</td>
                  <td><span class="so-pill code-id">${row[2]}</span></td>
                  <td class="code-id" style="text-align:right">${row[3]}</td>
                  <td class="code-id text-[#8b96a5]" style="text-align:right">${row[4]}</td>
                  <td>${ctx.pill(row[5], row[6])}</td>
                  <td><button type="button" data-onclick="switchScreen('pt01-proposal-trace')" class="text-[#60a5fa] text-[11px] hover:underline">Trace</button></td>
                </tr>`,
                  )
                  .join("")}
              </tbody>
            </table></div>`,
            {
              subtitle: state.evalBoost
                ? "100.0% after the latest run"
                : "99.0% on the saved run",
              right: ctx.pill("Example data", "amber"),
            },
          )}
          ${sectionCard(
            "Model comparison",
            `<div class="so-table-scroll -mx-3.5 -mb-3"><table class="so-table">
              <thead><tr><th>Profile</th><th style="text-align:right">Pass rate</th><th style="text-align:right">P95</th><th style="text-align:right">Est. cost</th><th>Tier</th></tr></thead>
              <tbody>
                ${models
                  .map(
                    (row) => `<tr>
                  <td class="code-id font-semibold text-white">${row[0]}</td>
                  <td class="code-id text-[#34d399]" style="text-align:right">${row[1]}</td>
                  <td class="code-id" style="text-align:right">${row[2]}</td>
                  <td class="code-id text-[#8b96a5]" style="text-align:right">${row[3]}</td>
                  <td>${ctx.pill(row[4], row[5])}</td>
                </tr>`,
                  )
                  .join("")}
              </tbody>
            </table></div>`,
          )}
        `
        }
      </div>
    `;
  }

  function showcase() {
    return sectionCard(
      "Section",
      `
        ${entityTitle("Entity title", "Release channel edge-cohorts", `${compoundTag("window", state.componentRamp, "blue")} ${keyCombo("mod+k")}`)}
        <div class="mt-2">${segmentedCtrl(
          [
            ["off", "Off"],
            ["1h", "1h"],
            ["3h", "3h"],
            ["6h", "6h"],
          ],
          state.componentRamp,
          "state.componentRamp='__VAL__'; renderMainWorkspace()",
        )}</div>
      `,
      {
        subtitle:
          "Section card, compound tag, segmented control, and key combo.",
        right: compoundTag("sample", "DATA", "amber"),
      },
    );
  }

  return {
    screens: [
      {
        id: "rm09-recalls",
        num: 28,
        short: "Recalls",
        tabTitle: "Recalls & Vulnerabilities",
        hasSidebar: true,
        showKpis: false,
        subtitle: "RELEASE RECALLS & VULNERABILITY GATE",
      },
      {
        id: "rm10-ramped-rollout",
        num: 29,
        short: "Ramped Rollouts",
        tabTitle: "Ramped Rollout",
        hasSidebar: true,
        showKpis: true,
        subtitle: "CHANNEL RAMP WINDOW",
      },
      {
        id: "rm11-debug-terminal",
        num: 30,
        short: "Debug Terminal",
        tabTitle: "Terminal · edge-west-02",
        hasSidebar: false,
        showKpis: false,
        appGroup: "plans_gov",
        parentLabel: "Environment Hub",
        parentId: "rm01-env-page",
        subtitle: "DEBUG TERMINAL & TIMED ACCESS",
      },
      {
        id: "pt01-proposal-trace",
        num: 31,
        short: "Proposal Trace",
        tabTitle: "Proposal Trace",
        hasSidebar: true,
        showKpis: false,
        subtitle: "PROPOSAL TRACE",
      },
      {
        id: "pt02-proposals-evals",
        num: 32,
        short: "Proposals & Evals",
        tabTitle: "Proposals & Evals",
        hasSidebar: true,
        showKpis: false,
        subtitle: "PROPOSALS & EVALS",
      },
    ],
    navItems: [
      { label: "Ramped Rollouts", icon: "branch", id: "rm10-ramped-rollout" },
      { label: "Recalls", icon: "recall", id: "rm09-recalls" },
    ],
    toolApps: [
      { label: "Proposal Trace", icon: "sparkle", id: "pt01-proposal-trace" },
      { label: "Proposals & Evals", icon: "flask", id: "pt02-proposals-evals" },
      { label: "Debug Terminal", icon: "terminal", id: "rm11-debug-terminal" },
    ],
    proposalPeers: [
      { id: "pt01-proposal-trace", label: "Proposal Trace" },
      { id: "pt02-proposals-evals", label: "Proposals & Evals" },
    ],
    renderers: {
      "rm09-recalls": renderRecalls,
      "rm10-ramped-rollout": renderRamp,
      "rm11-debug-terminal": renderTerminal,
      "pt01-proposal-trace": renderTrace,
      "pt02-proposals-evals": renderProposals,
    },
    showcase,
    extendState() {
      Object.assign(state, {
        recallMode: "CLOSED_RANGE",
        recallProduct: "ingress-gateway",
        recallStart: "1.23.0",
        recallEnd: "1.23.4",
        recallNote:
          "Burst-replay memory regression and CVE-2026-41820. Roll forward to 1.23.4.",
        recallBanner: "",
        skipDisconnectedProbe: true,
        recalls: [
          {
            id: "RCL-2026-019",
            prod: "ingress-gateway",
            mode: "CLOSED_RANGE",
            expr: ">= 1.23.0, < 1.23.4 (excl. 1.23.2-hotfix)",
            why: "Burst-replay memory use and CVE-2026-41820",
            envs: 5,
            status: "Active Roll-Off",
            tone: "amber",
          },
          {
            id: "RCL-2026-014",
            prod: "telemetry-collector",
            mode: "SINGLE",
            expr: "== 0.9.3",
            why: "Recalled after a container scan (zlib-ng)",
            envs: 0,
            status: "Contained",
            tone: "green",
          },
        ],
        suppressions: [
          {
            id: "SUP-408",
            cve: "CVE-2026-30911",
            pkg: "golang.org/x/net@0.29.0",
            sev: "Medium",
            sTone: "amber",
            slaLeft: 46,
            slaTotal: 60,
            approved: true,
            why: "HTTP/3 listener is disabled in the Spoke chart values.",
          },
          {
            id: "SUP-412",
            cve: "CVE-2026-38104",
            pkg: "alpine/libcrypto3@3.3.2",
            sev: "High",
            sTone: "red",
            slaLeft: 11,
            slaTotal: 14,
            approved: false,
            why: "Unreachable because the legacy cipher suite is disabled in the Spoke chart values.",
          },
        ],
        rampWindow: "3h",
        rampPaused: false,
        componentRamp: "3h",
        stagedEdits: [
          {
            object: "Release channel",
            key: "edge-cohorts",
            field: "rampWindow",
            from: "off",
            to: "3h",
          },
          {
            object: "Environment",
            key: "edge-west-02",
            field: "memoryCeiling",
            from: "768Mi",
            to: "1Gi",
          },
        ],
        rampCohorts: [
          {
            id: "edge-east-01",
            canary: false,
            st: "Upgraded",
            tone: "green",
            note: "Slot 1 · converged in 02m 14s",
          },
          {
            id: "metro-core-03",
            canary: false,
            st: "Upgrading",
            tone: "blue",
            note: "Slot 2 · applying release r42",
          },
          {
            id: "edge-west-01",
            canary: true,
            st: "Rate Limited",
            tone: "amber",
            note: "Queued by the 3h ramp. Order is not fixed.",
          },
          {
            id: "edge-west-02",
            canary: false,
            st: "Rate Limited",
            tone: "amber",
            note: "Queued in the 3h window. Pauses if the maintenance window closes.",
          },
        ],
        terminalProfile: "conduit-sre-netshoot:1.4.0",
        accessStatus: "Pending peer approval",
        terminalDraft: "",
        terminalLines: [
          "hub-session://edge-west-02 [profile=conduit-sre-netshoot:1.4.0, access=read-only, ttl=03h58m]",
          "$ kubectl get pods -n conduit-system",
          "NAME                               READY   STATUS      RESTARTS      AGE",
          "ingress-gateway-7d9c4b8f-x92lm     1/2     OOMKilled   4 (82s ago)   19m",
          "spoke-relay-agent-6bb9d4-2k8pq     1/1     Running     0             6d",
        ],
        traceStaged: true,
        traceScope: "Project",
        traceBlock: "llm",
        traceFixture: "oom",
        traceRuns: 1,
        traceBanner: "",
        proposalTab: "proposals",
        proposalFilter: "ALL",
        evalBoost: false,
        proposals: [
          {
            id: "PRP-901",
            rule: "WHEN Plan.status == FAILED (exit 137)",
            target: "edge-west-02 · ingress-gateway",
            action:
              "Stage CR-308 (limits.memory: 768Mi -> 1Gi) and hold OVR-117",
            conf: "97%",
            status: "Pending Review",
            log: "Read reported state (peak 764Mi) and plan PLN-2026-0416. Worker headroom 3.8GiB is above the 2.4GiB policy.",
          },
          {
            id: "PRP-899",
            rule: "WHEN Environment.status == DRIFT_DETECTED",
            target: "edge-west-02 · egress-relay-lock",
            action: "Reconcile network policy CIDR /16 to /24 on PLN-2026-0418",
            conf: "95%",
            status: "Pending Review",
            log: "Matched an out-of-band edit against the restricted baseline and staged a lockstep change.",
          },
          {
            id: "PRP-894",
            rule: "WHEN Spoke.spoolLagMb > 12",
            target: "spoke-diode-hall-b · uds-core-0.31.2",
            action: "Re-slice the differential archive into 16 MB frames",
            conf: "94%",
            status: "Applied",
            log: "A 32 MB frame timed out on the one-way path. Applied CR-309 and checked the chunk digest.",
          },
        ],
      });
    },
    bind(win) {
      win.issueRecall = issueRecall;
      win.toggleRecall = toggleRecall;
      win.toggleSuppression = toggleSuppression;
      win.setRampWindow = setRampWindow;
      win.bypassRamp = bypassRamp;
      win.setTraceBlock = setTraceBlock;
      win.runProposalTrace = runProposalTrace;
      win.setProposalStatus = setProposalStatus;
      win.switchTerminalProfile = switchTerminalProfile;
      win.approveTimedAccess = approveTimedAccess;
      win.revokeTimedAccess = revokeTimedAccess;
      win.runTerminalCommand = runTerminalCommand;
    },
    onKey(ev) {
      if (
        (ev.metaKey || ev.ctrlKey) &&
        ev.key === "Enter" &&
        state.screen === "pt01-proposal-trace"
      ) {
        ev.preventDefault();
        runProposalTrace();
        return true;
      }
      return false;
    },
  };
}
