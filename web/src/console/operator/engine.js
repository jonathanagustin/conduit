/* Ported from the release console. Example rows stay labeled in the top bar. */
import {
  readPref,
  writePref,
  siteSummary,
  kpisFromHub,
  allLive,
  DOCK_SURFACES,
  DOCK_DEFAULT,
  clampDockWidth,
  dockSurfacesMarkup,
} from "./shell.js";
import { createReleaseGates } from "./release-gates.js";
import { createOntologyGraph } from "./ontology-graph.js";
import { installInlineActions } from "./inline-actions.js";
import { facetsMarkup, envDetailMarkup } from "./env-facets.js";
import { overviewMarkup } from "./command-center.js";
import {
  escapeHtml as escAttr,
  filterPalette,
  paletteItems,
  paletteResultsMarkup,
} from "./palette.js";
import { dayTexture, paintDisc } from "./globe-texture.js";
import { reviewMarkup } from "./change-review.js";
import { runsMarkup } from "./packaging-runs.js";
import { createConduitClient } from "@conduit/ontology-client";
import { createSettingsDialog, THEME_KEY } from "./settings-dialog.js";
import {
  inspectorMarkup,
  loadInspector,
  ontologyRef,
  validateFor,
} from "./object-inspector.js";
import { createAuditLedger } from "./audit-ledger.js";
import { productRows, productsMarkup } from "./release-products.js";
import { createSettings } from "./settings.js";
import { createLiveHub } from "./live-hub.js";
import { createIntelligence } from "./intelligence.js";
import { mountDevWorkspaces } from "./dev-workspaces.js";
import {
  assistantChrome,
  publishAssistantContext,
  rememberResult,
  resultsMarkup,
  resumeWrite,
  runAssistantAction,
} from "./assistant-rail.js";
import {
  attachControl,
  attachmentList,
  bindAttach,
  ensureLoaded as ensureAttachments,
} from "./attachments.js";
import {
  areaObject,
  areaSupplement,
  bindKit,
  objectHeader,
  wavesFromRecords,
} from "./kit.js";

export function mountOperator(host, options) {
  const notifyOperator = () => {
    const mount = host.querySelector("#assistant-thread-mount");
    options.onAssistantMount(state.chatCollapsed ? null : mount);
  };
  host.innerHTML =
    '<!-- ===================================================================== -->\n  <!-- Conduit 44px top bar (kit v6.1 rhythm)                                -->\n  <!-- ===================================================================== -->\n  <header class="cnd-topbar shrink-0 bg-[#0d1317] border-b border-[#1e252b] px-4 flex items-center justify-between gap-4 z-40">\n    <!-- Left: Bold Product Wordmark + Divider + Wide-Tracked Operational Context -->\n    <div class="flex items-center gap-3 min-w-0 shrink-0">\n      <button id="btn-mobile-menu" data-onclick="toggleMobileNav()" class="so-btn so-btn-sm lg:hidden" aria-label="Toggle navigation">\n        <span data-ic="menu"></span>\n      </button>\n      <span data-onclick="switchScreen(\'s13-fleet-overview\')" class="font-bold text-[17px] tracking-[-0.02em] text-[#ffffff] cursor-pointer select-none">Conduit</span>\n      <span class="h-4 w-[1px] bg-[#242e38]"></span>\n      <span id="topbar-subtitle" class="text-[10.5px] font-medium uppercase tracking-[0.14em] text-[#7c8794] hidden sm:inline truncate">FLEET COMMAND CENTER</span>\n    </div>\n\n    <!-- Center: Iconic Conduit Icon + 20px Bold Numeral + Two-Line Label KPI Strip -->\n    <div id="header-kpi-strip" class="topbar-kpi-strip hidden lg:flex items-center gap-4 shrink-0">\n    </div>\n\n    <!-- Right: Search (Cmd+K) + LIVE Badge + Ontology Button + Avatar -->\n    <div class="flex items-center gap-2 shrink-0">\n      <div class="w-[142px] hidden md:flex items-center relative">\n        <span class="absolute left-2.5 text-[#596573] pointer-events-none flex" data-ic="search"></span>\n        <input id="global-search-input" type="text" data-onfocus="openCmdOmnibar()" data-oninput="handleGlobalSearch(this.value)"\n          placeholder="Jump to view..."\n          class="so-input w-full pl-7 pr-9 text-[11px]" />\n        <kbd class="code-id absolute right-1.5 px-1.5 py-0.5 text-[9px] text-[#8b96a5] bg-[#151b1f] border border-[#242e38] rounded-[3px]">&#8984;K</kbd>\n      </div>\n\n      <span class="hidden xl:inline-flex items-center h-[26px] px-2 rounded-[4px] bg-[#2a2012] border border-[#4f391b] text-[9.5px] font-semibold code-id text-[#eab854]" id="example-data-badge" title="Example data">EXAMPLE DATA</span>\n\n      <button type="button" id="theme-toggle" data-onclick="toggleTheme()" class="so-btn !h-[26px]">Light</button>\n\n      <button type="button" id="settings-toggle" data-onclick="toggleSettingsDialog()" class="so-btn !h-[26px] !px-2" title="Settings (Ctrl/Cmd + ,)" aria-label="Settings">&#9881;</button>\n\n      <button data-onclick="switchScreen(\'s19-ontology\')" class="so-btn !h-[26px]">\n        <span data-ic="globe"></span>\n        <span class="hidden sm:inline">Ontology</span>\n      </button>\n\n      <button data-onclick="toggleChatRail()" class="so-btn !h-[26px] !px-2" title="Toggle right chat panel">\n        <span data-ic="panelRight"></span>\n      </button>\n\n      <div class="h-[26px] px-2.5 rounded-[4px] bg-[#151b1f] border border-[#242e38] flex items-center gap-1.5 text-[11px] font-medium text-[#f0f5fa]" id="operator-chip" title="Operator">\n        <span id="operator-initials" class="w-4 h-4 rounded-full bg-[#1f3452] text-[#7cb4ff] text-[9.5px] font-semibold flex items-center justify-center">OP</span>\n        <span id="operator-name" class="inline">Operator</span>\n      </div>\n    </div>\n  </header>\n\n  <!-- ===================================================================== -->\n  <!-- THREE-PANE BODY: Conduit LEFT SIDEBAR + TABBED WORKSPACE + PLAIN CHAT  -->\n  <!-- ===================================================================== -->\n  <div class="flex-1 min-h-0 min-w-0 flex relative overflow-hidden">\n\n    <div id="mobile-nav-backdrop" data-onclick="closeMobileNav()"></div>\n\n    <!-- PANE 1 (LEFT SIDEBAR, 206px — ONLY on Hub screens, calm & spacious like Conduit f_01_18_cc!) -->\n    <aside id="left-nav" class="w-[var(--w-rail)] shrink-0 bg-[#101519] border-r border-[#1e252b] flex flex-col justify-between min-h-0 select-none">\n      <div id="left-nav-sections" class="px-2.5 pt-3.5 space-y-5 overflow-y-auto"></div>\n      <!-- Minimal Conduit Bottom-Left Two-Dot Collapse Indicator (No text, no box!) -->\n      <div class="px-4 py-3 shrink-0 flex flex-col items-start gap-1.5">\n        <button data-onclick="toggleLeftRailCollapse()" class="inline-flex items-center gap-1.5 py-1 opacity-85 hover:opacity-100" title="Expand / collapse sidebar" aria-label="Expand or collapse sidebar">\n          <span class="w-[6px] h-[6px] rounded-full bg-[#34d399] shadow-[0_0_5px_#34d399]"></span>\n          <span class="w-[6px] h-[6px] rounded-full bg-[#1f5c42]"></span>\n        </button>\n        <div id="left-nav-sites" class="text-[10px] text-[#596573]"></div>\n      </div>\n    </aside>\n\n    <!-- PANE 2 (WORKSPACE — Flush below <header> on Hub screens; clean breadcrumb sub-bar on Dedicated App screens!) -->\n    <section class="flex-1 min-w-0 min-h-0 flex flex-col bg-[#0b1113] overflow-hidden">\n      <div id="app-breadcrumb-bar" class="hidden h-[34px] shrink-0 bg-[#0d1317] border-b border-[#1a2229] px-4 items-center justify-between gap-3 z-30"></div>\n      <main id="main-workspace" class="flex-1 min-w-0 min-h-0 overflow-y-auto overflow-x-hidden cnd-workspace"></main>\n    </section>\n\n    <!-- PANE 3 (PLAIN NEUTRAL CHAT DRAWER — Closed by default like Conduit, toggleable from top bar!) -->\n    <aside id="right-chat-rail" class="hidden w-[248px] shrink-0 bg-[#0f1418] border-l border-[#1e252b] flex-col min-h-0"></aside>\n  </div>\n\n  <div id="so-portal-tooltip" class="so-tooltip-bubble hidden"></div>\n  <div id="conduit-modal-root"></div>';

  const ICONS = {
    extLink:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M9.5 2.5h4v4M13.5 2.5L7.5 8.5M6.5 3.5H3.5a1 1 0 0 0-1 1v8a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1v-3"/></svg>',
    arrowLeft:
      '<svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M13 8H3M7 4L3 8l4 4"/></svg>',
    overview:
      '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="8" cy="8" r="6"/><circle cx="8" cy="8" r="2.2"/><path d="M8 2v2M8 12v2M2 8h2M12 8h2"/></svg>',
    delivery:
      '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2 8.5h2.5l1.8-4.5 3.2 8 2-3.5H14"/></svg>',
    develop:
      '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M5.5 4.5L2 8l3.5 3.5M10.5 4.5L14 8l-3.5 3.5M9 2.8L7 13.2"/></svg>',
    approvals:
      '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M8 1.8l5 2v4.3c0 3.3-2.2 5.3-5 6.4-2.8-1.1-5-3.1-5-6.4V3.8l5-2z"/><path d="M5.6 8.1l1.7 1.7 3.3-3.4"/></svg>',
    audit:
      '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="8" cy="8" r="6"/><path d="M8 4.6V8l2.4 1.6"/></svg>',
    settings:
      '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="2.5" y="2.5" width="4.5" height="4.5" rx="1"/><rect x="9" y="2.5" width="4.5" height="4.5" rx="1"/><rect x="2.5" y="9" width="4.5" height="4.5" rx="1"/><rect x="9" y="9" width="4.5" height="4.5" rx="1"/></svg>',
    checkCircle:
      '<svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7"><circle cx="8" cy="8" r="6.2"/><path d="M5.2 8.2l1.9 1.9 3.8-3.9"/></svg>',
    cluster:
      '<svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="2" y="2.5" width="12" height="4" rx="1"/><rect x="2" y="9.5" width="12" height="4" rx="1"/><circle cx="4.5" cy="4.5" r="0.8" fill="currentColor"/><circle cx="4.5" cy="11.5" r="0.8" fill="currentColor"/></svg>',
    cube: '<svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M8 1.8l5.5 3.1v6.2L8 14.2 2.5 11.1V4.9L8 1.8z"/><path d="M2.5 4.9L8 8l5.5-3.1M8 8v6.2"/></svg>',
    shield:
      '<svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M8 1.8l5 2v4.3c0 3.3-2.2 5.3-5 6.4-2.8-1.1-5-3.1-5-6.4V3.8l5-2z"/></svg>',
    warnTri:
      '<svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M8 2.2L14.3 13.5H1.7L8 2.2z"/><path d="M8 6.4v3.2M8 11.5v.6"/></svg>',
    globe:
      '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4"><circle cx="8" cy="8" r="6"/><ellipse cx="8" cy="8" rx="2.8" ry="6"/><path d="M2.2 8h11.6"/></svg>',
    bulb: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M6 12h4M6.5 14h3M8 2a4.5 4.5 0 0 0-2.6 8.2c.4.3.6.8.6 1.3h4c0-.5.2-1 .6-1.3A4.5 4.5 0 0 0 8 2z"/></svg>',
    bolt: '<svg width="10" height="10" viewBox="0 0 16 16" fill="#60a5fa"><path d="M9 1.5L3.5 9h4l-.8 5.5L12.5 7h-4L9 1.5z"/></svg>',
    wrench:
      '<svg width="10" height="10" viewBox="0 0 16 16" fill="#fbbf24"><path d="M13.2 4.8a3.5 3.5 0 0 1-4.6 4.1L4.2 13.3a1.2 1.2 0 0 1-1.7-1.7l4.4-4.4a3.5 3.5 0 0 1 4.1-4.6L9.2 4.4l1.2 1.2 2.8-1.8z"/></svg>',
    ontShield:
      '<svg width="10" height="10" viewBox="0 0 16 16" fill="none" stroke="#34d399" stroke-width="1.8"><circle cx="8" cy="8" r="5.8"/><path d="M5.4 8.2l1.7 1.7 3.5-3.6"/></svg>',
    search:
      '<svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="7" cy="7" r="4.6"/><path d="M10.5 10.5L14 14"/></svg>',
    menu: '<svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M2 4h12M2 8h12M2 12h12"/></svg>',
    check:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 8.3l3.2 3.2L13 4.7"/></svg>',
    cross:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.9"><path d="M4 4l8 8M12 4l-8 8"/></svg>',
    chevDown:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 6l4 4 4-4"/></svg>',
    chevRight:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M6 4l4 4-4 4"/></svg>',
    panelRight:
      '<svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="2" y="2.5" width="12" height="11" rx="1"/><path d="M10 2.5v11"/></svg>',
    refresh:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M13.5 8a5.5 5.5 0 1 1-1.6-3.9L13.5 5.5V2"/><path d="M13.5 5.5H10"/></svg>',
    lock: '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="3.5" y="7" width="9" height="6.5" rx="1"/><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2"/></svg>',
    pin: '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M5 2.5h6l-1 4.5 2 2v1H4v-1l2-2-1-4.5zM8 10v4"/></svg>',
    folder:
      '<svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2 4h4l1.5 1.8H14v7.2H2V4z"/></svg>',
    file: '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4"><path d="M4 2h5.5L13 5.5V14H4V2z"/><path d="M9.5 2v3.5H13"/></svg>',
    paperclip:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M10.5 4.5l-5.2 5.2a1.8 1.8 0 0 0 2.5 2.5l5.6-5.6a3.2 3.2 0 1 0-4.5-4.5L3.3 7.7a4.6 4.6 0 0 0 6.5 6.5l4.2-4.2"/></svg>',
    download:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M8 2.5v8M5 7.8L8 10.8l3-3M3 13.2h10"/></svg>',
    grip: '<svg width="10" height="12" viewBox="0 0 12 16" fill="currentColor"><circle cx="3.5" cy="3.5" r="1.1"/><circle cx="8.5" cy="3.5" r="1.1"/><circle cx="3.5" cy="8" r="1.1"/><circle cx="8.5" cy="8" r="1.1"/><circle cx="3.5" cy="12.5" r="1.1"/><circle cx="8.5" cy="12.5" r="1.1"/></svg>',
    branch:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="5" cy="4" r="1.8"/><circle cx="5" cy="12" r="1.8"/><circle cx="11.5" cy="6" r="1.8"/><path d="M5 5.8v4.4M11.5 7.8c0 2-2.5 2.2-6.5 2.4"/></svg>',
    external:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M9.5 2.5h4v4M13.5 2.5L7.5 8.5M6.5 3.5H3v10h10V10"/></svg>',
    play: '<svg width="10" height="10" viewBox="0 0 16 16" fill="currentColor"><path d="M4 2.8l9.5 5.2L4 13.2V2.8z"/></svg>',
    pause:
      '<svg width="10" height="10" viewBox="0 0 16 16" fill="currentColor"><rect x="3.5" y="3" width="3" height="10"/><rect x="9.5" y="3" width="3" height="10"/></svg>',
    stop: '<svg width="10" height="10" viewBox="0 0 16 16" fill="currentColor"><rect x="3.5" y="3.5" width="9" height="9" rx="1"/></svg>',
    cursor:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M3.5 2.5l9 4.2-4.2 1.4-1.6 4.4-3.2-10z"/></svg>',
    more: '<svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor"><circle cx="3.5" cy="8" r="1.3"/><circle cx="8" cy="8" r="1.3"/><circle cx="12.5" cy="8" r="1.3"/></svg>',
    inbox:
      '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3"><path d="M2.5 3h11l1 6v4h-13V9l1-6z"/><path d="M1.5 9h4l1 1.8h3L10.5 9h4"/></svg>',
    share:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="3.5" r="1.8"/><circle cx="4" cy="8" r="1.8"/><circle cx="12" cy="12.5" r="1.8"/><path d="M5.6 7.1l4.8-2.7M5.6 8.9l4.8 2.7"/></svg>',
    edit: '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M10.8 2.5l2.7 2.7-8 8H2.8v-2.7l8-8z"/></svg>',
    trash:
      '<svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M3 4.5h10M6 4.5V3h4v1.5M4.5 4.5l.6 9h5.8l.6-9"/></svg>',
    sparkle:
      '<svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M8 1.8l1.6 3.9L13.5 7.3l-3.9 1.6L8 12.8 6.4 8.9 2.5 7.3l3.9-1.6L8 1.8z"/></svg>',
    flask:
      '<svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M6 2.5h4M6.8 2.5v3.3L3.3 12a1.2 1.2 0 0 0 1.1 1.8h7.2A1.2 1.2 0 0 0 12.7 12L9.2 5.8V2.5"/><path d="M4.8 10h6.4"/></svg>',
    recall:
      '<svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="8" cy="8" r="6"/><path d="M5.5 5.5l5 5M10.5 5.5l-5 5"/></svg>',
    terminal:
      '<svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="1.8" y="2.5" width="12.4" height="11" rx="1.5"/><path d="M4.5 6l2.5 2-2.5 2M8.2 10h3.2"/></svg>',
  };
  function ic(n) {
    return ICONS[n] || ICONS.file;
  }

  // Authentic Conduit Capsules (Matched 1:1 to f_01_18_cc.png, f_02_38_cn_run.png & f_05_08_comms_det.png)
  function ontChip(typeName, rCount, wCount = 0, targetScreen = null) {
    const rw = `<span class="rw-r">R:${rCount}</span>${wCount ? `<span class="rw-w">W:${wCount}</span>` : ""}`;
    const clickAttr = targetScreen
      ? `data-onclick="event.stopPropagation(); switchScreen('${targetScreen}')"`
      : "";
    return `<span ${clickAttr} class="chip-ontology">${ic("ontShield")}<span>${typeName}</span>${rw}</span>`;
  }
  function toolChip(name, rCount, wCount = 0) {
    const rw = `<span class="rw-r">R:${rCount}</span>${wCount ? `<span class="rw-w">W:${wCount}</span>` : ""}`;
    return `<span class="chip-tool">${ic("wrench")}<span>${name}</span>${rw}</span>`;
  }
  function agentChip(name, rCount, wCount = 0) {
    const rw = `<span class="rw-r">R:${rCount}</span>${wCount ? `<span class="rw-w">W:${wCount}</span>` : ""}`;
    return `<span class="chip-agent">${ic("bolt")}<span>${name}</span>${rw}</span>`;
  }
  function pill(label, tone = "muted") {
    const m = {
      green: "so-pill-green",
      blue: "so-pill-blue",
      amber: "so-pill-amber",
      red: "so-pill-red",
      violet: "so-pill-violet",
      muted: "so-pill-muted",
    };
    return `<span class="so-pill ${m[tone] || "so-pill-muted"}">${label}</span>`;
  }
  function kpiBox(val, lbl, colHex = "#f0f5fa") {
    return `
        <div class="px-3 py-1.5 rounded-[6px] bg-[#12181d] border border-[#222c36] text-center min-w-[78px]">
          <div class="text-[16px] font-bold tnum leading-tight" style="color:${colHex}">${val}</div>
          <div class="text-[8.5px] uppercase tracking-[0.09em] text-[#6d7885] mt-0.5">${lbl}</div>
        </div>
      `;
  }

  function governedBtn({
    label,
    role,
    auditEvent,
    onClickJs,
    cls = "so-btn",
    iconName = null,
    blockedReason = "",
  }) {
    if (blockedReason) {
      const tip =
        `Blocked: ${blockedReason} · Role: ${role} · Audit: ${auditEvent}`.replace(
          /'/g,
          "\\'",
        );
      return `
          <span class="so-tooltip-wrap" data-onmouseenter="showTip(event, '${tip}')" data-onmouseleave="hideTip()">
            <button type="button" aria-disabled="true" tabindex="-1" class="${cls} is-disabled">
              ${iconName ? ic(iconName) : ""}<span>${label}</span>
            </button>
          </span>`;
    }
    const tip = `Role: ${role} · Writes Audit event: ${auditEvent}`.replace(
      /'/g,
      "\\'",
    );
    return `
        <span class="so-tooltip-wrap" data-onmouseenter="showTip(event, '${tip}')" data-onmouseleave="hideTip()">
          <button type="button" data-onclick="recordAudit('${auditEvent}'); ${onClickJs}" class="${cls}">
            ${iconName ? ic(iconName) : ""}<span>${label}</span>
          </button>
        </span>`;
  }

  const SCREENS = [
    // Dedicated App: Work Intake (No sidebar — matched to Conduit Intelligent Comms f_05_08_comms_det.png!)
    {
      id: "s01-intake",
      num: 1,
      short: "Work Intake",
      tabTitle: "Intake · wp-edge-telemetry",
      hasSidebar: false,
      showKpis: true,
      appGroup: "intake",
      parentLabel: "Command Center",
      parentId: "s13-fleet-overview",
      subtitle: "INTELLIGENT WORK INTAKE",
    },
    // Hub Navigation Screens (Calm 214px Conduit Left Sidebar — matched to f_01_18_cc.png, f_02_15_onyx.png, f_06_05_ct2.png!)
    {
      id: "s02-workspaces",
      num: 2,
      short: "Workspaces",
      tabTitle: "Workspaces",
      hasSidebar: true,
      showKpis: true,
      subtitle: "WORKSPACE INVENTORY",
    },
    {
      id: "s03-saved-views",
      num: 3,
      short: "Saved Views",
      tabTitle: "Saved Views",
      hasSidebar: true,
      showKpis: true,
      subtitle: "SAVED VIEWS",
    },
    // Dedicated Delivery Apps (No sidebar — matched to Conduit Change Notice Manager f_02_38_cn_run.png!)
    {
      id: "s04-scope-plan",
      num: 4,
      short: "Scope Plan",
      tabTitle: "STR-214 · Scope Plan",
      hasSidebar: false,
      showKpis: false,
      appGroup: "delivery",
      parentLabel: "Story Backlog",
      parentId: "s05-backlog",
      subtitle: "STORY SCOPE PLANNER",
    },
    {
      id: "s05-backlog",
      num: 5,
      short: "Story Backlog",
      tabTitle: "Delivery Backlog",
      hasSidebar: false,
      showKpis: true,
      appGroup: "delivery",
      parentLabel: "Command Center",
      parentId: "s13-fleet-overview",
      subtitle: "DELIVERY STORY BACKLOG",
    },
    {
      id: "s06-active-work",
      num: 6,
      short: "Active Work",
      tabTitle: "STR-214 · Live Runner",
      hasSidebar: false,
      showKpis: false,
      appGroup: "delivery",
      parentLabel: "Story Backlog",
      parentId: "s05-backlog",
      subtitle: "ACTIVE WORK PROCESSOR",
    },
    {
      id: "s07-pipeline",
      num: 7,
      short: "Pipeline Graph",
      tabTitle: "RUN-8844 · Pipeline",
      hasSidebar: false,
      showKpis: false,
      appGroup: "packaging",
      parentLabel: "Command Center",
      parentId: "s13-fleet-overview",
      subtitle: "PACKAGING PIPELINE MANAGER",
    },
    {
      id: "s08-staged-run",
      num: 8,
      short: "Staged Run",
      tabTitle: "Packaging run",
      hasSidebar: true,
      showKpis: false,
      subtitle: "PACKAGING RUN MANAGER",
    },
    // Dedicated Developer Studio Apps (No sidebar — full-bleed code/diff/runtime/lifecycle!)
    {
      id: "s09-source",
      num: 9,
      short: "Source",
      tabTitle: "values.yaml",
      hasSidebar: false,
      showKpis: false,
      appGroup: "studio",
      parentLabel: "Workspaces",
      parentId: "s02-workspaces",
      subtitle: "WORKSPACE STUDIO",
    },
    {
      id: "s10-diff",
      num: 10,
      short: "Diff",
      tabTitle: "CR-308 · Diff",
      hasSidebar: false,
      showKpis: false,
      appGroup: "studio",
      parentLabel: "Workspaces",
      parentId: "s02-workspaces",
      subtitle: "CHANGE REQUEST MANAGER",
    },
    {
      id: "s11-preview",
      num: 11,
      short: "Preview & Runtime",
      tabTitle: "Preview & Runtime",
      hasSidebar: false,
      showKpis: false,
      appGroup: "studio",
      parentLabel: "Workspaces",
      parentId: "s02-workspaces",
      subtitle: "RUNTIME PREVIEW INSPECTOR",
    },
    {
      id: "s12-lifecycle",
      num: 12,
      short: "Lifecycle",
      tabTitle: "wp-edge-telemetry",
      hasSidebar: false,
      showKpis: false,
      appGroup: "studio",
      parentLabel: "Workspaces",
      parentId: "s02-workspaces",
      subtitle: "WORK PROFILE LIFECYCLE",
    },
    // Hub Navigation Screens (Calm 214px Conduit Left Sidebar!)
    {
      id: "s13-fleet-overview",
      num: 13,
      short: "Command Center",
      tabTitle: "Command Center",
      hasSidebar: true,
      showKpis: true,
      subtitle: "FLEET DELIVERY COMMAND CENTER",
    },
    {
      id: "s14-env-detail",
      num: 14,
      short: "Environments",
      tabTitle: "Environments",
      hasSidebar: true,
      showKpis: false,
      subtitle: "FLEET ENVIRONMENTS",
    },
    {
      id: "s15-plan-detail",
      num: 15,
      short: "Release Planning",
      tabTitle: "Release Planning",
      hasSidebar: true,
      showKpis: true,
      subtitle: "RELEASE CONTROL TOWER",
    },
    // Dedicated Approvals Kanban App (No sidebar — matched 1:1 to Conduit f_04_08_comms_kan.png!)
    {
      id: "s16-approvals-board",
      num: 16,
      short: "Approvals Pipeline",
      tabTitle: "Approvals Pipeline",
      hasSidebar: false,
      showKpis: true,
      appGroup: "approvals",
      parentLabel: "Release Planning",
      parentId: "s15-plan-detail",
      subtitle: "APPROVALS PIPELINE",
    },
    // Hub Spoke Operations Screen (Calm 214px Conduit Left Sidebar — matched to Conduit Onyx360 f_02_15_onyx.png!)
    {
      id: "s17-spokes-audit",
      num: 17,
      short: "Spoke Operations",
      tabTitle: "Spoke Operations",
      hasSidebar: true,
      showKpis: true,
      subtitle: "SPOKE OPERATIONS CENTER",
    },
    // Dedicated System Reference Screens (No sidebar!)
    {
      id: "s18-components",
      num: 18,
      short: "Component System",
      tabTitle: "Component System",
      hasSidebar: false,
      showKpis: false,
      appGroup: "system",
      parentLabel: "Command Center",
      parentId: "s13-fleet-overview",
      subtitle: "COMPONENT DESIGN SYSTEM",
    },
    {
      id: "s19-ontology",
      num: 19,
      short: "Ontology Registry",
      tabTitle: "Ontology · 19 Types",
      hasSidebar: false,
      showKpis: false,
      appGroup: "system",
      parentLabel: "Command Center",
      parentId: "s13-fleet-overview",
      subtitle: "ONTOLOGY REGISTRY",
    },
    // 8 Release-Management Extension Views
    {
      id: "rm01-env-page",
      num: 20,
      short: "Environment Page",
      tabTitle: "Env · edge-west-02",
      hasSidebar: true,
      showKpis: true,
      subtitle: "ENVIRONMENT TELEMETRY HUB",
    },
    {
      id: "rm02-env-activity",
      num: 21,
      short: "Environment Activity",
      tabTitle: "Activity · edge-west-02",
      hasSidebar: true,
      showKpis: true,
      subtitle: "ENVIRONMENT ACTIVITY LEDGER",
    },
    {
      id: "rm03-plan-detail",
      num: 22,
      short: "Plan Inspector",
      tabTitle: "PLN-2026-0416 · Failed",
      hasSidebar: false,
      showKpis: false,
      appGroup: "plans_gov",
      parentLabel: "Environment Activity",
      parentId: "rm02-env-activity",
      subtitle: "PLAN EXECUTION INSPECTOR",
    },
    {
      id: "rm04-constraints",
      num: 23,
      short: "Constraint Gate",
      tabTitle: "Action Constraints",
      hasSidebar: false,
      showKpis: false,
      appGroup: "plans_gov",
      parentLabel: "Environment Hub",
      parentId: "rm01-env-page",
      subtitle: "ACTION CONSTRAINT GATE",
    },
    {
      id: "rm05-release-rollout",
      num: 24,
      short: "Release Rollout",
      tabTitle: "Release · uds-core 0.31.2",
      hasSidebar: true,
      showKpis: true,
      subtitle: "FLEET RELEASE CONVERGENCE",
    },
    {
      id: "rm06-promotion-pipe",
      num: 25,
      short: "Promotion Pipeline",
      tabTitle: "Channel Promotion",
      hasSidebar: true,
      showKpis: false,
      subtitle: "CHANNEL PROMOTION PIPELINE",
    },
    {
      id: "rm07-change-requests",
      num: 26,
      short: "Change Requests",
      tabTitle: "Change requests",
      hasSidebar: true,
      showKpis: false,
      subtitle: "CONFIG CHANGE REQUESTS",
    },
    {
      id: "rm08-bundle-preview",
      num: 27,
      short: "Bundle Preview",
      tabTitle: "Bundle preview",
      hasSidebar: true,
      showKpis: false,
      subtitle: "BUNDLE PREVIEW",
    },
    {
      id: "ai01-logic",
      num: 38,
      short: "Logic",
      tabTitle: "Logic workbench",
      hasSidebar: true,
      showKpis: false,
      subtitle: "PROPOSAL PIPELINE",
    },
    {
      id: "ai02-evals",
      num: 39,
      short: "Evals",
      tabTitle: "Eval cases",
      hasSidebar: true,
      showKpis: false,
      subtitle: "GATE CASES",
    },
    {
      id: "ai03-events",
      num: 40,
      short: "Automations",
      tabTitle: "Event automations",
      hasSidebar: true,
      showKpis: false,
      subtitle: "EXAMPLE AUTOMATIONS",
    },
    {
      id: "ai04-courses",
      num: 41,
      short: "Courses",
      tabTitle: "Courses of action",
      hasSidebar: true,
      showKpis: false,
      subtitle: "SCORED OPTIONS",
    },
    {
      id: "area-command",
      num: 41,
      short: "Command Center",
      tabTitle: "Command Center",
      hasSidebar: true,
      showKpis: true,
      subtitle: "COMMAND CENTER",
    },
    {
      id: "area-packaging",
      num: 42,
      short: "Packaging",
      tabTitle: "Packaging",
      hasSidebar: true,
      showKpis: true,
      subtitle: "PACKAGING",
    },
    {
      id: "area-releases",
      num: 43,
      short: "Releases",
      tabTitle: "Releases",
      hasSidebar: true,
      showKpis: true,
      subtitle: "RELEASES",
    },
    {
      id: "area-changes",
      num: 44,
      short: "Change requests",
      tabTitle: "Change requests",
      hasSidebar: true,
      showKpis: true,
      subtitle: "CHANGE REQUESTS",
    },
    {
      id: "area-intelligence",
      num: 45,
      short: "Intelligence",
      tabTitle: "Intelligence",
      hasSidebar: true,
      showKpis: true,
      subtitle: "INTELLIGENCE",
    },
    {
      id: "area-fleet",
      num: 46,
      short: "Fleet",
      tabTitle: "Fleet",
      hasSidebar: true,
      showKpis: true,
      subtitle: "FLEET",
    },
    {
      id: "area-audit",
      num: 47,
      short: "Audit",
      tabTitle: "Audit",
      hasSidebar: true,
      showKpis: true,
      subtitle: "AUDIT",
    },
    {
      id: "au01-audit",
      num: 37,
      short: "Audit",
      tabTitle: "Audit chain",
      hasSidebar: true,
      showKpis: false,
      subtitle: "AUTHORIZATION AUDIT",
    },
  ];

  const TREE_GROUPS = [
    {
      key: "profiles",
      label: "WORK PROFILES",
      count: 4,
      items: [
        {
          id: "wp-edge-telemetry",
          dot: "#ec9a3c",
          badge: "0.31.2",
          target: "s12-lifecycle",
        },
        {
          id: "wp-uds-core-base",
          dot: "#34d399",
          badge: "Synced",
          target: "s04-scope-plan",
        },
        {
          id: "wp-offline-sync",
          dot: "#34d399",
          badge: "Active",
          target: "s07-pipeline",
        },
        {
          id: "wp-policy-guard",
          dot: "#596573",
          badge: "Archived",
          target: "s09-source",
        },
      ],
    },
    {
      key: "environments",
      label: "ENVIRONMENTS",
      count: 48,
      items: [
        {
          id: "edge-west-02",
          dot: "#ec9a3c",
          badge: "3 Drifts",
          target: "s14-env-detail",
        },
        {
          id: "edge-east-01",
          dot: "#34d399",
          badge: "Online",
          target: "s14-env-detail",
        },
        {
          id: "offline-vault-04",
          dot: "#4c90f0",
          badge: "Diode",
          target: "s08-staged-run",
        },
        {
          id: "remote-rig-09",
          dot: "#e76a6e",
          badge: "Degraded",
          target: "s14-env-detail",
        },
      ],
    },
    {
      key: "spokes",
      label: "SPOKES",
      count: 6,
      items: [
        {
          id: "spoke-west-relay",
          dot: "#4c90f0",
          badge: "18 Envs",
          target: "s17-spokes-audit",
        },
        {
          id: "spoke-diode-hall-b",
          dot: "#34d399",
          badge: "6 Envs",
          target: "s17-spokes-audit",
        },
        {
          id: "spoke-courier-north",
          dot: "#ec9a3c",
          badge: "4 Envs",
          target: "s17-spokes-audit",
        },
      ],
    },
  ];

  const state = {
    screen: "s13-fleet-overview",
    operatorName: "Operator",
    auditCount: 142,
    leftRailCollapsed:
      readPref("conduit.console.railCollapsed", false) === true ||
      readPref("conduit.console.railCollapsed", false) === "true",
    treeOpen: { profiles: true, environments: true, spokes: true },
    openTabs: [
      "s13-fleet-overview",
      "s01-intake",
      "s04-scope-plan",
      "s07-pipeline",
      "s15-plan-detail",
    ],
    tabOverflowOpen: false,
    chatCollapsed: true,
    cmdOmnibarOpen: false,
    cmdOmnibarQuery: "",
    cmdSelectedIdx: 0,
    cmdCategory: "All",
    rmEnvResourceTab: "overview",
    rmActivitySubnav: "plans",
    rmPlanSearch: "",
    rmPlanStatus: "ALL",
    rmCopiedRaw: false,
    rmActionId: "execute_plan",
    rmFixWindow: false,
    rmFixApproval: false,
    rmFixUnsigned: false,
    rmFixRolling: false,
    rmExecNotice: "",
    rmWaveFilter: "ALL",
    rmSelectedChannel: "disconnected",
    rmPromoDialogOpen: false,
    rmPromoStrategy: "FAST_TRACK_TO_CHANNEL",
    rmPromoNote:
      "Verified 6.4h soak across 22 edge-cohorts clusters; staging signed bundle for disconnected sites.",
    rmPromotedNotice: "",
    rmSelectedCrId: "CR-308",
    rmIncludedCrs: ["CR-307"],
    rmBundleDownloaded: "",
    chatModel: "ops-standard-32k",
    assistantMode: "explore",
    assistantRemoved: [],
    assistantAdded: [],
    assistantResults: [],
    globalQuery: "",

    wsStatusFacet: "ALL",
    wsProfileFacet: "ALL",

    savedViews: [
      {
        id: "SV-01",
        name: "Drifted Edge Environments",
        targetType: "ConduitEnvironment",
        query: "status = DRIFT_DETECTED AND spoke = spoke-west-*",
        status: "System Locked",
        tone: "amber",
        pinned: true,
        hits: 5,
      },
      {
        id: "SV-02",
        name: "Plans Awaiting 2nd Approval",
        targetType: "ConduitPlan",
        query: "status = AWAITING_APPROVAL AND approvals = 1/2",
        status: "Shared",
        tone: "blue",
        pinned: true,
        hits: 3,
      },
      {
        id: "SV-03",
        name: "Active Telemetry Workspaces",
        targetType: "ConduitWorkspace",
        query: "profile = wp-edge-telemetry AND status IN (ACTIVE, BUILDING)",
        status: "Personal",
        tone: "muted",
        pinned: true,
        hits: 4,
      },
      {
        id: "SV-04",
        name: "Failed Packaging Runs (24h)",
        targetType: "ConduitPackagingRun",
        query: "status = FAILED AND started >= -24h",
        status: "Shared",
        tone: "blue",
        pinned: false,
        hits: 2,
      },
      {
        id: "SV-05",
        name: "Courier Import Queue",
        targetType: "ConduitEnvironment",
        query: "status = AWAITING_BUNDLE AND spoke = spoke-courier-north",
        status: "Personal",
        tone: "muted",
        pinned: false,
        hits: 6,
      },
    ],
    savedViewsForceEmpty: false,
    renamingViewId: null,

    selectedStoryId: "STR-214",
    agentPaused: false,

    selectedPipeNode: "publish",
    pipeFollowLogs: true,
    pipeLogQuery: "",

    showRerunConfirm: true,
    confirmActionType: "rerun",

    selectedBranch: "release/0.31.2",
    selectedSourceFile: "charts/ingress-gateway/values.yaml",
    diffMode: "split",
    selectedDiffFile: "charts/ingress-gateway/values.yaml",

    previewDevice: "desktop",
    feedbackMode: true,
    runtimeRunning: true,
    logLevelFilter: "ALL",
    pinnedFeedback: [
      {
        num: 1,
        el: "div.spoke-card#edge-west-02",
        author: "J. Alvarez",
        note: "Surface store-and-forward queue lag badge above 10m.",
      },
      {
        num: 2,
        el: "span.mem-gauge#proxy-limit",
        author: "M. Chen",
        note: "Display 1Gi target ceiling next to 712Mi Reported state.",
      },
      {
        num: 3,
        el: "button#btn-diode-sync",
        author: "S. Kline",
        note: "Disable manual sync while optical diode hardware lock is active.",
      },
    ],

    deleteConfirmInput: "",
    planSimQuorum: false,
    planSimWindow: false,
    envTab: "Overview",
    selectedKanbanPlanId: "PLN-2026-0418",
    revChecks: { sbom: true, soak: true, diode: false },

    chatMessages: [
      {
        role: "J. Alvarez",
        time: "09:48 PM",
        text: "Trace links for RUN-8842 and why its Plan is gated.",
        refs: [],
      },
      {
        role: "Assistant",
        time: "09:48 PM",
        text: "Packaging run RUN-8842 (Staged) proposes Plan PLN-2026-0418, targeting 14 Environments including edge-west-02.",
        refs: [
          { label: "ConduitPackagingRun · RUN-8842", target: "s08-staged-run" },
          { label: "ConduitPlan · PLN-2026-0418", target: "s15-plan-detail" },
          {
            label: "ConduitEnvironment · edge-west-02",
            target: "s14-env-detail",
          },
        ],
      },
      {
        role: "J. Alvarez",
        time: "09:52 PM",
        text: "Which Requirements block Approve Plan?",
        refs: [],
      },
      {
        role: "Assistant",
        time: "09:52 PM",
        text: "Two Requirement checks are Blocked: Approval quorum is 1/2 (@fleet-approvers) and the 02:00–05:00 UTC window is closed.",
        refs: [{ label: "ConduitChangeRequest · CR-308", target: "s10-diff" }],
      },
    ],
  };

  const releaseGates = createReleaseGates({
    state,
    ic,
    pill,
    ontChip,
    toolChip,
    agentChip,
    governedBtn,
    rmApiStrip,
    recordAudit,
    render: () => renderMainWorkspace(),
    switchScreen: (id) => switchScreen(id),
  });
  releaseGates.extendState();
  SCREENS.push(...releaseGates.screens);
  const ontologyGraph = createOntologyGraph({
    state,
    render: () => renderMainWorkspace(),
  });
  ontologyGraph.extendState();
  SCREENS.push(...ontologyGraph.screens);
  const settings = createSettings({
    state,
    render: () => renderApp(),
  });
  settings.extendState();
  SCREENS.push(...settings.screens);
  const liveHub = createLiveHub({
    state,
    render: () => renderApp(),
  });
  state.liveHub = liveHub;
  const intelligence = createIntelligence();

  function recordAudit(evt) {
    state.auditCount += 1;
  }
  function showTip(ev, text) {
    const tip = document.getElementById("so-portal-tooltip");
    if (!tip) return;
    tip.textContent = text;
    tip.classList.remove("hidden");
    const r = ev.currentTarget.getBoundingClientRect();
    const left = Math.max(8, Math.min(window.innerWidth - 308, r.left));
    const top =
      r.bottom + 6 + 48 > window.innerHeight ? r.top - 42 : r.bottom + 6;
    tip.style.left = `${Math.round(left)}px`;
    tip.style.top = `${Math.round(top)}px`;
  }
  function hideTip() {
    const tip = document.getElementById("so-portal-tooltip");
    if (tip) tip.classList.add("hidden");
  }

  const AREAS = {
    "area-command": {
      label: "Command Center",
      tabs: [
        { id: "command", label: "Overview", screen: "cc-overview" },
        {
          id: "example",
          label: "Example fleet",
          screen: "s13-fleet-overview",
        },
        {
          id: "tower",
          label: "Release Control Tower",
          screen: "s15-plan-detail",
        },
      ],
    },
    "area-packaging": {
      label: "Packaging",
      tabs: [
        { id: "runs", label: "Runs", screen: "pkg-runs" },
        { id: "timeline", label: "Timeline", screen: "s08-staged-run" },
        { id: "workspace", label: "Workspace", screen: "s02-workspaces" },
        { id: "bundle", label: "Bundle", screen: "rm08-bundle-preview" },
        { id: "proposals", label: "Proposals", screen: "pt01-proposal-trace" },
      ],
    },
    "area-releases": {
      label: "Releases",
      tabs: [
        { id: "environments", label: "Environments", screen: "s14-env-detail" },
        { id: "products", label: "Products", screen: "rel-products" },
        { id: "channels", label: "Channels", screen: "rm-channels" },
        { id: "waves", label: "Waves", screen: "rm-waves" },
        { id: "activity", label: "Activity", screen: "rm02-env-activity" },
        { id: "promotion", label: "Promotion", screen: "rm06-promotion-pipe" },
        { id: "recall", label: "Recall", screen: "rm09-recalls" },
      ],
    },
    "area-changes": {
      label: "Change requests",
      tabs: [
        { id: "list", label: "Requests", screen: "rm07-change-requests" },
        { id: "approvals", label: "Approvals", screen: "s16-approvals-board" },
      ],
    },
    "area-intelligence": {
      label: "Intelligence",
      tabs: [
        { id: "logic", label: "Logic", screen: "ai01-logic" },
        { id: "evals", label: "Evals", screen: "ai02-evals" },
        { id: "courses", label: "Courses", screen: "ai04-courses" },
        { id: "events", label: "Automations", screen: "ai03-events" },
      ],
    },
    "area-fleet": {
      label: "Fleet",
      tabs: [
        { id: "environments", label: "Environments", screen: "flt-environments" },
        { id: "drift", label: "Drift", screen: "s14-env-detail" },
        { id: "spokes", label: "Example spokes", screen: "s17-spokes-audit" },
      ],
    },
    "area-audit": {
      label: "Audit",
      tabs: [{ id: "chain", label: "Hash chain", screen: "au01-audit" }],
    },
  };
  const REDIRECT = {};
  Object.entries(AREAS).forEach(([area, spec]) => {
    spec.tabs.forEach((tab) => {
      REDIRECT[tab.screen] = { area, tab: tab.id };
    });
  });
  // Links to the Command Center land on its live overview, not the example.
  REDIRECT["s13-fleet-overview"] = { area: "area-command", tab: "command" };
  REDIRECT["pt02-proposals-evals"] = { area: "area-packaging", tab: "proposals" };
  REDIRECT["s09-source"] = { area: "area-packaging", tab: "workspace" };
  REDIRECT["s10-diff"] = { area: "area-packaging", tab: "workspace" };
  REDIRECT["s11-preview"] = { area: "area-packaging", tab: "workspace" };
  REDIRECT["s12-lifecycle"] = { area: "area-packaging", tab: "workspace" };
  REDIRECT["rm01-env-page"] = { area: "area-releases", tab: "environments" };
  REDIRECT["rm05-release-rollout"] = { area: "area-releases", tab: "activity" };

  function areaFor(id) {
    return REDIRECT[id] || null;
  }

  function switchScreen(scrId) {
    const mapped = areaFor(scrId);
    if (mapped) {
      state.screen = mapped.area;
      state.areaTab = mapped.tab;
    } else if (AREAS[scrId]) {
      state.screen = scrId;
      if (
        !state.areaTab ||
        !AREAS[scrId].tabs.some((tab) => tab.id === state.areaTab)
      ) {
        state.areaTab = AREAS[scrId].tabs[0].id;
      }
    } else {
      state.screen = scrId;
    }
    const nextHash = "#" + (mapped ? state.screen : scrId);
    if (location.hash !== nextHash) history.replaceState(null, "", nextHash);
    if (!state.openTabs.includes(scrId)) {
      state.openTabs.splice(1, 0, scrId);
      if (state.openTabs.length > 5) state.openTabs.pop();
    }
    state.tabOverflowOpen = false;
    closeMobileNav();
    renderApp();
  }
  function closeTab(ev, scrId) {
    ev.stopPropagation();
    if (state.openTabs.length <= 1) return;
    state.openTabs = state.openTabs.filter((id) => id !== scrId);
    if (state.screen === scrId) state.screen = state.openTabs[0];
    renderApp();
  }
  function toggleMobileNav() {
    const el = document.getElementById("left-nav");
    const bd = document.getElementById("mobile-nav-backdrop");
    if (!el) return;
    const open = !el.classList.contains("mobile-open");
    el.classList.toggle("mobile-open", open);
    if (bd) bd.classList.toggle("is-open", open);
  }
  function closeMobileNav() {
    const el = document.getElementById("left-nav");
    const bd = document.getElementById("mobile-nav-backdrop");
    if (el) el.classList.remove("mobile-open");
    if (bd) bd.classList.remove("is-open");
  }
  function toggleLeftRailCollapse() {
    state.leftRailCollapsed = !state.leftRailCollapsed;
    writePref("conduit.console.railCollapsed", state.leftRailCollapsed);
    renderLeftRail();
  }
  function toggleChatRail() {
    state.chatCollapsed = !state.chatCollapsed;
    renderChatRail();
    renderConduitModals();
  }
  function handleGlobalSearch(val) {
    state.globalQuery = val.trim().toLowerCase();
    renderMainWorkspace();
  }

  // =========================================================================
  // AUTHENTIC Conduit LEFT RAIL + TOP BAR + PLAIN RIGHT CHAT
  // =========================================================================
  // Screens whose body is read from the Hub. Everything else is example data
  // until it is wired, and says so in the header badge.
  const LIVE_SCREENS = new Set([
    "cc-overview",
    "pkg-runs",
    "s08-staged-run",
    "s02-workspaces",
    "rm08-bundle-preview",
    "s14-env-detail",
    "rel-products",
    "rm-channels",
    "rm-waves",
    "rm06-promotion-pipe",
    "rm09-recalls",
    "rm07-change-requests",
    "flt-environments",
    "au01-audit",
    "ai01-logic",
    "ai02-evals",
    "ai04-courses",
    "ai03-events",
  ]);
  // Screens that label each section Hub or Example data themselves.
  const SECTION_LABELLED = new Set(["adm02-org-control-panel"]);

  /** The screen actually on view: an area's current tab, or the screen. */
  function visibleScreen() {
    const area = AREAS[state.screen];
    if (!area) return state.screen;
    const tab =
      area.tabs.find((item) => item.id === state.areaTab) || area.tabs[0];
    return tab.screen;
  }

  function updateExampleBadge() {
    const badge = document.getElementById("example-data-badge");
    if (!badge) return;
    const screen = visibleScreen();
    const live =
      LIVE_SCREENS.has(screen) ||
      (screen === "pt01-proposal-trace" &&
        (state.hub?.packaging || []).length > 0) ||
      (screen === "og01-ontology-graph" && state.graphNote === "Ontology API");
    if (SECTION_LABELLED.has(screen)) {
      badge.style.display = "none";
      return;
    }
    badge.style.display = "";
    badge.dataset.mode = live ? "live" : "example";
    badge.textContent = live ? "LIVE" : "EXAMPLE DATA";
    badge.title = live
      ? "This view is read from the Hub"
      : "This view shows example data, not this Hub's records";
  }

  function renderKpiStrip() {
    const strip = document.getElementById("header-kpi-strip");
    if (!strip) return;
    const kpis = kpisFromHub(state.hub);
    const icons = {
      convergence: "checkCircle",
      environments: "cluster",
      packaging: "cube",
      plans: "shield",
      changes: "warnTri",
    };
    const tones = {
      ok: "#34d399",
      info: "#4c90f0",
      warn: "#eab854",
      bad: "#e76a6e",
    };
    const esc = (v) =>
      String(v).replace(
        /[&<>"]/g,
        (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c],
      );
    const cells = kpis
      .map((k) => {
        const color = tones[k.tone] || tones.info;
        return `
      <div class="flex flex-col">
        <div class="flex items-center gap-1.5">
          <span class="flex" style="color:${color}">${ic(icons[k.id])}</span>
          <span class="text-[17px] font-bold tnum leading-none" style="color:${color}">${esc(k.value)}</span>
        </div>
        <div class="text-[9px] uppercase tracking-[0.08em] text-[#6d7885] mt-1">${esc(k.label)} <span class="kpi-sub-extra normal-case tracking-normal text-[#596573]">${k.sub ? "· " + esc(k.sub) : ""}</span></div>
      </div>`;
      })
      .join("");
    strip.innerHTML =
      (allLive(kpis)
        ? ""
        : '<span class="text-[10px] uppercase tracking-[0.08em] text-[#eab854]">Example data</span>') +
      cells;
  }

  function renderLeftRail() {
    document.querySelectorAll("[data-ic]").forEach((el) => {
      el.innerHTML = ic(el.getAttribute("data-ic"));
    });

    const activeMeta = SCREENS.find((s) => s.id === state.screen) || SCREENS[0];
    const subEl = document.getElementById("topbar-subtitle");
    if (subEl) {
      subEl.textContent = activeMeta.subtitle;
      subEl.className = activeMeta.hasSidebar
        ? "text-[10.5px] font-medium uppercase tracking-[0.14em] text-[#7c8794] hidden sm:inline truncate"
        : "text-[10.5px] font-semibold uppercase tracking-[0.14em] text-[#eab854] hidden sm:inline truncate";
    }

    const kpiEl = document.getElementById("header-kpi-strip");
    if (kpiEl) {
      if (activeMeta.showKpis) kpiEl.classList.remove("kpi-hidden");
      else kpiEl.classList.add("kpi-hidden");
    }
    renderKpiStrip();
    const operatorName = state.operatorName || "Operator";
    const operatorLabel = document.getElementById("operator-name");
    const operatorMark = document.getElementById("operator-initials");
    const operatorChip = document.getElementById("operator-chip");
    if (operatorLabel) operatorLabel.textContent = operatorName;
    if (operatorMark) {
      const parts = operatorName.split(/[^A-Za-z0-9]+/).filter(Boolean);
      operatorMark.textContent = parts.length
        ? parts
            .slice(0, 2)
            .map((part) => part[0].toUpperCase())
            .join("")
        : "OP";
    }
    if (operatorChip) operatorChip.title = `Operator: ${operatorName}`;
    updateExampleBadge();

    const aside = document.getElementById("left-nav");
    const sectionsEl = document.getElementById("left-nav-sections");
    if (!aside || !sectionsEl) return;

    // Dedicated App Screens hide left sidebar on >=1024px via .hub-hidden while keeping mobile drawer ready on <1024px!
    const hubHiddenCls = activeMeta.hasSidebar ? "" : " hub-hidden";

    const navItems = [
      { label: "Packaging", icon: "cube", id: "area-packaging" },
      { label: "Releases", icon: "cluster", id: "area-releases" },
      { label: "Change requests", icon: "approvals", id: "area-changes" },
      { label: "Intelligence", icon: "bolt", id: "area-intelligence" },
      { label: "Fleet", icon: "overview", id: "area-fleet" },
      { label: "Audit", icon: "audit", id: "area-audit" },
    ];

    const navigationItems = [
      { label: "Command Center", icon: "overview", id: "area-command" },
      ...navItems,
    ];
    const builderApps = [
      { label: "Work Intake & Stories", icon: "inbox", id: "s01-intake" },
      { label: "Workspaces", icon: "develop", id: "s02-workspaces" },
      { label: "Active Work Runner", icon: "bolt", id: "s06-active-work" },
    ];
    const governanceApps = [
      { label: "Ontology", icon: "globe", id: "s19-ontology" },
      { label: "Component System", icon: "settings", id: "s18-components" },
      ...settings.navItems(),
    ];
    // Every section is a list of groups so the collapsed rail can flatten them.
    const sections = [
      { groups: [{ title: "NAVIGATION", nav: true, items: navigationItems }] },
      {
        groups: [
          { title: "BUILDER & STUDIO", items: builderApps },
          { title: "OPERATOR TOOLS", items: releaseGates.toolApps },
        ],
      },
      { groups: [{ title: "GOVERNANCE", items: governanceApps }] },
    ];

    const iconRail =
      state.leftRailCollapsed ||
      window.matchMedia("(max-width: 1024px) and (min-width: 1024px)").matches;

    const sitesEl = document.getElementById("left-nav-sites");
    if (sitesEl) {
      const s = siteSummary(state.hub);
      sitesEl.textContent =
        !iconRail && s ? `${s.sites} sites · ${s.spokes} spokes` : "";
      sitesEl.classList.toggle("hidden", iconRail);
    }

    if (iconRail) {
      aside.className =
        "is-rail-collapsed w-[var(--w-rail-collapsed)] shrink-0 bg-[#101519] border-r border-[#1e252b] flex flex-col justify-between items-center py-2.5 select-none" +
        hubHiddenCls;
      sectionsEl.className = "flex flex-col items-center gap-1.5 pt-1";
      sectionsEl.innerHTML = sections
        .map((section) =>
          section.groups
            .flatMap((g) => g.items)
            .map(
              (m) =>
                `<button data-onclick="switchScreen('${m.id}')" title="${m.label}" aria-label="${m.label}" class="so-nav-row !w-[36px] !px-0 !justify-center ${
                  state.screen === m.id ? "is-active" : ""
                }">${ic(m.icon)}</button>`,
            )
            .join(""),
        )
        .join(
          '<div class="w-[24px] h-px bg-[#1e252b] my-1" role="separator"></div>',
        );
      return;
    }

    aside.className =
      "w-[var(--w-rail)] shrink-0 bg-[#101519] border-r border-[#1e252b] flex flex-col justify-between min-h-0 select-none" +
      hubHiddenCls;
    sectionsEl.className = "px-2.5 pt-3.5 space-y-5 overflow-y-auto";

    const renderAppGroup = (title, apps) => `
        <div>
          <div class="px-2.5 mb-1.5 text-[9.5px] font-semibold uppercase tracking-[0.11em] text-[#596573]">${title}</div>
          <div class="space-y-0.5">
            ${apps
              .map(
                (a) => `
              <button data-onclick="switchScreen('${a.id}')" class="so-nav-row ${state.screen === a.id ? "is-active" : ""}">
                <span class="flex items-center gap-2 min-w-0 flex-1">
                  <span class="flex shrink-0 text-[#7c8794]">${ic(a.icon)}</span>
                  <span class="truncate">${a.label}</span>
                </span>
                <span class="text-[#4a5562] flex shrink-0">${ic("extLink")}</span>
              </button>
            `,
              )
              .join("")}
          </div>
        </div>
      `;

    const renderNavGroup = (title, items) => `
        <div>
          <div class="px-2.5 mb-1.5 text-[9.5px] font-semibold uppercase tracking-[0.11em] text-[#596573]">${title}</div>
          <div class="space-y-0.5">
            ${items
              .map((m) => {
                const isAct =
                  state.screen === m.id ||
                  (m.id === "rm01-env-page" &&
                    (state.screen === "rm02-env-activity" ||
                      state.screen === "s14-env-detail")) ||
                  (m.id === "s02-workspaces" &&
                    state.screen === "s03-saved-views");
                const tone =
                  {
                    "area-packaging": "kit-bar-ok",
                    "area-releases": "kit-bar-info",
                    "area-changes": "kit-bar-warn",
                    "area-intelligence": "kit-bar-ai",
                    "area-fleet": "kit-bar-info",
                  }[m.id] || "";
                return `
                <button data-onclick="switchScreen('${m.id}')" class="so-nav-row kit-nav ${tone} ${isAct ? "is-active" : ""}">
                  <span class="flex items-center gap-2 min-w-0 flex-1">
                    <span class="flex shrink-0">${ic(m.icon)}</span>
                    <span class="truncate">${m.label}</span>
                  </span>
                  ${isAct ? '<span class="text-[13px] text-[#5c9ce6] shrink-0">&rsaquo;</span>' : ""}
                </button>
              `;
              })
              .join("")}
          </div>
        </div>
      `;

    sectionsEl.innerHTML = sections
      .map(
        (section, i) => `
        <div class="${i ? "border-t border-[#1e252b] pt-4" : ""} space-y-5">
          ${section.groups
            .map((g) =>
              g.nav
                ? renderNavGroup(g.title, g.items)
                : renderAppGroup(g.title, g.items),
            )
            .join("")}
        </div>`,
      )
      .join("");
  }

  function renderCenterTabStrip() {
    const bar = document.getElementById("app-breadcrumb-bar");
    if (!bar) return;
    const activeMeta = SCREENS.find((s) => s.id === state.screen) || SCREENS[0];

    // Hub screens sit 100% FLUSH below <header> with NO secondary bar (exact match to Conduit f_01_18_cc, f_02_15, f_06_05)!
    if (activeMeta.hasSidebar) {
      bar.className = "hidden";
      bar.innerHTML = "";
      return;
    }

    // Dedicated App screens get Conduit's clean 34px breadcrumb bar (exact match to "<- Queue > CN-2026-0848" in f_02_38_cn_run.png!)
    const groupPeers =
      {
        intake: [
          { id: "s01-intake", label: "Intake Triage" },
          { id: "s16-approvals-board", label: "Approvals Pipeline" },
        ],
        delivery: [
          { id: "s05-backlog", label: "Backlog" },
          { id: "s04-scope-plan", label: "Scope Plan" },
          { id: "s06-active-work", label: "Active Work" },
        ],
        packaging: [
          { id: "s07-pipeline", label: "Pipeline Graph" },
          { id: "s08-staged-run", label: "Staged Run" },
          { id: "rm08-bundle-preview", label: "Bundle Preview" },
        ],
        plans_gov: [
          { id: "rm03-plan-detail", label: "Plan Inspector (Failed)" },
          { id: "rm04-constraints", label: "Constraint Gate" },
          { id: "rm11-debug-terminal", label: "Debug Terminal" },
          { id: "s15-plan-detail", label: "Control Tower" },
        ],
        proposals: releaseGates.proposalPeers,
        studio: [
          { id: "s09-source", label: "Source" },
          { id: "rm07-change-requests", label: "Change Requests" },
          { id: "s10-diff", label: "Workspace Diff" },
          { id: "s11-preview", label: "Preview & Runtime" },
          { id: "s12-lifecycle", label: "Lifecycle" },
        ],
        approvals: [
          { id: "s16-approvals-board", label: "Approvals Kanban" },
          { id: "rm04-constraints", label: "Constraint Gate" },
          { id: "s15-plan-detail", label: "Control Tower" },
        ],
        system: [
          { id: "s18-components", label: "Component System" },
          { id: "s19-ontology", label: "Ontology (19 Types)" },
        ],
        settings: [
          { id: "adm01-user-prefs", label: "Preferences" },
          { id: "adm02-org-control-panel", label: "Organization" },
          { id: "adm03-auth-signin", label: "Sign-in" },
          { id: "adm04-data-sources", label: "Data sources" },
        ],
      }[activeMeta.appGroup] || [];

    bar.className =
      "h-[34px] shrink-0 bg-[#0d1317] border-b border-[#1a2229] px-4 flex items-center justify-between gap-3 z-30";
    bar.innerHTML = `
        <div class="flex items-center gap-2 text-[11.5px] min-w-0">
          <button data-onclick="switchScreen('${activeMeta.parentId || "s13-fleet-overview"}')" class="inline-flex items-center gap-1.5 text-[#eab854] hover:underline font-medium shrink-0">
            ${ic("arrowLeft")} <span>${activeMeta.parentLabel || "Command Center"}</span>
          </button>
          <span class="text-[#4a5562]">&rsaquo;</span>
          <span class="text-[#f0f5fa] font-medium truncate">${activeMeta.tabTitle}</span>
        </div>

        <div class="so-peer-scroll flex items-center gap-1 max-w-full overflow-x-auto no-scrollbar">
          ${groupPeers
            .map(
              (p) => `
            <button type="button" data-peer-active="${state.screen === p.id ? "1" : "0"}" data-onclick="switchScreen('${p.id}')" class="h-[24px] px-2.5 rounded-[4px] text-[11px] font-medium shrink-0 whitespace-nowrap transition-colors ${
              state.screen === p.id
                ? "bg-[#17253a] text-[#7cb4ff] border border-[#2a4263]"
                : "text-[#8b96a5] hover:text-[#f0f5fa] hover:bg-[#141c24]"
            }">${p.label}</button>
          `,
            )
            .join("")}
        </div>
      `;
    const activePeer = bar.querySelector("[data-peer-active='1']");
    if (activePeer && activePeer.scrollIntoView) {
      try {
        activePeer.scrollIntoView({ block: "nearest", inline: "center" });
      } catch {
        /* the peer strip can be detached */
      }
    }
  }

  function renderChatRail() {
    const rail = document.getElementById("right-chat-rail");
    if (!rail) return;
    publishAssistantContext(state);
    if (state.chatCollapsed) {
      rail.className = "hidden";
      rail.innerHTML = "";
      if (typeof notifyOperator === "function") notifyOperator();
      return;
    }
    const openClass =
      "shrink-0 bg-[#0e1418] border-l border-[#1e252b] flex flex-row min-h-0";
    if (rail.className !== openClass) rail.className = openClass;
    rail.style.width = `${clampDockWidth(Number(readPref("conduit.console.dockWidth", DOCK_DEFAULT)))}px`;
    if (!rail.querySelector("#assistant-thread-mount")) {
      rail.innerHTML = `
          <div id="dock-resize" role="separator" aria-orientation="vertical" aria-label="Resize dock"></div>
          <div class="flex-1 min-w-0 min-h-0 flex flex-col p-2.5 gap-2">
            <div data-surface="assistant" class="flex flex-col flex-1 min-h-0 gap-2">
              <div data-assistant-chrome>${assistantChrome(state)}</div>
              <div id="assistant-thread-mount" class="assistant-thread-mount"></div>
            </div>
            <div data-surface-other class="hidden flex-1 min-h-0 overflow-y-auto"></div>
          </div>
          <div class="kit-dock-strip" data-dock-strip></div>
        `;
      bindAssistantRail(rail);
    }
    bindDock(rail);
    if (!state.dockSurface) state.dockSurface = "assistant";
    const surface = state.dockSurface;
    const assistantPane = rail.querySelector("[data-surface='assistant']");
    const otherPane = rail.querySelector("[data-surface-other]");
    if (assistantPane)
      assistantPane.classList.toggle("hidden", surface !== "assistant");
    if (otherPane) {
      otherPane.classList.toggle("hidden", surface === "assistant");
      if (surface !== "assistant") {
        const markup = dockSurfacesMarkup(state, {
          areaObject,
          objectHeader,
          esc: escHtml,
        });
        otherPane.innerHTML =
          surface === "inspector"
            ? inspectorPane(markup.inspector)
            : markup[surface] || "";
      }
    }
    const strip = rail.querySelector("[data-dock-strip]");
    if (strip)
      strip.innerHTML = DOCK_SURFACES.map(
        (s) =>
          `<button type="button" data-dock-surface="${s.id}" aria-pressed="${s.id === surface}" title="${s.label}" aria-label="${s.label}">${s.glyph}</button>`,
      ).join("");
    const chrome = rail.querySelector("[data-assistant-chrome]");
    if (chrome) chrome.innerHTML = assistantChrome(state);
    bindAttach(rail, state, renderChatRail);
    ensureAttachments(state, renderChatRail);
    const results = rail.querySelector("[data-assistant-results]");
    if (results) results.innerHTML = resultsMarkup(state);
    if (typeof notifyOperator === "function") notifyOperator();
  }

  // Dock inspector: the selected ontology object, its links, governed
  // actions (validate only), audit timeline, and raw record.
  const ontologyClient = createConduitClient();
  const inspector = { key: "", ref: null, model: null, tab: "props" };

  function currentInspectRef() {
    return state.inspectRef || ontologyRef(areaObject(state.hub, state.screen));
  }

  function inspectorPane(headerMarkup) {
    const ref = currentInspectRef();
    if (!ref) return headerMarkup;
    const key = `${ref.objectType}/${ref.primaryKey}`;
    if (inspector.key !== key) {
      inspector.key = key;
      inspector.ref = ref;
      inspector.model = { ref, loading: true, links: [] };
      loadInspector(ontologyClient, ref).then((model) => {
        if (inspector.key !== key) return;
        inspector.model = model;
        if (state.dockSurface === "inspector") renderChatRail();
      });
    }
    const header = state.inspectRef ? "" : headerMarkup;
    return `${header}<div class="kit-inspector mt-2">${inspectorMarkup(
      inspector.model,
      {
        tab: inspector.tab,
        esc: escHtml,
        audit: Array.isArray(state.hub?.audit) ? state.hub.audit : [],
      },
    )}</div>`;
  }

  function inspectObject(objectType, primaryKey) {
    state.inspectRef = objectType && primaryKey ? { objectType, primaryKey } : null;
    state.dockSurface = "inspector";
    state.chatCollapsed = false;
    renderChatRail();
  }
  window.inspectObject = inspectObject;

  function handleInspectorClick(target) {
    const tab = target.closest("[data-inspector-tab]");
    if (tab) {
      inspector.tab = tab.getAttribute("data-inspector-tab") || "props";
      renderChatRail();
      return true;
    }
    const link = target.closest("[data-inspect-type]");
    if (link) {
      inspectObject(
        link.getAttribute("data-inspect-type"),
        link.getAttribute("data-inspect-key"),
      );
      return true;
    }
    const check = target.closest("[data-inspector-validate]");
    if (check && inspector.ref && inspector.model) {
      const name = check.getAttribute("data-inspector-validate");
      const key = inspector.key;
      validateFor(ontologyClient, inspector.ref, name, inspector.model.object).then(
        (outcome) => {
          if (inspector.key !== key || !inspector.model) return;
          inspector.model = { ...inspector.model, outcome };
          renderChatRail();
        },
      );
      return true;
    }
    return false;
  }

  // Audit ledger: the recorded authorization chain with search, decision
  // filter, paging, and a client-side check of the hash links.
  let ledgerRoot = null;
  const auditLedger = createAuditLedger({
    getEntries: () => (Array.isArray(state.hub?.audit) ? state.hub.audit : []),
    rerender: () => {
      if (ledgerRoot && ledgerRoot.isConnected) auditLedger.render(ledgerRoot);
    },
  });
  function renderAuditLedger(node) {
    if (!node) return;
    const entries = Array.isArray(state.hub?.audit) ? state.hub.audit : [];
    if (!entries.length) {
      liveHub.renderAudit(node);
      return;
    }
    node.innerHTML = `<div class="so-view-enter so-panel kit-ledger-panel">
      <div class="kit-ledger-head"><h2>Authorization audit</h2><span class="kit-muted">${entries.length} recorded decisions</span></div>
      <div data-ledger-root></div></div>`;
    ledgerRoot = node.querySelector("[data-ledger-root]");
    auditLedger.render(ledgerRoot);
    auditLedger.bind(ledgerRoot);
  }

  function escHtml(v) {
    return String(v ?? "").replace(
      /[&<>"']/g,
      (c) =>
        ({
          "&": "&amp;",
          "<": "&lt;",
          ">": "&gt;",
          '"': "&quot;",
          "'": "&#39;",
        })[c],
    );
  }

  function bindDock(rail) {
    if (rail.dataset.dockBound === "1") return;
    rail.dataset.dockBound = "1";
    rail.addEventListener("click", (event) => {
      const target = event.target instanceof Element ? event.target : null;
      if (target && handleInspectorClick(target)) return;
      const btn = target && target.closest("[data-dock-surface]");
      if (!btn) return;
      state.dockSurface = btn.getAttribute("data-dock-surface");
      if (state.dockSurface === "inspector") state.inspectRef = null;
      renderChatRail();
    });
    let drag = null;
    rail.addEventListener("pointerdown", (event) => {
      const target = event.target instanceof Element ? event.target : null;
      const handle = target && target.closest("#dock-resize");
      if (!handle) return;
      drag = {
        startX: event.clientX,
        startWidth: rail.getBoundingClientRect().width,
        width: rail.getBoundingClientRect().width,
      };
      if (handle.setPointerCapture) handle.setPointerCapture(event.pointerId);
      event.preventDefault();
    });
    rail.addEventListener("pointermove", (event) => {
      if (!drag) return;
      drag.width = clampDockWidth(
        drag.startWidth + (drag.startX - event.clientX),
      );
      rail.style.width = `${drag.width}px`;
    });
    const finish = () => {
      if (!drag) return;
      writePref("conduit.console.dockWidth", drag.width);
      drag = null;
    };
    rail.addEventListener("pointerup", finish);
    rail.addEventListener("pointercancel", finish);
  }

  function bindAssistantRail(rail) {
    if (rail.dataset.bound === "1") return;
    rail.dataset.bound = "1";
    rail.addEventListener("change", (event) => {
      const target = event.target;
      if (
        !(target instanceof HTMLSelectElement) ||
        !target.matches("[data-assistant-mode]")
      )
        return;
      state.assistantMode = target.value;
      renderChatRail();
    });
    rail.addEventListener("click", async (event) => {
      const target = event.target instanceof Element ? event.target : null;
      if (!target) return;
      const modeButton = target.closest("[data-set-mode]");
      if (modeButton) {
        state.assistantMode = modeButton.getAttribute("data-set-mode");
        renderChatRail();
        return;
      }
      const remove = target.closest("[data-remove-chip]");
      if (remove) {
        const key = remove.getAttribute("data-remove-chip");
        state.assistantRemoved = [...(state.assistantRemoved || []), key];
        state.assistantAdded = (state.assistantAdded || []).filter(
          (chip) => `${chip.objectType}:${chip.id}` !== key,
        );
        renderChatRail();
        return;
      }
      const open = target.closest("[data-open-screen]");
      if (open) {
        switchScreen(open.getAttribute("data-open-screen"));
        return;
      }
      const resume = target.closest("[data-resume-write]");
      if (resume) {
        const form = resume.closest("form");
        const card = resume.closest("article");
        const index = Number(card?.getAttribute("data-result-index") || "0");
        const prior = (state.assistantResults || [])[index] || {};
        const args = Object.assign({}, prior.elicitation || {});
        form?.querySelectorAll("[name]").forEach((input) => {
          args[input.getAttribute("name")] = input.value;
        });
        if (resume.getAttribute("data-confirm") === "1") args.confirm = true;
        const resumed = await resumeWrite(state, args);
        rememberResult(state, resumed);
        renderApp();
        return;
      }
      const action = target.closest("[data-assistant-action]");
      if (!action) return;
      const result = await runAssistantAction(
        state,
        action.getAttribute("data-assistant-action"),
        action.getAttribute("data-chip-key"),
      );
      rememberResult(state, result);
      renderChatRail();
    });
    window.addEventListener("conduit-assistant-result", (event) => {
      rememberResult(state, event.detail || {});
      renderChatRail();
    });
  }

  function renderS13FleetOverview(root) {
    const profiles = [
      {
        code: "wp-edge-telemetry",
        name: "Edge Telemetry & Ingress",
        sub: "14 Environments · uds-core 0.31.2",
        badge: "Upgrade Ready",
        tone: "amber",
        pct: 78,
        note: "RUN-8844 stage 5/6",
      },
      {
        code: "wp-uds-core-base",
        name: "Core Platform Baseline",
        sub: "20 Environments · uds-core 0.31.2",
        badge: "Synced",
        tone: "green",
        pct: 100,
        note: "20/20 online converged",
      },
      {
        code: "wp-offline-sync",
        name: "Optical Diode & Courier",
        sub: "10 Environments · uds-core 0.30.9",
        badge: "Staged",
        tone: "blue",
        pct: 64,
        note: "Delta 41.2 MB queued",
      },
    ];

    const liveOps = [
      {
        actor: "Pipeline Agent",
        role: "SWARM",
        tone: "blue",
        ts: "09:54:28 PM",
        text: "Uploading signed zstd chunk 6/8 — <strong>spoke-west-relay</strong> store-and-forward queue healthy",
        a: ["Pipeline Runner", 6, 2],
        t: ["Chunk Signer", 8, 1],
        o: ["ConduitPackagingRun", 6, 1, "s07-pipeline"],
      },
      {
        actor: "Policy Sentinel",
        role: "SPECIALIST",
        tone: "emerald",
        ts: "09:42:10 PM",
        text: "Gate evaluation on <strong>PLN-2026-0418</strong> — holding for 2nd approver and 02:00 UTC window",
        a: ["Gate Evaluator", 5, 1],
        t: ["Window Clock", 14, 0],
        o: ["ConduitPlan", 14, 1, "s15-plan-detail"],
      },
      {
        actor: "M. Chen",
        role: "APPROVER",
        tone: "amber",
        ts: "08:58:12 PM",
        text: "Signed cryptographic approval 1/2 on <strong>PLN-2026-0418</strong> after verifying canary soak",
        a: null,
        t: null,
        o: ["ConduitApproval", 2, 1, "s16-approvals-board"],
      },
      {
        actor: "Drift Monitor",
        role: "SPECIALIST",
        tone: "emerald",
        ts: "08:19:04 PM",
        text: "Out-of-band kubectl memory edit detected on <strong>edge-west-02</strong> (768Mi vs 1Gi desired)",
        a: ["State Diff", 14, 1],
        t: ["Schema Check", 3, 0],
        o: ["ConduitReportedState", 3, 1, "s14-env-detail"],
      },
    ];

    root.innerHTML = `
        <div class="flex flex-col gap-3 h-full min-h-0">
          <div class="so-warn-card p-2 text-[12px] text-[#eab854]">Example data</div>
          <div class="so-compact-kpi-strip shrink-0">
            <button type="button" data-onclick="switchScreen('rm05-release-rollout')" class="so-card so-card-interactive p-2.5 text-left">
              <div class="text-[9.5px] uppercase tracking-[0.07em] text-[#6d7885]">Convergence</div>
              <div class="text-[16px] font-bold text-[#34d399] tnum mt-0.5">87.5%</div>
            </button>
            <button type="button" data-onclick="switchScreen('rm01-env-page')" class="so-card so-card-interactive p-2.5 text-left">
              <div class="text-[9.5px] uppercase tracking-[0.07em] text-[#6d7885]">Environments</div>
              <div class="text-[16px] font-bold text-[#f0f5fa] tnum mt-0.5">42 <span class="text-[10.5px] font-normal text-[#8b96a5]">/ 48</span></div>
            </button>
            <button type="button" data-onclick="switchScreen('s07-pipeline')" class="so-card so-card-interactive p-2.5 text-left">
              <div class="text-[9.5px] uppercase tracking-[0.07em] text-[#6d7885]">Packaging</div>
              <div class="text-[16px] font-bold text-[#f0f5fa] tnum mt-0.5">3 <span class="text-[10.5px] font-normal text-[#34d399]">Active</span></div>
            </button>
            <button type="button" data-onclick="switchScreen('s15-plan-detail')" class="so-card so-card-interactive p-2.5 text-left">
              <div class="text-[9.5px] uppercase tracking-[0.07em] text-[#6d7885]">Release plans</div>
              <div class="text-[16px] font-bold text-[#f0f5fa] tnum mt-0.5">4 <span class="text-[10.5px] font-normal text-[#9881f3]">Gated</span></div>
            </button>
            <button type="button" data-onclick="switchScreen('s14-env-detail')" class="so-card so-card-interactive p-2.5 text-left">
              <div class="text-[9.5px] uppercase tracking-[0.07em] text-[#6d7885]">Open drifts</div>
              <div class="text-[16px] font-bold text-[#e76a6e] tnum mt-0.5">5 <span class="text-[10.5px] font-normal text-[#e76a6e]">2 critical</span></div>
            </button>
          </div>
          <div class="so-tri-layout so-resp-stack flex gap-3 flex-1 min-h-0 items-stretch">
          <!-- Left Column (256px): Active Work Profiles & Spokes (Matched to f_01_18_cc Active New Builds) -->
          <div class="so-resp-full w-[216px] xl:w-[248px] shrink-0 flex flex-col gap-3 min-h-0 overflow-y-auto">
            <div class="so-panel p-3 space-y-2.5 flex-1">
              <div class="flex items-center justify-between">
                <span class="flex items-center gap-2 text-[13px] font-semibold text-[#ffffff]">${ic("cube")} <span>Active Work Profiles</span></span>
                <span class="tnum text-[11px] text-[#6d7885]">4 Total</span>
              </div>
              <div class="space-y-2.5">
                ${profiles
                  .map(
                    (p) => `
                  <div data-onclick="switchScreen('s12-lifecycle')" class="so-card so-card-interactive p-3 cursor-pointer space-y-1.5">
                    <div class="flex items-center justify-between">
                      <span class="code-id text-[12.5px] font-semibold text-[#5c9ce6]">${p.code}</span>
                      ${pill(p.badge, p.tone)}
                    </div>
                    <div class="text-[12.5px] font-semibold text-[#f0f5fa]">${p.name}</div>
                    <div class="text-[11px] text-[#6d7885]">${p.sub}</div>
                    <div class="pt-1">
                      <div class="flex justify-between text-[11px] mb-1"><span class="text-[#8b96a5]">Convergence</span><span class="tnum font-semibold text-[#f0f5fa]">${p.pct}%</span></div>
                      <div class="h-1.5 w-full bg-[#0b1014] rounded-full overflow-hidden">
                        <div class="h-full bg-[#32a467]" style="width:${p.pct}%"></div>
                      </div>
                    </div>
                    <div class="text-[10.5px] text-[#34d399] pt-0.5">&uarr; ${p.note}</div>
                  </div>
                `,
                  )
                  .join("")}
              </div>
            </div>
          </div>

          <!-- Center Column: Atmospheric Fleet Globe/Topology + Emerald Optimization Opportunities -->
          <div class="flex-1 min-w-0 flex flex-col gap-3 min-h-0">
            <!-- Top: Dark Spatial Fleet Topology Canvas (Matched to f_01_18_cc Globe Panel) -->
            <div class="so-panel flex-1 min-h-[300px] flex flex-col overflow-hidden relative" style="background: radial-gradient(circle at 50% 54%, #10233b 0%, #0b131c 48%, #090e12 100%);">
              <div class="px-3.5 py-2.5 flex flex-wrap items-center justify-between gap-2 z-10">
                <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <span class="flex items-center gap-2 text-[13.5px] font-semibold text-[#ffffff]">${ic("globe")} <span>Fleet Operations</span></span>
                  <span class="flex items-center gap-1.5 text-[11px] text-[#8b96a5]"><i class="w-2 h-2 rounded-full bg-[#4c90f0] inline-block"></i> Online</span>
                  <span class="flex items-center gap-1.5 text-[11px] text-[#8b96a5]"><i class="w-2 h-2 rounded-full bg-[#9881f3] inline-block"></i> Store-and-Forward</span>
                  <span class="flex items-center gap-1.5 text-[11px] text-[#8b96a5]"><i class="w-2 h-2 rounded-full bg-[#34d399] inline-block"></i> Optical Diode</span>
                </div>
                <span class="text-[11.5px] font-medium text-[#5c9ce6] border-b-2 border-[#5c9ce6] pb-0.5">Topology</span>
              </div>

              <!-- Orthographic fleet globe: night globe in dark theme, daytime Blue Marble in light -->
              <div id="conduit-globe-wrap" class="relative flex-1 min-h-[260px] w-full flex items-center justify-center overflow-hidden cursor-grab active:cursor-grabbing">
                <canvas id="conduit-globe-canvas" class="max-w-full"></canvas>
                <div data-onclick="switchScreen('s14-env-detail')" class="absolute left-3.5 bottom-3 px-2.5 py-1 rounded-[5px] bg-[#11171d]/90 border border-[#242f3b] text-[11px] text-[#dce4ee] flex items-center gap-1.5 cursor-pointer hover:border-[#3b82f6] tnum">
                  <span class="font-bold text-[#34d399]">48</span>
                  <span class="text-[#abb3bf]">Environments &amp; 6 Spokes</span>
                </div>
              </div>
            </div>

            <!-- Bottom: Conduit Optimization / Governance Opportunities (Matched 1:1 to f_01_18_cc) -->
            <div class="so-panel overflow-hidden shrink-0">
              <div class="flex items-center justify-between px-3.5 py-2 border-b border-[#1c242c]">
                <div class="flex items-center gap-2">
                  <span class="text-[#34d399] flex">${ic("bulb")}</span>
                  <span class="text-[13px] font-semibold text-[#ffffff]">Governance Opportunities</span>
                  ${pill("3 Active", "green")}
                </div>
                <div class="flex items-center gap-2 shrink-0">
                  <span class="px-2 py-0.5 rounded text-[10px] font-medium bg-[#191d32] text-[#9881f3] border border-[#2c3359] inline-flex items-center gap-1 whitespace-nowrap">
                    ${ic("pulse")} <span>Diagnostics</span>
                  </span>
                  <button data-onclick="switchScreen('s15-plan-detail')" class="text-[11px] text-[#8b96a5] hover:text-white whitespace-nowrap shrink-0">View All &rarr;</button>
                </div>
              </div>

              <div class="so-opt-grid so-grid-2 p-2.5 gap-2.5">
                <div class="so-opt-card p-2.5 flex flex-col justify-between gap-2 min-w-0">
                  <div class="min-w-0">
                    <div class="flex items-center justify-between text-[10.5px] gap-1">
                      <span class="truncate"><span class="text-[#60a5fa] font-medium">Drift Remediation</span> <span class="text-[#34d399] ml-1 tnum">96%</span></span>
                      <span class="text-[#6d7885] tnum shrink-0">02:00Z</span>
                    </div>
                    <div class="text-[12px] font-semibold text-[#ffffff] mt-1 leading-snug">
                      edge-west-02 ingress memory drift — promote CR-308 &amp; uds-core 0.31.2
                    </div>
                    <div class="text-[10.5px] text-[#34d399] mt-1">↗ Clears 3 drifted resources across West cohort</div>
                  </div>
                  <div class="flex items-center justify-between gap-1.5 pt-1 border-t border-[#1a2520]">
                    <span data-onclick="switchScreen('s15-plan-detail')" class="so-chip chip-ontology cursor-pointer whitespace-nowrap" title="ConduitPlan">${ic("checkCircle")} <span>Plan</span> <span class="rw-r">R:14</span> <span class="rw-w">W:1</span></span>
                    <button data-onclick="switchScreen('s15-plan-detail')" class="so-btn so-btn-sm so-btn-emerald shrink-0">&rarr; Review</button>
                  </div>
                </div>

                <div class="so-opt-card p-2.5 flex flex-col justify-between gap-2 min-w-0">
                  <div class="min-w-0">
                    <div class="flex items-center justify-between text-[10.5px] gap-1">
                      <span class="truncate"><span class="text-[#60a5fa] font-medium">Offline bundle spool</span> <span class="text-[#34d399] ml-1 tnum">91%</span></span>
                      <span class="text-[#6d7885] tnum shrink-0">63.8 MB</span>
                    </div>
                    <div class="text-[12px] font-semibold text-[#ffffff] mt-1 leading-snug">
                      Differential zstd bundle staged for spoke-diode-hall-b — export receipt
                    </div>
                    <div class="text-[10.5px] text-[#34d399] mt-1">↗ -71% diode transfer time vs full archive</div>
                  </div>
                  <div class="flex items-center justify-between gap-1.5 pt-1 border-t border-[#1a2520]">
                    <span data-onclick="switchScreen('s08-staged-run')" class="so-chip chip-ontology cursor-pointer whitespace-nowrap" title="ConduitPackagingRun">${ic("checkCircle")} <span>Run</span> <span class="rw-r">R:4</span></span>
                    <button data-onclick="switchScreen('s08-staged-run')" class="so-btn so-btn-sm so-btn-emerald shrink-0">&rarr; Review</button>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- Right Column (286px): Conduit Live Operations Stream (Matched 1:1 to f_01_18_cc Right Column!) -->
          <div class="so-resp-full so-panel w-[286px] shrink-0 flex flex-col min-h-0 overflow-hidden">
            <div class="px-3.5 py-2.5 border-b border-[#1c242c] flex items-center justify-between">
              <span class="flex items-center gap-2 text-[13.5px] font-semibold text-[#ffffff]">${ic("delivery")} <span>Operations</span></span>
            </div>

            <!-- 4-Count Sub-Header Strip (HUMAN / AGENT / RUNNERS / CONVERGED) -->
            <div class="grid grid-cols-4 py-2 border-b border-[#1c242c] text-center bg-[#0e1418]">
              <div><div class="text-[13px] font-bold text-[#eab854] tnum">4</div><div class="text-[8.5px] uppercase tracking-[0.08em] text-[#6d7885]">OPERATOR</div></div>
              <div><div class="text-[13px] font-bold text-[#60a5fa] tnum">3</div><div class="text-[8.5px] uppercase tracking-[0.08em] text-[#6d7885]">AGENTS</div></div>
              <div><div class="text-[13px] font-bold text-[#9881f3] tnum">6</div><div class="text-[8.5px] uppercase tracking-[0.08em] text-[#6d7885]">SPOKES</div></div>
              <div><div class="text-[13px] font-bold text-[#34d399] tnum">42</div><div class="text-[8.5px] uppercase tracking-[0.08em] text-[#6d7885]">SYNCED</div></div>
            </div>

            <div class="p-3 space-y-3 overflow-y-auto flex-1">
              ${liveOps
                .map(
                  (ev) => `
                <div class="so-stream-item so-stream-${ev.tone} space-y-1.5 pb-2.5 border-b border-[#171f26]">
                  <div class="flex items-center justify-between">
                    <span class="flex items-center gap-1.5"><span class="font-semibold text-[12px] text-[#f0f5fa]">${ev.actor}</span>${pill(ev.role, ev.tone === "emerald" ? "green" : ev.tone)}</span>
                    <span class="tnum text-[10px] text-[#596573]">${ev.ts}</span>
                  </div>
                  <div class="text-[11.5px] text-[#b6c2cf] leading-snug">${ev.text}</div>
                  <div class="flex flex-wrap gap-1 pt-0.5">
                    ${ev.a ? agentChip(ev.a[0], ev.a[1], ev.a[2]) : ""}
                    ${ev.t ? toolChip(ev.t[0], ev.t[1], ev.t[2]) : ""}
                    ${ontChip(ev.o[0], ev.o[1], ev.o[2], ev.o[3])}
                  </div>
                </div>
              `,
                )
                .join("")}
            </div>
          </div>
        </div>
        </div>
      `;
    initConduitGlobe();
  }

  const CONDUIT_METRO_HUBS = [
    [-74.0, 40.7, 2.2, 18],
    [-71.1, 42.3, 1.8, 12],
    [-75.2, 39.9, 1.8, 10],
    [-77.0, 38.9, 2.0, 14],
    [-76.3, 36.9, 1.8, 11],
    [-87.6, 41.8, 2.1, 16],
    [-83.0, 42.3, 1.6, 10],
    [-79.4, 43.7, 1.7, 11],
    [-84.4, 33.7, 1.9, 13],
    [-80.2, 25.8, 1.7, 10],
    [-95.4, 29.7, 2.0, 14],
    [-96.8, 32.8, 2.0, 14],
    [-93.3, 45.0, 1.6, 10],
    [-90.2, 38.6, 1.5, 9],
    [-80.8, 35.2, 1.6, 10],
    [-105.0, 39.7, 1.7, 11],
    [-112.1, 33.4, 1.7, 10],
    [-118.2, 34.0, 2.1, 15],
    [-122.4, 37.8, 1.9, 12],
    [-122.3, 47.6, 1.9, 12],
    [-123.1, 49.3, 1.5, 8],
    [-99.1, 19.4, 1.9, 12],
    [-113.5, 53.5, 1.3, 6],
    [-73.6, 45.5, 1.5, 8],
  ];
  const CONDUIT_NIGHT_LIGHTS = [];
  (function seedConduitLights() {
    let s = 4219;
    const rand = () => {
      s = (s * 16807) % 2147483647;
      return (s - 1) / 2147483646;
    };
    CONDUIT_METRO_HUBS.forEach(([lng, lat, size, count]) => {
      CONDUIT_NIGHT_LIGHTS.push([lng, lat, size, 0.9]);
      for (let i = 0; i < count; i++) {
        const spread = i < count * 0.6 ? 1.35 : 3.0;
        CONDUIT_NIGHT_LIGHTS.push([
          lng + (rand() - 0.5) * spread * 1.55,
          lat + (rand() - 0.5) * spread,
          0.55 + rand() * 0.85,
          0.22 + rand() * 0.52,
        ]);
      }
    });
  })();

  let conduitGlobeRot = [96, -37, 0];

  function initConduitGlobe() {
    const wrap = document.getElementById("conduit-globe-wrap");
    const canvas = document.getElementById("conduit-globe-canvas");
    if (!wrap || !canvas) return;

    const width = wrap.clientWidth || 560;
    const height = wrap.clientHeight || 290;
    const dpr = window.devicePixelRatio || 1;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    canvas.style.width = width + "px";
    canvas.style.height = height + "px";

    const ctx = canvas.getContext("2d");
    ctx.scale(dpr, dpr);

    const DEG2RAD = Math.PI / 180;
    const baseR = Math.min(width, height) * 0.465;
    // Light theme draws the daytime Blue Marble; dark keeps the night globe.
    const light =
      document.querySelector(".operator-root")?.getAttribute("data-theme") ===
      "light";
    const day = light
      ? dayTexture(() => {
          if (document.getElementById("conduit-globe-canvas")) initConduitGlobe();
        })
      : null;

    function project(coord) {
      const lam = (coord[0] + conduitGlobeRot[0]) * DEG2RAD;
      const phi = coord[1] * DEG2RAD;
      const phi0 = -conduitGlobeRot[1] * DEG2RAD;
      const cosc =
        Math.sin(phi0) * Math.sin(phi) +
        Math.cos(phi0) * Math.cos(phi) * Math.cos(lam);
      if (cosc < 0) return null;
      return [
        baseR * Math.cos(phi) * Math.sin(lam) + width / 2,
        baseR *
          (Math.sin(phi0) * Math.cos(phi) * Math.cos(lam) -
            Math.cos(phi0) * Math.sin(phi)) +
          height / 2 +
          4,
      ];
    }

    function geoDist(a, b) {
      const l1 = a[0] * DEG2RAD,
        p1 = a[1] * DEG2RAD,
        l2 = b[0] * DEG2RAD,
        p2 = b[1] * DEG2RAD;
      const c =
        Math.sin(p1) * Math.sin(p2) +
        Math.cos(p1) * Math.cos(p2) * Math.cos(l2 - l1);
      return Math.acos(Math.max(-1, Math.min(1, c)));
    }

    const landPolys = [
      [
        [-168, 65],
        [-162, 60],
        [-152, 60],
        [-140, 60],
        [-130, 54],
        [-125, 48],
        [-124, 40],
        [-120, 34],
        [-117, 32],
        [-110, 24],
        [-105, 20],
        [-97, 16],
        [-88, 16],
        [-83, 10],
        [-79, 9],
        [-77, 8],
        [-82, 15],
        [-87, 21],
        [-90, 21],
        [-97, 22],
        [-97, 28],
        [-90, 29],
        [-83, 29],
        [-81, 25],
        [-80, 27],
        [-81, 31],
        [-76, 35],
        [-75, 39],
        [-71, 41],
        [-70, 43],
        [-67, 45],
        [-64, 44],
        [-60, 46],
        [-65, 49],
        [-56, 52],
        [-62, 58],
        [-68, 62],
        [-78, 62],
        [-82, 55],
        [-90, 57],
        [-95, 61],
        [-105, 68],
        [-125, 70],
        [-145, 70],
        [-162, 70],
        [-168, 65],
      ],
      [
        [-74, 62],
        [-60, 60],
        [-44, 60],
        [-22, 70],
        [-18, 78],
        [-42, 82],
        [-62, 80],
        [-74, 75],
        [-74, 62],
      ],
      [
        [-85, 22],
        [-79, 22],
        [-74, 20],
        [-77, 20],
        [-84, 21],
        [-85, 22],
      ],
      [
        [-78, 9],
        [-72, 12],
        [-62, 10],
        [-50, 1],
        [-35, -6],
        [-38, -15],
        [-43, -23],
        [-53, -33],
        [-58, -38],
        [-70, -18],
        [-77, -12],
        [-81, -5],
        [-78, 2],
        [-78, 9],
      ],
      [
        [-10, 36],
        [-9, 43],
        [-2, 44],
        [-4, 48],
        [2, 51],
        [8, 54],
        [8, 58],
        [18, 68],
        [28, 70],
        [40, 67],
        [40, 47],
        [28, 41],
        [15, 38],
        [5, 43],
        [-5, 36],
        [-10, 36],
      ],
    ];

    const fleetNodes = [
      {
        lng: -77.4,
        lat: 38.9,
        color: "#60a5fa",
        r: 4.5,
        label: "edge-east-01 (Hub)",
        screen: "s14-env-detail",
        dx: 9,
        dy: 2,
      },
      {
        lng: -122.3,
        lat: 47.6,
        color: "#ec9a3c",
        r: 4.5,
        label: "edge-west-02 · 3 drifts",
        screen: "s14-env-detail",
        dx: -8,
        dy: -7,
        rightAlign: true,
      },
      {
        lng: -104.8,
        lat: 38.8,
        color: "#34d399",
        r: 4.2,
        label: "offline-vault-04 (Diode)",
        screen: "s08-staged-run",
        dx: -8,
        dy: 12,
        rightAlign: true,
      },
      {
        lng: -90.5,
        lat: 28.2,
        color: "#e76a6e",
        r: 4.0,
        label: "remote-rig-09",
        screen: "s17-spokes-audit",
        dx: 8,
        dy: 11,
      },
      {
        lng: -87.6,
        lat: 41.8,
        color: "#9881f3",
        r: 4.0,
        label: "metro-core-03",
        screen: "s17-spokes-audit",
        dx: -6,
        dy: -12,
        rightAlign: true,
      },
      {
        lng: -147.7,
        lat: 64.8,
        color: "#60a5fa",
        r: 3.8,
        label: "forward-arctic-01",
        screen: "s05-backlog",
        dx: 8,
        dy: -4,
      },
      {
        lng: -117.2,
        lat: 32.7,
        color: "#4c90f0",
        r: 3.4,
        label: "",
        screen: "s14-env-detail",
      },
      {
        lng: -95.4,
        lat: 29.7,
        color: "#9881f3",
        r: 3.4,
        label: "",
        screen: "s17-spokes-audit",
      },
      {
        lng: -71.1,
        lat: 42.3,
        color: "#4c90f0",
        r: 3.4,
        label: "",
        screen: "s14-env-detail",
      },
      {
        lng: -80.2,
        lat: 25.8,
        color: "#34d399",
        r: 3.4,
        label: "",
        screen: "s08-staged-run",
      },
    ];

    const corridors = [
      [-77.4, 38.9, -122.3, 47.6, "#ec9a3c"],
      [-77.4, 38.9, -104.8, 38.8, "#34d399"],
      [-77.4, 38.9, -90.5, 28.2, "#e76a6e"],
      [-77.4, 38.9, -87.6, 41.8, "#9881f3"],
      [-122.3, 47.6, -147.7, 64.8, "#60a5fa"],
    ];

    function draw() {
      const cx = width / 2,
        cy = height / 2 + 4;
      ctx.clearRect(0, 0, width, height);

      // 1. Atmospheric outer blue limb halo (f_01_18_cc.png)
      const halo = ctx.createRadialGradient(
        cx,
        cy,
        baseR * 0.86,
        cx,
        cy,
        baseR * 1.28,
      );
      halo.addColorStop(0, "rgba(45, 114, 210, 0.36)");
      halo.addColorStop(0.45, "rgba(30, 64, 135, 0.15)");
      halo.addColorStop(1, "rgba(9, 14, 19, 0)");
      ctx.fillStyle = halo;
      ctx.beginPath();
      ctx.ellipse(cx, cy, baseR * 1.28, baseR * 1.28, 0, 0, Math.PI * 2);
      ctx.fill();

      if (day) {
        drawDayDisc(cx, cy);
      } else {
        drawNightDisc(cx, cy);
      }

      // 5. Spoke relay great-circle links
      ctx.save();
      ctx.beginPath();
      ctx.ellipse(cx, cy, baseR, baseR, 0, 0, Math.PI * 2);
      ctx.clip();
      drawCorridors();
      ctx.restore();
      drawRimAndPins();
    }

    function drawDayDisc(cx, cy) {
      const size = Math.max(2, Math.round(baseR * 2 * dpr));
      const off = document.createElement("canvas");
      off.width = size;
      off.height = size;
      const g = off.getContext("2d");
      if (!g) return;
      const img = g.createImageData(size, size);
      paintDisc(img.data, size, day, conduitGlobeRot);
      g.putImageData(img, 0, 0);
      ctx.drawImage(off, cx - baseR, cy - baseR, baseR * 2, baseR * 2);
      ctx.save();
      ctx.beginPath();
      ctx.ellipse(cx, cy, baseR, baseR, 0, 0, Math.PI * 2);
      ctx.clip();
      drawGraticules("rgba(255, 255, 255, 0.14)");
      ctx.restore();
    }

    function drawGraticules(color) {
      ctx.strokeStyle = color;
      ctx.lineWidth = 0.6;
      for (let lat = -45; lat <= 75; lat += 20) {
        ctx.beginPath();
        let st = false;
        for (let lng = -180; lng <= 180; lng += 5) {
          const p = project([lng, lat]);
          if (p) {
            if (!st) {
              ctx.moveTo(p[0], p[1]);
              st = true;
            } else ctx.lineTo(p[0], p[1]);
          } else st = false;
        }
        ctx.stroke();
      }
      for (let lng = -160; lng <= 40; lng += 20) {
        ctx.beginPath();
        let st = false;
        for (let lat = -60; lat <= 80; lat += 4) {
          const p = project([lng, lat]);
          if (p) {
            if (!st) {
              ctx.moveTo(p[0], p[1]);
              st = true;
            } else ctx.lineTo(p[0], p[1]);
          } else st = false;
        }
        ctx.stroke();
      }
    }

    function drawNightDisc(cx, cy) {
      // 2. Deep bathymetric ocean sphere
      const ocean = ctx.createRadialGradient(
        cx - baseR * 0.22,
        cy - baseR * 0.22,
        baseR * 0.08,
        cx,
        cy,
        baseR,
      );
      ocean.addColorStop(0, "#0e2b4a");
      ocean.addColorStop(0.62, "#07182c");
      ocean.addColorStop(1, "#040c18");
      ctx.fillStyle = ocean;
      ctx.beginPath();
      ctx.ellipse(cx, cy, baseR, baseR, 0, 0, Math.PI * 2);
      ctx.fill();

      // Subtle graticules clipped inside sphere
      ctx.save();
      ctx.beginPath();
      ctx.ellipse(cx, cy, baseR, baseR, 0, 0, Math.PI * 2);
      ctx.clip();

      drawGraticules("rgba(76, 144, 240, 0.08)");

      // 3. Dark satellite continents
      ctx.fillStyle = "#08111c";
      ctx.strokeStyle = "rgba(92, 156, 230, 0.28)";
      ctx.lineWidth = 0.85;
      landPolys.forEach((ring) => {
        ctx.beginPath();
        let st = false;
        ring.forEach((c) => {
          const p = project(c);
          if (p) {
            if (!st) {
              ctx.moveTo(p[0], p[1]);
              st = true;
            } else ctx.lineTo(p[0], p[1]);
          } else st = false;
        });
        ctx.fill();
        ctx.stroke();
      });

      // 4. Golden city-light clusters
      CONDUIT_NIGHT_LIGHTS.forEach((c) => {
        if (
          geoDist([c[0], c[1]], [-conduitGlobeRot[0], -conduitGlobeRot[1]]) <
          1.46
        ) {
          const pt = project([c[0], c[1]]);
          if (pt) {
            if (c[2] > 1.35) {
              const g = ctx.createRadialGradient(
                pt[0],
                pt[1],
                0,
                pt[0],
                pt[1],
                c[2] * 3.2,
              );
              g.addColorStop(0, "rgba(255, 238, 190, " + c[3].toFixed(2) + ")");
              g.addColorStop(
                0.4,
                "rgba(245, 180, 75, " + (c[3] * 0.38).toFixed(2) + ")",
              );
              g.addColorStop(1, "rgba(245, 180, 75, 0)");
              ctx.fillStyle = g;
              ctx.beginPath();
              ctx.ellipse(
                pt[0],
                pt[1],
                c[2] * 3.2,
                c[2] * 3.2,
                0,
                0,
                Math.PI * 2,
              );
              ctx.fill();
            } else {
              ctx.fillStyle = "rgba(252, 211, 125, " + c[3].toFixed(2) + ")";
              ctx.beginPath();
              ctx.ellipse(pt[0], pt[1], c[2], c[2], 0, 0, Math.PI * 2);
              ctx.fill();
            }
          }
        }
      });

      ctx.restore();
    }

    function drawCorridors() {
      corridors.forEach((cor) => {
        const pA = project([cor[0], cor[1]]);
        const pB = project([cor[2], cor[3]]);
        if (pA && pB) {
          const mx = (pA[0] + pB[0]) * 0.5;
          const my =
            (pA[1] + pB[1]) * 0.5 -
            Math.hypot(pB[0] - pA[0], pB[1] - pA[1]) * 0.22;
          ctx.save();
          ctx.strokeStyle = cor[4];
          ctx.globalAlpha = 0.55;
          ctx.lineWidth = 1.35;
          ctx.setLineDash([3, 3]);
          ctx.beginPath();
          ctx.moveTo(pA[0], pA[1]);
          ctx.quadraticCurveTo(mx, my, pB[0], pB[1]);
          ctx.stroke();
          ctx.restore();
        }
      });
    }

    function drawRimAndPins() {
      const cx = width / 2,
        cy = height / 2 + 4;
      // Crisp atmospheric rim outline
      ctx.strokeStyle = "rgba(96, 165, 250, 0.35)";
      ctx.lineWidth = 1.1;
      ctx.beginPath();
      ctx.ellipse(cx, cy, baseR, baseR, 0, 0, Math.PI * 2);
      ctx.stroke();

      // 6. Glowing Kubernetes Environment & Spoke Pins + Clean Callouts
      ctx.font = "500 10px Inter, sans-serif";
      fleetNodes.forEach((n) => {
        if (
          geoDist([n.lng, n.lat], [-conduitGlobeRot[0], -conduitGlobeRot[1]]) >
          1.48
        )
          return;
        const pt = project([n.lng, n.lat]);
        if (!pt) return;
        const glow = ctx.createRadialGradient(
          pt[0],
          pt[1],
          0,
          pt[0],
          pt[1],
          n.r * 3.2,
        );
        glow.addColorStop(0, n.color);
        glow.addColorStop(1, "rgba(0,0,0,0)");
        ctx.fillStyle = glow;
        ctx.beginPath();
        ctx.ellipse(pt[0], pt[1], n.r * 3.2, n.r * 3.2, 0, 0, Math.PI * 2);
        ctx.fill();

        ctx.fillStyle = n.color;
        ctx.beginPath();
        ctx.ellipse(pt[0], pt[1], n.r, n.r, 0, 0, Math.PI * 2);
        ctx.fill();
        ctx.strokeStyle = "#090e12";
        ctx.lineWidth = 1.2;
        ctx.stroke();

        if (n.label && (width >= 420 || n.r >= 4.2)) {
          const tw = ctx.measureText(n.label).width;
          const rawLx = n.rightAlign ? pt[0] + n.dx - tw - 10 : pt[0] + n.dx;
          const lx = Math.max(6, Math.min(width - tw - 16, rawLx));
          const ly = Math.max(6, Math.min(height - 22, pt[1] + n.dy - 10));
          ctx.fillStyle = "rgba(13, 19, 24, 0.86)";
          ctx.strokeStyle = "rgba(42, 56, 72, 0.85)";
          ctx.lineWidth = 1;
          ctx.beginPath();
          ctx.roundRect(lx, ly, tw + 10, 16, 4);
          ctx.fill();
          ctx.stroke();
          ctx.fillStyle = "#e6edf3";
          ctx.fillText(n.label, lx + 5, ly + 11.5);
        }
      });
    }

    draw();

    if (window._conduitGlobeRO) window._conduitGlobeRO.disconnect();
    if (typeof ResizeObserver !== "undefined") {
      let roRaf = 0;
      window._conduitGlobeRO = new ResizeObserver(() => {
        cancelAnimationFrame(roRaf);
        roRaf = requestAnimationFrame(() => {
          const nw = wrap.clientWidth,
            nh = wrap.clientHeight;
          if (
            nw > 40 &&
            nh > 40 &&
            (Math.abs(nw - width) > 2 || Math.abs(nh - height) > 2)
          ) {
            initConduitGlobe();
          }
        });
      });
      window._conduitGlobeRO.observe(wrap);
    }

    // Redraw with the other texture when the theme flips.
    if (window._conduitGlobeMO) window._conduitGlobeMO.disconnect();
    // The operator root may not exist yet on first paint, so watch the body.
    const themeRoot = document.body;
    if (themeRoot && typeof MutationObserver !== "undefined") {
      window._conduitGlobeMO = new MutationObserver(() => {
        if (document.getElementById("conduit-globe-canvas")) initConduitGlobe();
      });
      window._conduitGlobeMO.observe(themeRoot, {
        attributes: true,
        attributeFilter: ["data-theme"],
        subtree: true,
      });
    }

    let dragging = false,
      startPt = null,
      startRot = null;
    canvas.onpointerdown = (e) => {
      dragging = true;
      startPt = [e.clientX, e.clientY];
      startRot = [...conduitGlobeRot];
      canvas.setPointerCapture(e.pointerId);
    };
    canvas.onpointermove = (e) => {
      if (!dragging || !startPt) return;
      conduitGlobeRot[0] = startRot[0] + (e.clientX - startPt[0]) * 0.35;
      conduitGlobeRot[1] = Math.max(
        -68,
        Math.min(68, startRot[1] - (e.clientY - startPt[1]) * 0.35),
      );
      draw();
    };
    canvas.onpointerup = (e) => {
      if (
        dragging &&
        startPt &&
        Math.hypot(e.clientX - startPt[0], e.clientY - startPt[1]) < 5
      ) {
        const rect = canvas.getBoundingClientRect();
        const mx = e.clientX - rect.left,
          my = e.clientY - rect.top;
        for (const n of fleetNodes) {
          const pt = project([n.lng, n.lat]);
          if (pt && Math.hypot(pt[0] - mx, pt[1] - my) < 14) {
            switchScreen(n.screen);
            break;
          }
        }
      }
      dragging = false;
    };
  }

  // =========================================================================
  // SCREEN 1: CHAT-FIRST INTAKE (Matched 1:1 to Conduit f_05_08_comms_det.png!)
  // =========================================================================
  function renderS01Intake(root) {
    const stored = state.attachments?.items || [];

    root.innerHTML = `
        <div class="space-y-3 h-full flex flex-col">
          <!-- Top Conduit Striped Resolution Bar -->
          <div class="so-panel px-3.5 py-2 flex items-center gap-3 shrink-0">
            <span class="text-[#eab854] flex">${ic("inbox")}</span>
            <span class="text-[12px] font-medium text-[#f0f5fa] shrink-0">New-Work Intake</span>
            <div class="flex-1 h-2 bg-[#090e12] rounded-full overflow-hidden">
              <div class="h-full so-striped-bar" style="width:100%"></div>
            </div>
            <span class="tnum text-[11.5px] font-semibold text-[#34d399]">3 Context Cards Ready</span>
          </div>

          <!-- Split Layout Matched to f_05_08_comms_det.png -->
          <div class="so-resp-stack flex-1 min-h-0 flex gap-3 items-stretch">
            <!-- Left: Operator Message with Inline Highlighted Entities & Attachment Chips -->
            <div class="so-resp-full w-[42%] flex flex-col gap-3 min-h-0">
              <div class="so-panel p-4 space-y-3 flex-1 overflow-y-auto">
                <div class="flex items-center gap-2.5 pb-2.5 border-b border-[#1c242c]">
                  <div class="w-8 h-8 rounded-full bg-[#243447] text-[#7cb4ff] font-semibold text-[12px] flex items-center justify-center">JA</div>
                  <div>
                    <div class="text-[12.5px] font-semibold text-[#ffffff]">J. Alvarez</div>
                    <div class="text-[11px] text-[#6d7885]">Platform Operator · Fleet Delivery</div>
                  </div>
                </div>

                <div class="text-[12.5px] text-[#dce4ee] leading-relaxed space-y-2.5">
                  <div class="text-[10px] uppercase tracking-[0.08em] text-[#eab854]">Example data · sample message</div>
                  <p>Release Control,</p>
                  <p>
                    Cluster <span data-onclick="switchScreen('s14-env-detail')" class="entity-span">edge-west-02</span> hit repeated <span class="entity-span-warn">ingress-gateway 768Mi OOM</span> restarts during burst replay over <span data-onclick="switchScreen('s17-spokes-audit')" class="entity-span">spoke-west-relay</span>.
                  </p>
                  <p>
                    I've attached the signed <span data-onclick="switchScreen('s08-staged-run')" class="entity-span">uds-telemetry 0.31.2 tarball</span>, the patched <span data-onclick="switchScreen('s10-diff')" class="entity-span">values.yaml raising memory to 1Gi</span>, and the alert screenshot. Let's stage the packaging run across all 14 environments in <span data-onclick="switchScreen('s12-lifecycle')" class="entity-span">wp-edge-telemetry</span>.
                  </p>
                </div>

                <div class="pt-2 border-t border-[#1c242c] space-y-1.5">
                  <div class="text-[10px] font-semibold uppercase tracking-[0.09em] text-[#6d7885]">ATTACHMENTS (${stored.length}) · stored on the Hub, read-only</div>
                  ${attachmentList(stored)}
                </div>
              </div>

              <!-- Intake Follow-up Input -->
              <div class="so-panel p-2.5 flex items-center gap-2 shrink-0">
                ${attachControl(state.attachments?.reason, "intake")}
                <input type="text" placeholder="Add rollout wave or spoke constraint..." class="so-input flex-1" />
                <button data-onclick="switchScreen('s04-scope-plan')" class="so-btn so-btn-sm so-btn-blue">Send</button>
              </div>
            </div>

            <!-- Right: 3 Conduit Context Resolution Cards (Work profile, Packaging run, Proposed Plan) + Action Footer -->
            <div class="flex-1 min-w-0 flex flex-col justify-between gap-2.5 min-h-0 overflow-y-auto">
              <div class="so-grid-2 gap-2.5">
                <!-- Context Card 1: Work profile -->
                <div class="so-opt-card p-3 space-y-2">
                  <div class="flex items-center justify-between">
                    <span class="flex items-center gap-1.5 font-semibold text-[12.5px] text-[#ffffff]"><span class="text-[#34d399]">${ic("checkCircle")}</span><span>Work profile · wp-edge-telemetry</span></span>
                    <span class="tnum text-[11px] text-[#34d399]">98%</span>
                  </div>
                  <div class="text-[11px] text-[#34d399]">&rarr; Status: Upgrade Available · 14 bound Environments</div>
                  <div class="space-y-1.5 pt-1 text-[11px]">
                    <div class="flex gap-2"><span class="w-4 h-4 rounded-full bg-[#192430] text-[#8b96a5] text-[10px] flex items-center justify-center shrink-0">1</span><div><div class="font-medium text-[#e6edf3]">Resolve fleet cohort</div><div class="text-[#6d7885]">14 clusters across 3 Spokes (3 drifted)</div><div class="mt-1">${ontChip("ConduitWorkProfile", 1, 0, "s12-lifecycle")}</div></div></div>
                    <div class="flex gap-2"><span class="w-4 h-4 rounded-full bg-[#192430] text-[#8b96a5] text-[10px] flex items-center justify-center shrink-0">2</span><div><div class="font-medium text-[#e6edf3]">Verify values.yaml diff</div><div class="text-[#6d7885]">Memory limit 768Mi &rarr; 1Gi (+12 -4 lines)</div><div class="mt-1">${ontChip("ConduitChangeRequest", 3, 1, "s10-diff")}</div></div></div>
                  </div>
                </div>

                <!-- Context Card 2: Packaging run -->
                <div class="so-opt-card p-3 space-y-2">
                  <div class="flex items-center justify-between">
                    <span class="flex items-center gap-1.5 font-semibold text-[12.5px] text-[#ffffff]"><span class="text-[#34d399]">${ic("checkCircle")}</span><span>Packaging run · RUN-8844</span></span>
                    <span class="tnum text-[11px] text-[#34d399]">95%</span>
                  </div>
                  <div class="text-[11px] text-[#34d399]">&rarr; Status: Queued · 6-step deterministic pipeline</div>
                  <div class="space-y-1.5 pt-1 text-[11px]">
                    <div class="flex gap-2"><span class="w-4 h-4 rounded-full bg-[#192430] text-[#8b96a5] text-[10px] flex items-center justify-center shrink-0">1</span><div><div class="font-medium text-[#e6edf3]">SPDX &amp; vulnerability scan</div><div class="text-[#6d7885]">418 packages · 0 critical / 0 high CVEs</div><div class="mt-1">${toolChip("SPDX Scanner", 418, 0)}</div></div></div>
                    <div class="flex gap-2"><span class="w-4 h-4 rounded-full bg-[#192430] text-[#8b96a5] text-[10px] flex items-center justify-center shrink-0">2</span><div><div class="font-medium text-[#e6edf3]">Build signed diode delta</div><div class="text-[#6d7885]">41.2 MB zstd delta · key prod-root-2026</div><div class="mt-1">${ontChip("ConduitPackagingRun", 6, 1, "s07-pipeline")}</div></div></div>
                  </div>
                </div>
              </div>

              <!-- Context Card 3: Proposed Plan + Create Plan CTA (Matched to f_05_08 bottom-right card) -->
              <div class="so-panel p-3.5 space-y-2.5 flex-1 flex flex-col justify-between">
                <div class="space-y-2">
                  <div class="flex items-center justify-between border-b border-[#1c242c] pb-2">
                    <div class="flex items-center gap-2">
                      <span class="text-[#34d399] flex">${ic("shield")}</span>
                      <span class="font-semibold text-[13px] text-[#ffffff]">Proposed Plan · PLN-2026-0421</span>
                      ${pill("Proposed · 3 Waves", "blue")}
                    </div>
                    ${ontChip("ConduitPlan", 14, 1, "s15-plan-detail")}
                  </div>
                  <div class="text-[12px] text-[#b6c2cf] leading-relaxed">
                    Stages <strong>Wave 1 Canary</strong> (2 online clusters), <strong>Wave 2 Regional Edge</strong> (6 clusters in <span class="code-id text-[#e6edf3]">02:00–05:00 UTC</span> window), and <strong>Wave 3 Disconnected Spokes</strong> (6 diode/courier exports). Requires 2/2 <span class="code-id text-[#e6edf3]">@fleet-approvers</span> signatures before execution.
                  </div>
                </div>

                <div class="pt-2.5 border-t border-[#1c242c] flex items-center justify-between">
                  <div class="flex items-center gap-2">
                    ${governedBtn({ label: "Rollout Immediately", role: "Operator", auditEvent: "plan.execute", onClickJs: "", cls: "so-btn so-btn-sm", iconName: "lock", blockedReason: "Plan status is Proposed (requires 2/2 Approvals)" })}
                    <button data-onclick="switchScreen('s10-diff')" class="so-btn so-btn-sm">Inspect Diff</button>
                  </div>
                  ${governedBtn({ label: "Create plan from this", role: "Operator", auditEvent: "plan.propose", onClickJs: "switchScreen('s04-scope-plan')", cls: "so-btn so-btn-emerald", iconName: "check" })}
                </div>
              </div>
            </div>
          </div>
        </div>
      `;
    bindAttach(root, state, renderApp);
    ensureAttachments(state, renderApp);
  }

  // =========================================================================
  // SCREEN 2: WORKSPACE INVENTORY
  // =========================================================================
  function renderS02Workspaces(root) {
    mountDevWorkspaces(root, { view: "list" });
  }

  // =========================================================================
  // SCREEN 3: SAVED VIEWS
  // =========================================================================
  function renderS03SavedViews(root) {
    const list = state.savedViewsForceEmpty ? [] : state.savedViews;
    root.innerHTML = `
        <div class="space-y-3 h-full flex flex-col">
          <div class="so-panel p-3.5 flex flex-wrap items-center justify-between gap-2.5 shrink-0">
            <div>
              <div class="flex items-center gap-2.5">
                <span class="text-[15px] font-bold text-[#ffffff]">Saved Views</span>
                ${ontChip("ConduitSavedView", 5, 1)}
              </div>
              <div class="text-[11.5px] text-[#6d7885] mt-0.5">Manage pinned sidebar filters, shared team queries, and alert presets</div>
            </div>
            <div class="flex items-center gap-2">
              <button data-onclick="state.savedViewsForceEmpty=!state.savedViewsForceEmpty; renderMainWorkspace()" class="so-btn so-btn-sm">
                ${state.savedViewsForceEmpty ? "Restore 5 Views" : "Toggle Empty State"}
              </button>
              ${governedBtn({ label: "New Saved View", role: "Operator", auditEvent: "saved_view.created", onClickJs: "switchScreen('s02-workspaces')", cls: "so-btn so-btn-blue" })}
            </div>
          </div>

          ${
            list.length > 0
              ? `
            <div class="so-panel overflow-hidden">
              <div class="so-table-scroll"><table class="so-table">
                <thead><tr><th>Pinned</th><th>Saved View &amp; Query</th><th>Target Object</th><th>Visibility</th><th>Matches</th><th>Actions</th></tr></thead>
                <tbody>
                  ${list
                    .map(
                      (v) => `
                    <tr>
                      <td>
                        <button data-onclick="toggleSavedViewPin('${v.id}')" class="so-btn so-btn-sm ${v.pinned ? "so-btn-blue" : ""}">${ic("pin")} <span>${v.pinned ? "Pinned" : "Pin"}</span></button>
                      </td>
                      <td>
                        <div class="font-semibold text-[#f0f5fa]">${v.name}</div>
                        <div class="code-id text-[10.5px] text-[#60a5fa] mt-0.5">${v.query}</div>
                      </td>
                      <td>${ontChip(v.targetType, v.hits, 0)}</td>
                      <td>${pill(v.status, v.tone)}</td>
                      <td class="tnum font-semibold">${v.hits}</td>
                      <td>
                        <div class="flex items-center gap-1.5">
                          ${governedBtn({ label: "Rename", role: "Owner", auditEvent: "saved_view.renamed", onClickJs: "", cls: "so-btn so-btn-sm", iconName: "edit", blockedReason: v.status === "System Locked" ? "Compliance preset is System Locked" : "" })}
                          ${governedBtn({ label: "Share", role: "Owner", auditEvent: "saved_view.shared", onClickJs: "", cls: "so-btn so-btn-sm", iconName: "share" })}
                          ${governedBtn({ label: "Delete", role: "Owner", auditEvent: "saved_view.deleted", onClickJs: `removeSavedView('${v.id}')`, cls: "so-btn so-btn-sm so-btn-outline-danger", iconName: "trash", blockedReason: v.status === "System Locked" ? "System Locked compliance view cannot be deleted" : "" })}
                        </div>
                      </td>
                    </tr>
                  `,
                    )
                    .join("")}
                </tbody>
              </table></div>
            </div>
          `
              : ""
          }

          <div class="so-panel p-5 flex-1 flex flex-col items-center justify-center text-center space-y-2">
            <div class="w-10 h-10 rounded-[6px] bg-[#151d24] border border-[#24303c] flex items-center justify-center text-[#60a5fa]">${ic("inbox")}</div>
            <div class="text-[13.5px] font-semibold text-[#ffffff]">${list.length === 0 ? "No Saved Views Configured" : "No Personal Spoke Watchlists Yet"}</div>
            <div class="text-[11.5px] text-[#8b96a5] max-w-[440px]">Save any filtered query from Workspace Inventory or Environment Health to pin live object counts in your left navigation rail.</div>
            <button data-onclick="switchScreen('s02-workspaces')" class="so-btn so-btn-sm so-btn-emerald mt-1">Create View from Workspace Inventory</button>
          </div>
        </div>
      `;
  }

  // =========================================================================
  // SCREEN 4: SCOPE PLAN (Conduit Control Tower Header + Task Gantt Bars!)
  // =========================================================================
  function renderS04ScopePlan(root) {
    const tasks = [
      {
        num: "01",
        name: "Raise ingress-gateway memory limit 768Mi -> 1Gi",
        est: "15m",
        dep: "—",
        st: "Done",
        tone: "green",
        left: 2,
        w: 16,
        bar: "#32a467",
      },
      {
        num: "02",
        name: "Validate Kubernetes v1.29–v1.31 schemas on CR-308",
        est: "20m",
        dep: "After #01",
        st: "Done",
        tone: "green",
        left: 18,
        w: 18,
        bar: "#32a467",
      },
      {
        num: "03",
        name: "Execute Packaging run RUN-8844 & sign 4 Artifacts",
        est: "25m",
        dep: "After #02",
        st: "In Progress",
        tone: "blue",
        left: 36,
        w: 20,
        bar: "#2d72d2",
      },
      {
        num: "04",
        name: "Verify Wave 1 canary soak on edge-west-01",
        est: "30m",
        dep: "After #03",
        st: "Ready",
        tone: "amber",
        left: 56,
        w: 18,
        bar: "#b78128",
      },
      {
        num: "05",
        name: "Export 41.2 MB zstd delta to spoke-diode-hall-b",
        est: "20m",
        dep: "After #03",
        st: "Ready",
        tone: "amber",
        left: 56,
        w: 14,
        bar: "#b78128",
      },
      {
        num: "06",
        name: "Execute Wave 2 rollout in 02:00–05:00 UTC window",
        est: "45m",
        dep: "After #04, #05",
        st: "Blocked",
        tone: "red",
        left: 74,
        w: 24,
        bar: "#8f3438",
      },
    ];

    root.innerHTML = `
        <div class="space-y-2.5 h-full flex flex-col justify-between">
          <div class="space-y-2.5">
            <!-- Conduit Control Tower Style Header (Matched to f_06_05_ct2.png) -->
            <div class="so-panel p-3.5 flex flex-wrap items-center justify-between gap-3">
              <div class="space-y-1 max-w-[650px]">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="code-id text-[13px] font-semibold text-[#5c9ce6]">STR-214</span>
                  ${pill("Scoping", "blue")}
                  ${ontChip("ConduitChangeRequest", 3, 1, "s10-diff")}
                  ${ontChip("ConduitPlan", 14, 1, "s15-plan-detail")}
                </div>
                <div class="text-[14.5px] font-bold text-[#ffffff]">Eliminate burst-replay OOM restarts &amp; promote signed uds-core 0.31.2 across 14 clusters</div>
              </div>
              <div class="flex items-center gap-2">
                ${kpiBox("2h 35m", "TOTAL EST", "#eab854")}
                ${kpiBox("6", "TASKS", "#60a5fa")}
                ${kpiBox("14", "CLUSTERS", "#34d399")}
              </div>
            </div>

            <div class="so-grid-2 gap-2.5">
              <div class="so-opt-card p-3 space-y-1.5">
                <div class="text-[12px] font-semibold text-[#34d399]">In Scope (4)</div>
                <div class="space-y-1 text-[11.5px] text-[#dce4ee]">
                  <div>✓ Raise <span class="code-id">ingress-gateway</span> memory limit to <span class="code-id">1Gi</span> in CR-308</div>
                  <div>✓ Reconcile <span class="code-id">egress-relay-lock</span> /24 CIDR drift on edge-west-02</div>
                  <div>✓ Build &amp; sign 4 release Artifacts in Packaging run RUN-8844</div>
                  <div>✓ Roll out Plan PLN-2026-0421 across 3 Spoke cohorts</div>
                </div>
              </div>
              <div class="so-danger-card p-3 space-y-1.5">
                <div class="text-[12px] font-semibold text-[#f87171]">Out of Scope (3)</div>
                <div class="space-y-1 text-[11.5px] text-[#9aa5b4]">
                  <div>✕ Underlying Kubernetes control-plane minor upgrade (v1.31)</div>
                  <div>✕ Spoke mTLS root certificate rotation (tracked in STR-219)</div>
                  <div>✕ Modifying local ZFS mirror storage quotas on relays</div>
                </div>
              </div>
            </div>

            <!-- Conduit Gantt + Dependency Task Breakdown -->
            <div class="so-panel overflow-hidden">
              <div class="px-3.5 py-2 border-b border-[#1c242c] flex items-center justify-between">
                <span class="text-[13px] font-semibold text-[#ffffff]">Numbered Task Breakdown &amp; Critical Path</span>
                <span class="text-[11px] text-[#6d7885]">Dependencies verified acyclic</span>
              </div>
              <div class="so-table-scroll"><table class="so-table">
                <thead><tr><th>#</th><th>Task</th><th>Est.</th><th>Dependency</th><th>Schedule Span</th><th>Status</th></tr></thead>
                <tbody>
                  ${tasks
                    .map(
                      (t) => `
                    <tr>
                      <td class="code-id font-semibold text-[#5c9ce6]">${t.num}</td>
                      <td class="font-medium text-[#f0f5fa]">${t.name}</td>
                      <td class="tnum">${t.est}</td>
                      <td class="text-[11px] text-[#8b96a5]">${t.dep}</td>
                      <td class="w-[180px]">
                        <div class="h-3.5 w-full bg-[#090e12] rounded-[3px] relative overflow-hidden border border-[#19222b]">
                          <div class="h-full rounded-[2px]" style="margin-left:${t.left}%;width:${t.w}%;background:${t.bar}"></div>
                        </div>
                      </td>
                      <td>${pill(t.st, t.tone)}</td>
                    </tr>
                  `,
                    )
                    .join("")}
                </tbody>
              </table></div>
            </div>

            <div class="so-panel px-3.5 py-2 flex items-center justify-between gap-3 text-[11.5px]">
              <div class="flex items-center gap-2">${pill("High Risk", "red")} <span class="font-medium text-[#f0f5fa]">2-worker cluster remote-rig-09 RAM headroom</span></div>
              <span class="text-[#8b96a5]">Mitigation: Requirement pre-flight enforces &ge; 2.4 GiB allocatable before wave execution</span>
            </div>
          </div>

          <div class="so-panel px-3.5 py-2.5 flex items-center justify-between shrink-0">
            <span class="text-[11.5px] text-[#8b96a5]">Approving transitions Story <span class="code-id text-[#f0f5fa]">STR-214</span> to Ready and queues execution</span>
            <div class="flex items-center gap-2">
              ${governedBtn({ label: "Request changes", role: "Profile Owner", auditEvent: "story.changes_requested", onClickJs: "switchScreen('s01-intake')", cls: "so-btn so-btn-outline-warn" })}
              ${governedBtn({ label: "Approve Scope", role: "Profile Owner", auditEvent: "story.scope_approved", onClickJs: "switchScreen('s05-backlog')", cls: "so-btn so-btn-emerald", iconName: "check" })}
            </div>
          </div>
        </div>
      `;
  }

  // =========================================================================
  // SCREEN 5: BACKLOG
  // =========================================================================
  function renderS05Backlog(root) {
    const stories = [
      {
        id: "STR-216",
        grp: "Ready",
        title: "Configure canary rollback thresholds on edge-west-01",
        prio: "P1 · High",
        pTone: "amber",
        size: "2 pt",
        plan: "PLN-2026-0421",
        run: "RUN-8844",
        pct: 0,
        desc: "Halts rollout wave if pod restart count >= 2 during 5-minute canary soak.",
      },
      {
        id: "STR-217",
        grp: "Ready",
        title: "Generate courier verification manifest for forward-arctic-01",
        prio: "P2 · Med",
        pTone: "blue",
        size: "1 pt",
        plan: "PLN-2026-0418",
        run: "RUN-8842",
        pct: 0,
        desc: "Packages offline public key and SHA-256 checklist for courier transfer.",
      },
      {
        id: "STR-214",
        grp: "In progress",
        title: "Raise ingress-gateway limit to 1Gi & stage uds-core 0.31.2",
        prio: "P0 · Crit",
        pTone: "red",
        size: "3 pt",
        plan: "PLN-2026-0421",
        run: "RUN-8844",
        pct: 64,
        desc: "Authors CR-308 in ws-telemetry-oom-fix and runs 6-stage build RUN-8844.",
      },
      {
        id: "STR-215",
        grp: "In progress",
        title: "Pack 16 MB zstd delta chunks for spoke-diode-hall-b",
        prio: "P1 · High",
        pTone: "amber",
        size: "2 pt",
        plan: "PLN-2026-0421",
        run: "RUN-8844",
        pct: 45,
        desc: "Slices an offline delta bundle for optical diode transfer window.",
      },
      {
        id: "STR-212",
        grp: "Blocked",
        title: "Execute Wave 2 rollout across 6 edge-west Environments",
        prio: "P0 · Crit",
        pTone: "red",
        size: "5 pt",
        plan: "PLN-2026-0418",
        run: "RUN-8842",
        pct: 80,
        desc: "Blocked awaiting 2nd @fleet-approvers signature and 02:00 UTC maintenance window.",
      },
      {
        id: "STR-208",
        grp: "Done",
        title: "Rotate hardware signing key prod-root-2026 across 6 Spokes",
        prio: "P1 · High",
        pTone: "muted",
        size: "2 pt",
        plan: "PLN-2026-0409",
        run: "RUN-8835",
        pct: 100,
        desc: "Verified offline across all 48 target clusters.",
      },
    ];
    const sel =
      stories.find((s) => s.id === state.selectedStoryId) || stories[2];

    root.innerHTML = `
        <div class="so-resp-stack flex gap-3 h-full min-h-0 items-stretch">
          <div class="flex-1 min-w-0 flex flex-col gap-2.5 overflow-y-auto">
            <div class="flex items-center justify-between">
              <span class="text-[15px] font-bold text-[#ffffff]">Delivery Backlog · wp-edge-telemetry</span>
              <button data-onclick="switchScreen('s06-active-work')" class="so-btn so-btn-sm so-btn-blue">Open Active Work (STR-214) &rarr;</button>
            </div>
            ${[
              ["Ready", "blue"],
              ["In progress", "green"],
              ["Blocked", "red"],
              ["Done", "muted"],
            ]
              .map(([gName, gTone]) => {
                const items = stories.filter((s) => s.grp === gName);
                return `
                <div class="so-panel overflow-hidden shrink-0">
                  <div class="px-3 py-1.5 bg-[#0e1418] border-b border-[#1c242c] flex items-center justify-between">
                    <div class="flex items-center gap-2">${pill(gName, gTone)} <span class="tnum text-[11px] text-[#6d7885]">${items.length}</span></div>
                  </div>
                  <div class="divide-y divide-[#161e25]">
                    ${items
                      .map(
                        (st) => `
                      <div data-onclick="state.selectedStoryId='${st.id}'; renderMainWorkspace()"
                           class="px-3 py-2 flex items-center justify-between gap-2 cursor-pointer ${st.id === sel.id ? "bg-[#152232] border-l-2 border-[#5c9ce6]" : "hover:bg-[#141c24]"}">
                        <div class="flex items-center gap-2.5 min-w-0">
                          <span class="text-[#596573] cursor-grab">${ic("grip")}</span>
                          <span class="code-id text-[11.5px] font-semibold text-[#5c9ce6]">${st.id}</span>
                          ${pill(st.prio, st.pTone)}
                          <span class="text-[12px] font-medium text-[#f0f5fa] truncate">${st.title}</span>
                        </div>
                        <div class="flex items-center gap-2 shrink-0">
                          <span class="tnum text-[11px] px-2 py-0.5 rounded bg-[#0c1116] border border-[#202a34] text-[#8b96a5]">${st.size}</span>
                          ${ontChip(st.plan, 14, 0, "s15-plan-detail")}
                        </div>
                      </div>
                    `,
                      )
                      .join("")}
                  </div>
                </div>
              `;
              })
              .join("")}
          </div>

          <div class="so-resp-full so-panel w-[294px] shrink-0 p-3.5 flex flex-col justify-between">
            <div class="space-y-3">
              <div class="flex items-center justify-between border-b border-[#1c242c] pb-2.5">
                <span class="code-id text-[14px] font-bold text-[#5c9ce6]">${sel.id}</span>
                ${pill(sel.grp, sel.grp === "Blocked" ? "red" : "green")}
              </div>
              <div class="text-[13px] font-semibold text-[#ffffff] leading-snug">${sel.title}</div>
              <div class="text-[11.5px] text-[#8b96a5] leading-relaxed">${sel.desc}</div>

              <div>
                <div class="flex justify-between text-[11px] mb-1"><span class="text-[#8b96a5]">Story Progress</span><span class="tnum font-semibold text-[#34d399]">${sel.pct}%</span></div>
                <div class="h-1.5 w-full bg-[#090e12] rounded-full overflow-hidden"><div class="h-full so-striped-bar" style="width:${sel.pct}%"></div></div>
              </div>

              <div class="space-y-2 pt-2 border-t border-[#1c242c] text-[11.5px]">
                <div class="flex justify-between items-center"><span class="text-[#6d7885]">Size</span><span class="tnum font-medium">${sel.size}</span></div>
                <div class="flex justify-between items-center"><span class="text-[#6d7885]">Packaging run</span>${ontChip(sel.run, 6, 0, "s07-pipeline")}</div>
                <div class="flex justify-between items-center"><span class="text-[#6d7885]">Linked Plan</span>${ontChip(sel.plan, 14, 0, "s15-plan-detail")}</div>
              </div>
            </div>

            <div class="pt-3 border-t border-[#1c242c] flex gap-2">
              ${governedBtn({
                label:
                  sel.grp === "In progress"
                    ? "Open Live Execution"
                    : "Start Story",
                role: "Operator",
                auditEvent: "story.started",
                onClickJs: "switchScreen('s06-active-work')",
                cls: "so-btn so-btn-emerald flex-1",
                iconName: sel.grp === "Blocked" ? "lock" : "play",
                blockedReason:
                  sel.grp === "Blocked"
                    ? `Plan ${sel.plan} has 2 Blocked Requirements`
                    : "",
              })}
            </div>
          </div>
        </div>
      `;
  }

  // =========================================================================
  // SCREEN 6: ACTIVE WORK PROGRESS (Matched 1:1 to f_02_38_cn_run.png!)
  // =========================================================================
  function renderS06ActiveWork(root) {
    const checks = [
      {
        st: "Done",
        tone: "green",
        title: "1. Parse tarball & verify SPDX digests",
        sub: "418 packages verified · 00m 42s",
      },
      {
        st: "Done",
        tone: "green",
        title: "2. Patch values.yaml memory ceiling (1Gi)",
        sub: "Change request CR-308 · +12 -4 lines",
      },
      {
        st: "Done",
        tone: "green",
        title: "3. Reconcile egress-relay-lock /24 CIDR",
        sub: "Environment edge-west-02 · 02m 08s",
      },
      {
        st: "In Progress",
        tone: "blue",
        title: "4. Dry-run schemas across 14 Environments",
        sub: "11 of 14 clusters validated against v1.30.6",
      },
      {
        st: "Pending",
        tone: "muted",
        title: "5. Trigger Packaging run RUN-8844",
        sub: "Queued next",
      },
      {
        st: "Pending",
        tone: "muted",
        title: "6. Propose governed Plan PLN-2026-0421",
        sub: "Awaiting signed artifacts",
      },
    ];

    const stream = [
      {
        ts: "09:54:28 PM",
        tone: "blue",
        title: "Validate cluster API schemas",
        desc: "Server-side dry-run clean on 11/14 target clusters (v1.29–v1.31).",
        a: ["Schema Agent", 14, 0],
        t: ["Kubeconform", 14, 0],
        o: ["ConduitEnvironment", 14, 0, "s14-env-detail"],
      },
      {
        ts: "09:51:40 PM",
        tone: "emerald",
        title: "Evaluate restricted admission policy",
        desc: "Verified runAsNonRoot=true and 1Gi limit within node capacity.",
        a: ["Policy Agent", 5, 1],
        t: ["Admission Check", 5, 0],
        o: ["ConduitRequirement", 5, 1, "s15-plan-detail"],
      },
      {
        ts: "09:48:19 PM",
        tone: "violet",
        title: "Author Change request CR-308",
        desc: "Updated ingress-gateway values.yaml and egress-relay-lock CIDR.",
        a: ["Diff Agent", 3, 2],
        t: ["Git Branch", 3, 1],
        o: ["ConduitChangeRequest", 3, 1, "s10-diff"],
      },
    ];

    root.innerHTML = `
        <div class="space-y-3 h-full flex flex-col">
          <!-- Conduit Change-Notice Processor Header + Striped Progress Track -->
          <div class="so-panel p-3.5 space-y-2.5 shrink-0">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div>
                <div class="flex items-center gap-2">
                  <span class="code-id text-[13px] font-semibold text-[#34d399]">STR-214</span>
                  ${pill(state.agentPaused ? "Paused" : "Working Story", state.agentPaused ? "amber" : "blue")}
                </div>
                <div class="text-[15px] font-bold text-[#ffffff] mt-0.5">Validating Kubernetes schemas across 14 bound Environments (Step 4 of 6)</div>
              </div>
              <div class="flex items-center gap-2">
                ${kpiBox("00:14:28", "ELAPSED", "#eab854")}
                ${kpiBox("11 / 14", "VERIFIED", "#34d399")}
                ${governedBtn({
                  label: state.agentPaused ? "Resume" : "Pause",
                  role: "Operator",
                  auditEvent: state.agentPaused
                    ? "story.resumed"
                    : "story.paused",
                  onClickJs:
                    "state.agentPaused=!state.agentPaused; renderMainWorkspace()",
                  cls: "so-btn so-btn-outline-warn",
                  iconName: state.agentPaused ? "play" : "pause",
                })}
                ${governedBtn({
                  label: "Stop",
                  role: "Operator",
                  auditEvent: "story.stopped",
                  onClickJs: "state.agentPaused=true; renderMainWorkspace()",
                  cls: "so-btn so-btn-outline-danger",
                  iconName: "stop",
                })}
              </div>
            </div>

            <div class="flex items-center gap-3">
              <div class="flex-1 h-2 bg-[#080c10] rounded-full overflow-hidden border border-[#1b242e]">
                <div class="h-full ${state.agentPaused ? "bg-[#eab854]" : "so-striped-bar"}" style="width:64%"></div>
              </div>
              <span class="tnum text-[14px] font-bold text-[#34d399]">64%</span>
            </div>
          </div>

          <div class="so-resp-stack flex-1 min-h-0 flex gap-3 items-stretch">
            <div class="so-resp-full so-panel w-[44%] flex flex-col overflow-hidden">
              <div class="px-3.5 py-2.5 border-b border-[#1c242c] flex items-center justify-between">
                <span class="text-[13px] font-semibold text-[#ffffff]">Task Checklist</span>
                <span class="tnum text-[11px] text-[#34d399]">3 Done · 1 Active · 2 Pending</span>
              </div>
              <div class="p-3 space-y-2 overflow-y-auto flex-1">
                ${checks
                  .map(
                    (c) => `
                  <div class="${c.st === "In Progress" ? "so-opt-card" : "so-card"} p-2.5 flex items-center justify-between gap-2">
                    <div>
                      <div class="font-medium text-[12px] text-[#f0f5fa]">${c.title}</div>
                      <div class="text-[10.5px] text-[#6d7885] mt-0.5">${c.sub}</div>
                    </div>
                    ${pill(c.st, c.tone)}
                  </div>
                `,
                  )
                  .join("")}
              </div>
            </div>

            <div class="so-panel flex-1 min-w-0 flex flex-col overflow-hidden">
              <div class="px-3.5 py-2.5 border-b border-[#1c242c] flex items-center justify-between">
                <span class="text-[13px] font-semibold text-[#ffffff]">Live Activity Feed</span>
                <button data-onclick="switchScreen('s10-diff')" class="so-btn so-btn-sm">Inspect CR-308 Diff &rarr;</button>
              </div>
              <div class="p-3 space-y-3 overflow-y-auto flex-1">
                ${stream
                  .map(
                    (s) => `
                  <div class="so-card p-3 so-stream-item so-stream-${s.tone} space-y-1.5">
                    <div class="flex items-center justify-between">
                      <span class="font-semibold text-[12px] text-[#ffffff]">${s.title}</span>
                      <span class="tnum text-[10.5px] text-[#596573]">${s.ts}</span>
                    </div>
                    <div class="text-[11.5px] text-[#9aa5b4]">${s.desc}</div>
                    <div class="flex flex-wrap gap-1.5 pt-0.5">
                      ${agentChip(s.a[0], s.a[1], s.a[2])}
                      ${toolChip(s.t[0], s.t[1], s.t[2])}
                      ${ontChip(s.o[0], s.o[1], s.o[2], s.o[3])}
                    </div>
                  </div>
                `,
                  )
                  .join("")}
              </div>
            </div>
          </div>
        </div>
      `;
  }

  // =========================================================================
  // SCREEN 7: PIPELINE (Left-to-Right 6-Node DAG + Timestamped Log Viewer)
  // =========================================================================
  function renderS07Pipeline(root) {
    const nodes = [
      {
        id: "fetch",
        name: "fetch",
        st: "Succeeded",
        tone: "green",
        dur: "00m 41s",
        lines: [
          "21:32:04.102Z  Resolving wp-edge-telemetry lockfile (14 OCI images, 6 charts)",
          "21:32:45.009Z  Verified upstream digests; 11/14 cache hits -> exit 0",
        ],
      },
      {
        id: "scan",
        name: "scan",
        st: "Succeeded",
        tone: "green",
        dur: "01m 18s",
        lines: [
          "21:32:46.110Z  Cataloged 418 packages into sbom-uds-core-0.31.2.spdx.json",
          "21:34:04.315Z  Policy baseline-restricted-v3 PASS (0 Critical, 0 High)",
        ],
      },
      {
        id: "sign",
        name: "sign",
        st: "Succeeded",
        tone: "green",
        dur: "00m 22s",
        lines: [
          "21:34:05.018Z  Attaching hardware signature hsm://slot-02/prod-root-2026",
          "21:34:26.774Z  Signed 4 release Artifacts + SLSA provenance",
        ],
      },
      {
        id: "package",
        name: "package",
        st: "Succeeded",
        tone: "green",
        dur: "01m 39s",
        lines: [
          "21:34:28.119Z  Packing differential zstd archive against 0.31.1 (41.2 MB delta)",
        ],
      },
      {
        id: "publish",
        name: "publish",
        st: "Running",
        tone: "blue",
        dur: "00m 48s",
        lines: [
          "21:36:08.004Z  Pushing signed OCI bundle to internal hub registry.conduit.internal",
          "21:36:21.318Z  Uploaded chunk 6/8 (31.4 / 41.2 MB) to spoke-west-relay store-and-forward",
          "21:36:34.990Z  Staging optical diode export spool on spoke-diode-hall-b",
          "21:36:55.401Z  Awaiting chunk 8/8 fsync receipt from spoke-diode-hall-b...",
        ],
      },
      {
        id: "propose",
        name: "propose plan",
        st: "Pending",
        tone: "muted",
        dur: "—",
        lines: [
          "21:36:55.402Z  Queued: waiting for publish completion to open Plan PLN-2026-0421",
        ],
      },
    ];
    const sel = nodes.find((n) => n.id === state.selectedPipeNode) || nodes[4];
    const q = state.pipeLogQuery.toLowerCase();
    const lines = sel.lines.filter((l) => !q || l.toLowerCase().includes(q));

    root.innerHTML = `
        <div class="space-y-3 h-full flex flex-col">
          <div class="so-panel p-3.5 space-y-2.5 shrink-0">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <div class="flex items-center gap-2.5">
                <span class="text-[#60a5fa] flex">${ic("cube")}</span>
                <span class="text-[15px] font-bold text-[#ffffff]">Packaging Pipeline · RUN-8844</span>
                ${pill("Running · Stage 5 of 6", "blue")}
                ${ontChip("ConduitArtifact", 4, 4, "s08-staged-run")}
              </div>
              <div class="flex items-center gap-2">
                ${governedBtn({ label: "Rerun from stage", role: "Operator", auditEvent: "packaging_run.rerun_from_step", onClickJs: "", cls: "so-btn so-btn-sm", iconName: "lock", blockedReason: "Packaging run RUN-8844 is currently Running" })}
                <button data-onclick="switchScreen('s08-staged-run')" class="so-btn so-btn-sm so-btn-emerald">Open Staged Run (RUN-8842) &rarr;</button>
              </div>
            </div>

            <div class="flex items-center gap-3">
              <div class="flex-1 h-2 bg-[#080c10] rounded-full overflow-hidden"><div class="h-full so-striped-bar" style="width:78%"></div></div>
              <span class="tnum text-[13px] font-bold text-[#34d399]">78%</span>
            </div>

            <!-- Left-to-Right 6-Node Graph -->
            <div class="so-grid-6 gap-2 pt-1">
              ${nodes
                .map(
                  (n, i) => `
                <div class="flex items-center">
                  <button data-onclick="state.selectedPipeNode='${n.id}'; renderMainWorkspace()"
                    class="w-full p-2.5 text-left ${n.tone === "green" ? "so-opt-card" : "so-card"} ${n.id === sel.id ? "!border-[#5c9ce6] ring-1 ring-[#5c9ce6]/50" : ""}">
                    <div class="flex items-center justify-between">
                      <span class="font-semibold text-[12px] text-[#ffffff] truncate">${n.name}</span>
                      <span class="tnum text-[10px] text-[#8b96a5]">${n.dur}</span>
                    </div>
                    <div class="mt-1.5">${pill(n.st, n.tone)}</div>
                  </button>
                  ${i < 5 ? '<span class="text-[#596573] pl-1 text-[12px]">&rarr;</span>' : ""}
                </div>
              `,
                )
                .join("")}
            </div>
          </div>

          <div class="so-panel flex-1 min-h-0 flex flex-col overflow-hidden">
            <div class="px-3.5 py-2 border-b border-[#1c242c] flex flex-wrap items-center justify-between gap-2 bg-[#0e1418]">
              <div class="flex items-center gap-2">
                <span class="text-[12.5px] font-semibold text-[#ffffff]">Job Output · ${sel.name}</span>
                ${pill(sel.st, sel.tone)}
              </div>
              <div class="flex items-center gap-2">
                <input type="text" value="${state.pipeLogQuery}" data-oninput="state.pipeLogQuery=this.value; renderMainWorkspace()" placeholder="Search log lines..." class="so-input !h-[23px] w-[165px]" />
                <button data-onclick="state.pipeFollowLogs=!state.pipeFollowLogs; renderMainWorkspace()" class="so-btn so-btn-sm ${state.pipeFollowLogs ? "so-btn-blue" : ""}">Follow: ${state.pipeFollowLogs ? "On" : "Off"}</button>
                ${governedBtn({ label: "Download", role: "Operator", auditEvent: "step.log_downloaded", onClickJs: "", cls: "so-btn so-btn-sm", iconName: "download" })}
              </div>
            </div>
            <div class="flex-1 min-h-0 overflow-y-auto p-3 bg-[#080c0f] code-id text-[11px] leading-[1.7]">
              ${lines
                .map(
                  (ln, idx) => `
                <div class="flex gap-3 px-1.5 rounded hover:bg-[#121920]">
                  <span class="w-5 text-right text-[#596573] select-none">${idx + 1}</span>
                  <span class="${ln.includes("PASS") || ln.includes("Signed") ? "text-[#34d399]" : ln.includes("Awaiting") ? "text-[#60a5fa]" : "text-[#dce4ee]"}">${ln}</span>
                </div>
              `,
                )
                .join("")}
            </div>
          </div>
        </div>
      `;
  }

  // =========================================================================
  // SCREEN 8: STAGED RUN PAGE (Stage Tracker + Output + Artifacts + Confirm + Conduit Audit Rail)
  // =========================================================================
  function renderS08StagedRun(root) {
    liveHub.renderPackaging(root);
    return;
    const arts = [
      [
        "uds-core-amd64-0.31.2.tar.zst",
        "Bundle Archive",
        "488.6 MB",
        "sha256:9c71fe08...2d44",
        "Signed",
      ],
      [
        "uds-core-0.31.2-delta.tar.zst",
        "Diode Delta",
        "63.8 MB",
        "sha256:3b19a441...70e1",
        "Published",
      ],
      [
        "sbom-uds-core-0.31.2.spdx.json",
        "SPDX SBOM",
        "1.84 MB",
        "sha256:77e01c12...99a4",
        "Signed",
      ],
      [
        "provenance-slsa-v1.intoto.jsonl",
        "Attestation",
        "48 KB",
        "sha256:14f09d88...3c10",
        "Signed",
      ],
    ];
    const audits = [
      {
        who: "J. Alvarez",
        when: "09:53 PM",
        what: "Requested rerun from package stage",
        why: "Re-slice 16 MB diode chunks for Hall-B.",
        o: ["ConduitArtifact", 4, 2],
      },
      {
        who: "Release Runner",
        when: "09:36 PM",
        what: "Proposed Plan PLN-2026-0418",
        why: "All 6 build & signature stages succeeded.",
        o: ["ConduitPlan", 14, 1],
      },
      {
        who: "HSM Slot-02",
        when: "09:34 PM",
        what: "Signed 4 release artifacts",
        why: "Zero high/critical CVEs in SPDX scan.",
        o: ["ConduitStep", 6, 0],
      },
    ];

    root.innerHTML = `
        <div class="space-y-2.5 h-full flex flex-col">
          <div class="so-panel p-3 flex flex-wrap items-center justify-between gap-3 shrink-0">
            <div class="flex items-center gap-3">
              ${pill("Staged · 6/6 Complete", "green")}
              <div>
                <div class="text-[14.5px] font-bold text-[#ffffff]">RUN-8842 · uds-core 0.31.2</div>
                <div class="text-[11px] text-[#8b96a5]">Proposed <button data-onclick="switchScreen('s15-plan-detail')" class="text-[#5c9ce6] underline">Plan PLN-2026-0418</button> · Duration 04m 12s</div>
              </div>
            </div>
            <div class="flex items-center gap-2">
              <button data-onclick="state.confirmActionType='rerun'; state.showRerunConfirm=true; renderMainWorkspace()" class="so-btn so-btn-sm">${ic("refresh")} <span>Rerun from stage...</span></button>
              <button data-onclick="state.confirmActionType='cancel'; state.showRerunConfirm=true; renderMainWorkspace()" class="so-btn so-btn-sm so-btn-outline-danger">${ic("cross")} <span>Cancel</span></button>
            </div>
          </div>

          <div class="so-grid-6 gap-2 shrink-0">
            ${[
              "1. fetch",
              "2. scan",
              "3. sign",
              "4. package",
              "5. publish",
              "6. propose plan",
            ]
              .map(
                (s) => `
              <div class="so-opt-card px-2.5 py-1.5 flex items-center justify-between">
                <span class="font-medium text-[11.5px] text-[#ffffff] truncate">${s}</span>
                <span class="text-[#34d399]">${ic("check")}</span>
              </div>
            `,
              )
              .join("")}
          </div>

          <div class="so-resp-stack flex-1 min-h-0 flex gap-3 items-stretch">
            <div class="flex-1 min-w-0 flex flex-col gap-2.5 min-h-0">
              <div class="so-panel overflow-hidden shrink-0">
                <div class="px-3 py-1.5 bg-[#0e1418] border-b border-[#1c242c] flex justify-between text-[11.5px] font-semibold">
                  <span>Stage Output</span><span class="code-id text-[#34d399]">exit 0</span>
                </div>
                <pre class="m-0 p-2.5 bg-[#080c0f] code-id text-[10.5px] text-[#9aa5b4] leading-relaxed">[21:34:28Z] Assembled deterministic zstd delta (63.8 MB) against base 0.31.1
[21:36:19Z] Published 4 signed artifacts and proposed Plan PLN-2026-0418</pre>
              </div>

              ${
                state.showRerunConfirm
                  ? `
                <div class="${state.confirmActionType === "cancel" ? "so-danger-card" : "so-warn-card"} p-3 flex items-center justify-between gap-3 shrink-0">
                  <div>
                    <div class="font-semibold text-[12px] text-[#ffffff]">${state.confirmActionType === "cancel" ? "Confirm Cancel Run RUN-8842?" : "Confirm Rerun from Stage 4 (package)?"}</div>
                    <div class="text-[11px] text-[#9aa5b4]">Re-runs package, publish, and propose plan using pinned scan digest and logs an Audit event.</div>
                  </div>
                  <div class="flex gap-1.5 shrink-0">
                    <button data-onclick="state.showRerunConfirm=false; renderMainWorkspace()" class="so-btn so-btn-sm">Dismiss</button>
                    ${governedBtn({ label: "Confirm", role: "Operator", auditEvent: "packaging_run.rerun_from_step", onClickJs: "state.showRerunConfirm=false; renderMainWorkspace()", cls: "so-btn so-btn-sm so-btn-blue" })}
                  </div>
                </div>
              `
                  : ""
              }

              <div class="so-panel overflow-hidden flex-1">
                <div class="px-3 py-2 bg-[#0e1418] border-b border-[#1c242c] text-[12.5px] font-semibold text-[#ffffff]">Signed Artifacts (4)</div>
                <div class="so-table-scroll"><table class="so-table">
                  <thead><tr><th>Artifact Name</th><th>Type</th><th>Size</th><th>Digest</th><th>Signed</th><th>Download</th></tr></thead>
                  <tbody>
                    ${arts
                      .map(
                        (a) => `
                      <tr>
                        <td class="code-id text-[#5c9ce6] font-medium">${a[0]}</td>
                        <td>${a[1]}</td>
                        <td class="tnum">${a[2]}</td>
                        <td class="code-id text-[10.5px] text-[#8b96a5]">${a[3]}</td>
                        <td>${pill(a[4], "green")}</td>
                        <td>${governedBtn({ label: "Download", role: "Operator", auditEvent: "artifact.downloaded", onClickJs: "", cls: "so-btn so-btn-sm", iconName: "download" })}</td>
                      </tr>
                    `,
                      )
                      .join("")}
                  </tbody>
                </table></div>
              </div>
            </div>

            <!-- Right Audit Rail (Matched to f_02_38_cn_run.png Right Column) -->
            <div class="so-resp-full so-panel w-[264px] shrink-0 flex flex-col overflow-hidden">
              <div class="px-3 py-2 bg-[#0e1418] border-b border-[#1c242c] flex items-center justify-between">
                <span class="flex items-center gap-1.5 text-[12.5px] font-semibold text-[#34d399]">${ic("audit")} <span>Audit Log</span></span>
                <span class="tnum text-[11px] px-1.5 rounded bg-[#10221f] text-[#34d399]">3</span>
              </div>
              <div class="p-3 space-y-3 overflow-y-auto flex-1">
                ${audits
                  .map(
                    (a) => `
                  <div class="so-stream-item so-stream-emerald space-y-1 pb-2.5 border-b border-[#171f26]">
                    <div class="flex justify-between text-[11px]"><span class="font-semibold text-[#ffffff]">${a.who}</span><span class="tnum text-[#596573]">${a.when}</span></div>
                    <div class="text-[11.5px] text-[#dce4ee]">${a.what}</div>
                    <div class="text-[10.5px] text-[#8b96a5]">Why: ${a.why}</div>
                    <div class="pt-0.5">${ontChip(a.o[0], a.o[1], a.o[2])}</div>
                  </div>
                `,
                  )
                  .join("")}
              </div>
            </div>
          </div>
        </div>
      `;
  }

  // =========================================================================
  // SCREEN 9: SOURCE
  // =========================================================================
  function renderS09Source(root) {
    mountDevWorkspaces(root, { view: "source" });
  }

  // =========================================================================
  // SCREEN 10: DIFF
  // =========================================================================
  function renderS10Diff(root) {
    mountDevWorkspaces(root, { view: "diff" });
  }

  // =========================================================================
  // SCREEN 11: PREVIEW AND RUNTIME
  // =========================================================================
  function renderS11Preview(root) {
    mountDevWorkspaces(root, { view: "runtime" });
  }

  // =========================================================================
  // SCREEN 12: LIFECYCLE ACTIONS
  // =========================================================================
  function renderS12Lifecycle(root) {
    mountDevWorkspaces(root, { view: "lifecycle" });
  }

  // =========================================================================
  // SCREEN 14: ENVIRONMENT DETAIL
  // =========================================================================
  function renderS14EnvDetail(root) {
    liveHub.renderEnvironments(root);
    return;
    const tabs = [
      "Overview",
      "Installations",
      "Reported state",
      "Plans",
      "Audit events",
    ];
    root.innerHTML = `
        <div class="space-y-3 h-full flex flex-col">
          <div class="so-panel p-3.5 flex flex-wrap items-center justify-between gap-2.5 shrink-0">
            <div>
              <div class="flex items-center gap-2.5">
                <span class="code-id text-[15px] font-bold text-[#5c9ce6]">edge-west-02</span>
                ${pill("Drift Detected", "amber")}
                ${ontChip("ConduitSpoke", 1, 0, "s17-spokes-audit")}
              </div>
              <div class="text-[11.5px] text-[#8b96a5] mt-0.5">Kubernetes v1.30.6 · Spoke spoke-west-relay · Last heartbeat 14m ago</div>
            </div>
            <div class="flex items-center gap-2">
              ${kpiBox("3", "DRIFTS", "#eab854")}
              ${kpiBox("99.9%", "UPTIME", "#34d399")}
              ${governedBtn({ label: "Reconcile Drift", role: "Operator", auditEvent: "environment.reconcile_triggered", onClickJs: "", cls: "so-btn so-btn-blue", iconName: "lock", blockedReason: "Plan PLN-2026-0418 is Awaiting Approval" })}
            </div>
          </div>

          <div class="border-b border-[#1c242c] flex gap-2 shrink-0">
            ${tabs.map((t) => `<button data-onclick="state.envTab='${t}'; renderMainWorkspace()" class="px-3 py-1.5 text-[12px] font-medium border-b-2 -mb-[1px] ${state.envTab === t ? "border-[#5c9ce6] text-[#ffffff]" : "border-transparent text-[#8b96a5]"}">${t}</button>`).join("")}
          </div>

          <div class="so-grid-2 gap-3 shrink-0">
            <div class="so-panel p-3 space-y-1.5 text-[11.5px]">
              <div class="font-semibold text-[12.5px] text-[#ffffff] pb-1 border-b border-[#1c242c]">Cluster &amp; Spoke Properties</div>
              <div class="flex justify-between"><span class="text-[#6d7885]">Spoke Transport</span><span>spoke-west-relay (Store-and-Forward)</span></div>
              <div class="flex justify-between"><span class="text-[#6d7885]">Nodes</span><span class="tnum">3 control-plane · 5 worker</span></div>
              <div class="flex justify-between"><span class="text-[#6d7885]">Maintenance Window</span><span class="text-[#eab854] tnum">02:00–05:00 UTC (Closed)</span></div>
            </div>
            <div class="so-panel p-3 space-y-1.5 text-[11.5px]">
              <div class="font-semibold text-[12.5px] text-[#ffffff] pb-1 border-b border-[#1c242c]">Reported State &amp; Bundle</div>
              <div class="flex justify-between"><span class="text-[#6d7885]">Installed Bundle</span><span class="code-id">uds-core 0.31.1</span></div>
              <div class="flex justify-between"><span class="text-[#6d7885]">Desired Staged</span><span class="code-id text-[#5c9ce6]">uds-core 0.31.2 (PLN-2026-0418)</span></div>
              <div class="flex justify-between"><span class="text-[#6d7885]">Agent Signature</span><span class="text-[#34d399]">Verified (ed25519)</span></div>
            </div>
          </div>

          <div class="so-panel overflow-hidden flex-1 flex flex-col">
            <div class="px-3.5 py-2 bg-[#0e1418] border-b border-[#1c242c] flex justify-between items-center">
              <span class="text-[12.5px] font-semibold text-[#ffffff]">Desired vs. Reported State Diff</span>
              ${ontChip("ConduitReportedState", 3, 0)}
            </div>
            <div class="p-3 bg-[#080c0f] flex-1 overflow-y-auto code-id text-[11px] space-y-2">
              <div class="p-2 rounded bg-[#2a1216] text-[#f87171]">- Deployment/ingress-gateway: resources.limits.memory = "768Mi"  (Reported)</div>
              <div class="p-2 rounded bg-[#0f261d] text-[#34d399]">+ Deployment/ingress-gateway: resources.limits.memory = "1Gi"    (Desired · CR-308)</div>
              <div class="p-2 rounded bg-[#2a1216] text-[#f87171]">- NetworkPolicy/egress-relay-lock: cidr = "10.240.0.0/16"</div>
              <div class="p-2 rounded bg-[#0f261d] text-[#34d399]">+ NetworkPolicy/egress-relay-lock: cidr = "10.240.12.0/24"</div>
            </div>
          </div>
        </div>
      `;
  }

  // =========================================================================
  // SCREEN 15: PLAN CONTROL TOWER (Matched 1:1 to Conduit f_06_05_ct2.png!)
  // =========================================================================
  function renderS15PlanDetail(root) {
    const ok = state.planSimQuorum && state.planSimWindow;
    const reqs = [
      {
        name: "Validation passed",
        st: "Pass",
        tone: "green",
        desc: "14/14 cluster dry-runs clean",
      },
      {
        name: "Signatures verified",
        st: "Pass",
        tone: "green",
        desc: "Signed with prod-root-2026",
      },
      {
        name: `Required approvals (${state.planSimQuorum ? "2/2" : "1/2"})`,
        st: state.planSimQuorum ? "Pass" : "Blocked",
        tone: state.planSimQuorum ? "green" : "red",
        desc: state.planSimQuorum
          ? "2/2 @fleet-approvers signed"
          : "Needs 1 more @fleet-approvers signer",
        tog: "state.planSimQuorum=!state.planSimQuorum",
      },
      {
        name: "No rejections",
        st: "Pass",
        tone: "green",
        desc: "0 active holds on revision r3",
      },
      {
        name: "Maintenance window open",
        st: state.planSimWindow ? "Pass" : "Blocked",
        tone: state.planSimWindow ? "green" : "amber",
        desc: state.planSimWindow
          ? "Window override active"
          : "Opens 02:00 UTC (in 4h 06m)",
        tog: "state.planSimWindow=!state.planSimWindow",
      },
    ];

    root.innerHTML = `
        <div class="space-y-3 h-full flex flex-col">
          <!-- Control Tower Banner Matched to f_06_05_ct2.png -->
          <div class="so-panel p-3.5 flex flex-wrap items-center justify-between gap-3 shrink-0">
            <div>
              <div class="flex items-center gap-2.5">
                <span class="text-[15.5px] font-bold text-[#ffffff]">Release Plan Control Tower · PLN-2026-0418</span>
                ${pill(ok ? "Approved" : "Awaiting Approval", ok ? "green" : "amber")}
              </div>
              <div class="text-[11.5px] text-[#6d7885] mt-0.5">uds-core 0.31.2 + CR-308 across 14 Environments. Final gate review pending.</div>
            </div>
            <div class="flex flex-wrap items-center gap-2.5">
              ${agentChip("Gate Engine", 5, 1)}
              ${ontChip("ConduitPlan", 14, 1)}
              <div class="flex items-center gap-2 pl-2 border-l border-[#1f2933] tnum">
                <div><div class="text-[15px] font-bold text-[#e76a6e]">3 Drifts</div><div class="text-[8.5px] text-[#6d7885]">BASELINE</div></div>
                <span class="text-[#596573]">&rarr;</span>
                <div><div class="text-[15px] font-bold text-[#34d399]">0 Drifts</div><div class="text-[8.5px] text-[#6d7885]">POST-WAVE</div></div>
              </div>
            </div>
          </div>

          <!-- 3-Column Control Tower Body Matched to f_06_05_ct2.png -->
          <div class="so-tri-layout so-resp-stack flex-1 min-h-0 flex gap-3 items-stretch">
            <!-- Left: Requirements Checklist -->
            <div class="so-resp-full so-panel w-[262px] shrink-0 flex flex-col overflow-hidden">
              <div class="px-3 py-2 border-b border-[#1c242c] flex justify-between items-center">
                <span class="text-[12.5px] font-semibold text-[#ffffff]">Requirements Gate</span>
                <span class="tnum text-[11px] text-[#9881f3]">${ok ? "5/5" : "3/5"}</span>
              </div>
              <div class="p-2.5 space-y-2 overflow-y-auto flex-1">
                ${reqs
                  .map(
                    (r) => `
                  <div class="so-card p-2.5 space-y-1">
                    <div class="flex items-center justify-between"><span class="font-medium text-[11.5px] text-[#f0f5fa]">${r.name}</span>${pill(r.st, r.tone)}</div>
                    <div class="text-[10.5px] text-[#8b96a5]">${r.desc}</div>
                    ${r.tog ? `<button data-onclick="${r.tog}; renderMainWorkspace()" class="text-[10px] text-[#5c9ce6] hover:underline">Simulate unblock</button>` : ""}
                  </div>
                `,
                  )
                  .join("")}
              </div>
            </div>

            <!-- Center: Wave Rollout Gantt Timeline -->
            <div class="so-panel flex-1 min-w-0 p-3 flex flex-col justify-between overflow-y-auto">
              <div class="so-grid-3 gap-2">
                <div class="so-card p-2.5"><div class="text-[9.5px] uppercase text-[#6d7885]">CANARY IMPACT</div><div class="text-[14px] font-bold text-[#34d399] tnum mt-1">2 Clusters · 0 Drop</div></div>
                <div class="so-card p-2.5"><div class="text-[9.5px] uppercase text-[#6d7885]">REGIONAL EDGE</div><div class="text-[14px] font-bold text-[#60a5fa] tnum mt-1">6 Clusters · 02:00Z</div></div>
                <div class="so-card p-2.5"><div class="text-[9.5px] uppercase text-[#6d7885]">DIODE / COURIER</div><div class="text-[14px] font-bold text-[#eab854] tnum mt-1">6 Clusters · 63.8MB</div></div>
              </div>

              <div class="space-y-2.5 my-2">
                <div class="text-[12.5px] font-semibold text-[#ffffff]">Target Environments Rollout Waves (14 Clusters)</div>
                ${[
                  [
                    "Wave 1 · Canary (edge-west-01, south-06)",
                    8,
                    24,
                    "#32a467",
                    "15m soak",
                  ],
                  [
                    "Wave 2 · Regional (edge-west-02..07)",
                    32,
                    34,
                    "#b78128",
                    "02:00Z window",
                  ],
                  [
                    "Wave 3 · Optical Diode (offline-vault-04 +3)",
                    50,
                    30,
                    "#2d72d2",
                    "Diode spool",
                  ],
                  [
                    "Wave 3B · Courier (forward-arctic-01 +1)",
                    68,
                    26,
                    "#6d5acd",
                    "Signed media",
                  ],
                ]
                  .map(
                    (w) => `
                  <div data-onclick="switchScreen('s14-env-detail')" class="space-y-1 cursor-pointer">
                    <div class="flex justify-between text-[11px]"><span class="text-[#dce4ee]">${w[0]}</span><span class="tnum text-[#8b96a5]">${w[4]}</span></div>
                    <div class="h-4 w-full bg-[#090e12] rounded border border-[#19222b] relative overflow-hidden">
                      <div class="h-full rounded-[2px] flex items-center justify-center text-[9.5px] font-semibold text-white" style="margin-left:${w[1]}%;width:${w[2]}%;background:${w[3]}">${w[4]}</div>
                    </div>
                  </div>
                `,
                  )
                  .join("")}
              </div>

              <div class="so-card overflow-hidden mt-2">
                <div class="px-3 py-1.5 bg-[#0e1418] border-b border-[#1c242c] flex items-center justify-between">
                  <span class="text-[11.5px] font-semibold text-[#ffffff]">Cohort Spoke Dispatch Queue</span>
                  <span class="tnum text-[10.5px] text-[#8b96a5]">14 Environments · 3 Spokes</span>
                </div>
                <div class="so-table-scroll"><table class="so-table">
                  <thead><tr><th>Cohort</th><th>Spoke</th><th>Clusters</th><th>Transfer</th><th>Status</th></tr></thead>
                  <tbody>
                    <tr data-onclick="switchScreen('s14-env-detail')" class="cursor-pointer"><td class="font-medium text-[#f0f5fa]">Wave 1 Canary</td><td class="code-id text-[#8b96a5]">spoke-west-relay</td><td class="tnum">2</td><td class="code-id text-[#8b96a5]">mTLS stream</td><td>${pill("Ready", "green")}</td></tr>
                    <tr data-onclick="switchScreen('s14-env-detail')" class="cursor-pointer"><td class="font-medium text-[#f0f5fa]">Wave 2 Regional</td><td class="code-id text-[#8b96a5]">spoke-west-relay</td><td class="tnum">6</td><td class="code-id text-[#8b96a5]">02:00Z hold</td><td>${pill("Window Hold", "amber")}</td></tr>
                    <tr data-onclick="switchScreen('s08-staged-run')" class="cursor-pointer"><td class="font-medium text-[#f0f5fa]">Wave 3 Diode</td><td class="code-id text-[#8b96a5]">spoke-diode-hall-b</td><td class="tnum">4</td><td class="code-id text-[#8b96a5]">63.8 MB zstd</td><td>${pill("Staged", "blue")}</td></tr>
                    <tr data-onclick="switchScreen('s17-spokes-audit')" class="cursor-pointer"><td class="font-medium text-[#f0f5fa]">Wave 3B Courier</td><td class="code-id text-[#8b96a5]">spoke-courier-vault</td><td class="tnum">2</td><td class="code-id text-[#8b96a5]">Media manifest</td><td>${pill("Queued", "purple")}</td></tr>
                  </tbody>
                </table></div>
              </div>
            </div>

            <!-- Right: Downstream Approval Actions -->
            <div class="so-resp-full so-panel w-[248px] shrink-0 p-3 flex flex-col justify-between">
              <div class="space-y-2.5">
                <div class="text-[12.5px] font-semibold text-[#ffffff]">Downstream Actions</div>
                <div class="so-card p-2.5 space-y-1.5">
                  <div class="text-[11.5px] font-medium text-[#f0f5fa]">1. Co-sign @fleet-approvers quorum</div>
                  <div class="text-[10.5px] text-[#8b96a5]">M. Chen signed; 1 reviewer remaining</div>
                </div>
                <div class="so-card p-2.5 space-y-1.5">
                  <div class="text-[11.5px] font-medium text-[#f0f5fa]">2. Queue for 02:00 UTC window</div>
                  <div class="text-[10.5px] text-[#8b96a5]">Auto-dispatches to spoke-west-relay</div>
                </div>
              </div>
              <div class="space-y-2 pt-3 border-t border-[#1c242c]">
                ${governedBtn({ label: "Approve Plan", role: "Release Approver", auditEvent: "plan.approved", onClickJs: "", cls: "so-btn so-btn-emerald w-full", iconName: ok ? "check" : "lock", blockedReason: ok ? "" : "Blocked while 2 Requirements are unmet" })}
                ${governedBtn({ label: "Reject Plan", role: "Release Approver", auditEvent: "plan.rejected", onClickJs: "", cls: "so-btn so-btn-outline-danger w-full", iconName: "cross" })}
              </div>
            </div>
          </div>
        </div>
      `;
  }

  // =========================================================================
  // SCREEN 16: APPROVALS BOARD (Matched 1:1 to Conduit Kanban f_04_08_comms_kan.png!)
  // =========================================================================
  function renderS16ApprovalsBoard(root) {
    const cols = [
      { k: "Proposed", icon: "inbox", col: "#8b96a5" },
      { k: "Needs approval", icon: "warnTri", col: "#eab854" },
      { k: "Approved", icon: "shield", col: "#60a5fa" },
      { k: "Rolling out", icon: "bolt", col: "#9881f3" },
      { k: "Done", icon: "checkCircle", col: "#34d399" },
    ];
    const cards = [
      {
        id: "PLN-2026-0421",
        col: "Proposed",
        name: "uds-core 0.31.2 + CR-308",
        envs: 14,
        gate: "CAUTION",
        tone: "amber",
        why: "RUN-8844 publishing",
        age: "09:45 PM",
        pct: 0,
      },
      {
        id: "PLN-2026-0418",
        col: "Needs approval",
        name: "uds-core 0.31.2 West",
        envs: 14,
        gate: "NO-GO",
        tone: "red",
        why: "1/2 approvals · window closed",
        age: "09:36 PM",
        pct: 0,
      },
      {
        id: "PLN-2026-0417",
        col: "Needs approval",
        name: "policy-admission 3.2.7",
        envs: 31,
        gate: "CAUTION",
        tone: "amber",
        why: "Awaiting 2nd signer",
        age: "08:50 PM",
        pct: 0,
      },
      {
        id: "PLN-2026-0414",
        col: "Approved",
        name: "core-dns 1.11.3",
        envs: 16,
        gate: "GO",
        tone: "green",
        why: "Queued for 02:00 UTC",
        age: "05:04 PM",
        pct: 0,
      },
      {
        id: "PLN-2026-0412",
        col: "Rolling out",
        name: "runtime-patch v1.30.6",
        envs: 19,
        gate: "CAUTION",
        tone: "amber",
        why: "14/19 clusters converged",
        age: "11:20 AM",
        pct: 74,
      },
      {
        id: "PLN-2026-0411",
        col: "Rolling out",
        name: "log-shipper 0.41.1",
        envs: 24,
        gate: "GO",
        tone: "green",
        why: "22/24 healthy · 2 diode",
        age: "08:15 AM",
        pct: 91,
      },
      {
        id: "PLN-2026-0409",
        col: "Done",
        name: "uds-core 0.31.1",
        envs: 42,
        gate: "GO",
        tone: "green",
        why: "42/42 synced · 0 rollbacks",
        age: "2d ago",
        pct: 100,
      },
    ];
    const sel =
      cards.find((c) => c.id === state.selectedKanbanPlanId) || cards[1];

    root.innerHTML = `
        <div class="so-resp-stack flex gap-3 h-full min-h-0 items-stretch">
          <div class="flex-1 min-w-0 so-grid-5 gap-2 min-h-0">
            ${cols
              .map((c) => {
                const items = cards.filter((x) => x.col === c.k);
                return `
                <div class="so-panel flex flex-col min-h-0 overflow-hidden bg-[#0e1317]">
                  <div class="px-2.5 py-2 border-b border-[#1c242c] flex items-center justify-between">
                    <span class="text-[10px] font-semibold uppercase tracking-[0.08em]" style="color:${c.col}">${c.k}</span>
                    <span class="tnum text-[10px] px-1.5 rounded-full bg-[#172028] text-[#8b96a5]">${items.length}</span>
                  </div>
                  <div class="p-2 space-y-2 overflow-y-auto flex-1">
                    ${items
                      .map(
                        (cd) => `
                      <div data-onclick="state.selectedKanbanPlanId='${cd.id}'; renderMainWorkspace()"
                           class="so-card so-card-interactive p-2.5 cursor-pointer space-y-1.5 ${cd.id === sel.id ? "!border-[#5c9ce6]" : ""}">
                        <div class="flex items-center justify-between">
                          <span class="font-semibold text-[11.5px] text-[#ffffff]">${cd.id}</span>
                          ${pill(cd.gate, cd.tone)}
                        </div>
                        <div class="text-[11px] text-[#b6c2cf]">${cd.name}</div>
                        <div class="flex justify-between text-[10px] text-[#6d7885] tnum"><span>${cd.envs} targets</span><span>${cd.age}</span></div>
                        <div class="text-[10px] text-[#eab854]">${cd.why}</div>
                        ${cd.pct > 0 && cd.pct < 100 ? `<div class="h-1 w-full bg-[#090e12] rounded-full overflow-hidden"><div class="h-full bg-[#4c90f0]" style="width:${cd.pct}%"></div></div>` : ""}
                      </div>
                    `,
                      )
                      .join("")}
                  </div>
                </div>
              `;
              })
              .join("")}
          </div>

          <div class="so-resp-full so-panel w-[252px] shrink-0 p-3 flex flex-col justify-between">
            <div class="space-y-3">
              <div class="flex justify-between items-center border-b border-[#1c242c] pb-2">
                <span class="font-bold text-[13.5px] text-[#ffffff]">${sel.id}</span>
                ${pill(sel.gate, sel.tone)}
              </div>
              <div class="space-y-1.5">
                <div class="text-[11px] font-semibold uppercase tracking-[0.08em] text-[#6d7885]">Reviewer Checklist</div>
                ${[
                  ["sbom", "Verify SPDX zero high/crit CVEs"],
                  ["soak", "Confirm canary soak < 1%"],
                  ["diode", "Verify diode SHA-256 receipt"],
                ]
                  .map(
                    (it) => `
                  <label class="flex items-center gap-2 text-[11.5px] text-[#dce4ee] py-1 cursor-pointer">
                    <input type="checkbox" ${state.revChecks[it[0]] ? "checked" : ""} data-onchange="state.revChecks['${it[0]}']=this.checked; renderMainWorkspace()" />
                    <span>${it[1]}</span>
                  </label>
                `,
                  )
                  .join("")}
              </div>
              <div class="space-y-2 pt-2 border-t border-[#1c242c]">
                <div class="text-[11px] font-semibold uppercase tracking-[0.08em] text-[#6d7885]">History (Newest First)</div>
                <div class="so-stream-item so-stream-amber text-[11px]"><div class="tnum text-[#6d7885]">09:42 PM · Policy</div><div class="text-[#dce4ee]">Window closed until 02:00Z</div></div>
                <div class="so-stream-item so-stream-emerald text-[11px]"><div class="tnum text-[#6d7885]">08:58 PM · M. Chen</div><div class="text-[#dce4ee]">Signed Approval 1/2</div></div>
              </div>
            </div>
            <button data-onclick="switchScreen('s15-plan-detail')" class="so-btn so-btn-blue w-full">Open Control Tower &rarr;</button>
          </div>
        </div>
      `;
  }

  // =========================================================================
  // SCREEN 17: SPOKES & LIVE AUDIT STREAMS (Matched 1:1 to f_02_15_onyx.png 4-Column Live Streams!)
  // =========================================================================
  function renderS17SpokesAudit(root) {
    const cols = [
      {
        title: "West Store-and-Forward",
        kpi: "18 Envs · 94% Synced",
        col: "#34d399",
        bar: "emerald",
        events: [
          [
            "Relay Agent",
            "09:54 PM",
            "Uploaded chunk 6/8 (31.4 MB) to spoke-west-relay spool",
          ],
          [
            "Drift Watch",
            "09:19 PM",
            "Flagged 768Mi memory override on edge-west-02",
          ],
        ],
      },
      {
        title: "Optical Diode Hall-B",
        kpi: "6 Envs · One-Way Lock",
        col: "#60a5fa",
        bar: "blue",
        events: [
          [
            "Diode Guard",
            "09:36 PM",
            "Verified SHA-256 receipt for 63.8 MB zstd delta bundle",
          ],
          ["S. Kline", "08:41 PM", "Authorized offline-vault-04 staging spool"],
        ],
      },
      {
        title: "Courier & Offshore",
        kpi: "6 Envs · 1 Degraded",
        col: "#eab854",
        bar: "amber",
        events: [
          [
            "Link Probe",
            "09:15 PM",
            "Packet loss 9.2% on remote-rig-09 satellite relay",
          ],
          [
            "K. Wong",
            "07:20 PM",
            "Exported signed media manifest for forward-arctic-01",
          ],
        ],
      },
      {
        title: "Signed Audit Ledger",
        kpi: "142 Verified Events",
        col: "#9881f3",
        bar: "violet",
        events: [
          [
            "M. Chen",
            "08:58 PM",
            "Recorded ECDSA hardware signature 1/2 on PLN-2026-0418",
          ],
          [
            "HSM Slot-02",
            "08:34 PM",
            "Signed 4 release artifacts under prod-root-2026",
          ],
        ],
      },
    ];

    root.innerHTML = `
        <div class="space-y-3 h-full flex flex-col">
          <div class="so-panel p-3.5 flex flex-wrap items-center justify-between gap-2.5 shrink-0">
            <div>
              <div class="text-[15px] font-bold text-[#ffffff]">Spoke Transport</div>
              <div class="text-[11.5px] text-[#eab854]">Example data</div>
            </div>
            ${governedBtn({ label: "Export Signed Audit Ledger", role: "Security Admin", auditEvent: "audit_event.exported", onClickJs: "", cls: "so-btn so-btn-emerald", iconName: "download" })}
          </div>

          <div class="so-grid-4 gap-3 flex-1 min-h-0">
            ${cols
              .map(
                (c) => `
              <div class="so-panel flex flex-col overflow-hidden">
                <div class="px-3 py-2.5 border-b border-[#1c242c] flex items-center justify-between">
                  <span class="font-semibold text-[12px] text-[#ffffff] truncate">${c.title}</span>
                  <span class="text-[10px] text-[#eab854]">Example data</span>
                </div>
                <div class="px-3 py-1.5 bg-[#0e1418] border-b border-[#1c242c] text-[11px] font-medium tnum" style="color:${c.col}">${c.kpi}</div>
                <div class="p-3 space-y-3 overflow-y-auto flex-1">
                  ${c.events
                    .map(
                      (e) => `
                    <div class="so-stream-item so-stream-${c.bar} space-y-1 pb-2.5 border-b border-[#171f26]">
                      <div class="flex justify-between text-[11px]"><span class="font-semibold text-[#f0f5fa]">${e[0]}</span><span class="tnum text-[#596573]">${e[1]}</span></div>
                      <div class="text-[11.5px] text-[#9aa5b4] leading-snug">${e[2]}</div>
                    </div>
                  `,
                    )
                    .join("")}
                </div>
              </div>
            `,
              )
              .join("")}
          </div>
        </div>
      `;
  }

  // =========================================================================
  // SCREEN 18: COMPONENT LIBRARY SHEET & SCREEN 19: ONTOLOGY REGISTRY
  // =========================================================================
  function renderS18Components(root) {
    root.innerHTML = `
        <div class="space-y-3 h-full flex flex-col">
          <div class="so-panel p-3.5 flex flex-wrap items-center justify-between gap-2.5 shrink-0">
            <span class="text-[15px] font-bold text-[#ffffff]">Conduit Component System (Tactical Dark Theme)</span>
            <div class="flex gap-1.5 code-id text-[10.5px]">
              <span class="px-2 py-0.5 rounded bg-[#0b1113] border border-[#24303c]">#0b1113</span>
              <span class="px-2 py-0.5 rounded bg-[#151b1f] border border-[#24303c]">#151b1f</span>
              <span class="px-2 py-0.5 rounded bg-[#268e5b] text-white">#268e5b</span>
              <span class="px-2 py-0.5 rounded bg-[#2d72d2] text-white">#2d72d2</span>
            </div>
          </div>
          <div class="so-grid-2 gap-3 flex-1">
            <div class="so-panel p-3.5 space-y-3">
              <div class="text-[13px] font-semibold text-[#ffffff]">Buttons &amp; Disabled-with-Reason Tooltips</div>
              <div class="flex flex-wrap gap-2">
                <button class="so-btn so-btn-emerald">&rarr; Review</button>
                <button class="so-btn so-btn-blue">Approve</button>
                <button class="so-btn">Secondary</button>
                <button class="so-btn so-btn-outline-warn">Pause</button>
                <button class="so-btn so-btn-outline-danger">Reject</button>
              </div>
              <div class="pt-2 border-t border-[#1c242c] flex gap-2">
                ${governedBtn({ label: "Approve Plan", role: "Approver", auditEvent: "plan.approved", onClickJs: "", cls: "so-btn so-btn-emerald", iconName: "lock", blockedReason: "2 Requirements Blocked" })}
                ${governedBtn({ label: "Delete Profile", role: "Owner", auditEvent: "work_profile.deleted", onClickJs: "", cls: "so-btn so-btn-danger", iconName: "lock", blockedReason: "Confirmation name required" })}
              </div>
            </div>
            <div class="so-panel p-3.5 space-y-3">
              <div class="text-[13px] font-semibold text-[#ffffff]">Telemetry Capsules &amp; Status Pills</div>
              <div class="flex flex-wrap gap-1.5">
                ${agentChip("Pipeline Agent", 6, 2)}
                ${toolChip("SPDX Scanner", 418, 0)}
                ${ontChip("ConduitPlan", 14, 1)}
              </div>
              <div class="flex flex-wrap gap-1.5 pt-2 border-t border-[#1c242c]">
                ${pill("Synced", "green")} ${pill("In Progress", "blue")} ${pill("Drift Detected", "amber")} ${pill("Blocked", "red")} ${pill("GO", "green")} ${pill("NO-GO", "red")}
              </div>
            </div>
          </div>
          ${releaseGates.showcase()}
        </div>
      `;
  }

  function renderS19Ontology(root) {
    const rows = [
      [
        "Work profile",
        "Active, Upgrade Available, Archived, Deleting",
        "1:N Workspace, 1:N Story, 1:N Environment",
        "s12-lifecycle",
      ],
      [
        "Workspace",
        "Active, Building, Drifted, Gated, Idle, Error",
        "1:N Change request, runtimeStatus",
        "s02-workspaces",
      ],
      [
        "Story",
        "Scoping, Ready, In Progress, Paused, Blocked, Done",
        "1:N Packaging run, 0..1:N Change request",
        "s04-scope-plan",
      ],
      [
        "Change request",
        "Draft, In Review, Approved, Included in Plan",
        "0..N:0..1 Plan",
        "s10-diff",
      ],
      [
        "Packaging run",
        "Queued, Running, Staged, Succeeded, Failed, Canceled",
        "1:N Step, 1:N Artifact, 1:0..1 Plan",
        "s08-staged-run",
      ],
      [
        "Plan",
        "Proposed, Awaiting Approval, Approved, Rolling Out, Succeeded",
        "1:N Requirement, 1:0..N Approval, 1:N Environment",
        "s15-plan-detail",
      ],
      [
        "Environment",
        "Online, Drift Detected, Degraded, Awaiting Bundle",
        "1 Spoke, 0..N Installation, 1 Reported state",
        "s14-env-detail",
      ],
      [
        "Spoke",
        "Connected, Store-and-Forward, Diode One-Way, Courier",
        "1:1..N Environment",
        "s17-spokes-audit",
      ],
    ];
    root.innerHTML = `
        <div class="so-panel overflow-hidden h-full flex flex-col">
          <div class="px-3.5 py-2.5 border-b border-[#1c242c] flex justify-between items-center">
            <span class="text-[14.5px] font-bold text-[#ffffff]">Conduit Domain Ontology (16 Object Types)</span>
            ${ontChip("ConduitOntology", 16, 18)}
          </div>
          <div class="overflow-y-auto flex-1">
            <div class="so-table-scroll"><table class="so-table">
              <thead><tr><th>Object Type</th><th>Statuses</th><th>Links</th><th>Open</th></tr></thead>
              <tbody>
                ${rows.map((r) => `<tr data-onclick="switchScreen('${r[3]}')" class="cursor-pointer"><td class="font-semibold text-[#5c9ce6]">${r[0]}</td><td class="text-[#8b96a5]">${r[1]}</td><td>${r[2]}</td><td>${pill("Inspect &rarr;", "blue")}</td></tr>`).join("")}
              </tbody>
            </table></div>
          </div>
        </div>
      `;
  }

  // =========================================================================
  // RELEASE-MANAGEMENT EXTENSION VIEWS (PATTERNS 1..8) + CMD+K OMNIBAR
  // =========================================================================
  const RM_EXAMPLE_PLANS = [
    {
      planId: "PLN-2026-0416",
      entity: "ingress-gateway",
      type: "Upgrade",
      status: "Failed",
      tone: "red",
      duration: "02m 38s",
      started: "2026-11-13 02:04:11Z",
    },
    {
      planId: "PLN-2026-0418",
      entity: "uds-core",
      type: "Upgrade",
      status: "Awaiting Approval",
      tone: "amber",
      duration: "—",
      started: "2026-11-13 20:18:12Z",
    },
    {
      planId: "PLN-2026-0412",
      entity: "runtime-admission",
      type: "Config Change",
      status: "Rolling Out",
      tone: "blue",
      duration: "06m 52s",
      started: "2026-11-13 21:41:00Z",
    },
    {
      planId: "PLN-2026-0409",
      entity: "cert-manager-crds",
      type: "Upgrade",
      status: "Succeeded",
      tone: "green",
      duration: "01m 34s",
      started: "2026-11-12 02:15:30Z",
    },
    {
      planId: "PLN-2026-0405",
      entity: "log-forwarder",
      type: "Rollback",
      status: "Succeeded",
      tone: "green",
      duration: "01m 07s",
      started: "2026-11-11 03:40:19Z",
    },
  ];

  const RM_EXAMPLE_COMMANDS = [
    {
      id: "CMD-904",
      entity: "ingress-gateway",
      kind: "Diagnostic Bundle",
      status: "Succeeded",
      tone: "green",
      duration: "18s",
      started: "2026-11-13 02:08:00Z",
      by: "J. Alvarez",
    },
    {
      id: "CMD-902",
      entity: "spoke-spool-agent",
      kind: "Flush Spool",
      status: "Succeeded",
      tone: "green",
      duration: "9s",
      started: "2026-11-13 01:14:22Z",
      by: "S. Kline",
    },
    {
      id: "CMD-899",
      entity: "runtime-admission",
      kind: "Reconcile Now",
      status: "Succeeded",
      tone: "green",
      duration: "12s",
      started: "2026-11-12 18:45:10Z",
      by: "M. Chen",
    },
  ];

  const RM_EXAMPLE_OVERRIDES = [
    {
      id: "OVR-119",
      scope: "ingress-gateway",
      kind: "Channel Pin (edge-cohorts-hotfix)",
      status: "Active",
      tone: "green",
      reason:
        "Hold minor version while validating 1Gi memory ceiling in CR-308",
      expires: "2026-11-15 05:00Z",
    },
    {
      id: "OVR-117",
      scope: "ingress-gateway",
      kind: "Suppress Upgrades (Auto)",
      status: "Active",
      tone: "amber",
      reason: "Auto-created after PLN-2026-0416 OOMKilled failure",
      expires: "Until cleared",
    },
    {
      id: "OVR-114",
      scope: "Environment (Global)",
      kind: "Allow Outside Window",
      status: "Expired",
      tone: "muted",
      reason: "Certificate rotation emergency window",
      expires: "2026-11-10 08:00Z",
    },
  ];

  const RM_CHANNELS = [
    {
      id: "dev",
      name: "dev",
      tier: 1,
      current: "0.31.3-rc.1",
      candidate: null,
      envs: 4,
      upstream: null,
      soakDone: 3.5,
      soakReq: 1.0,
      health: "100%",
      healthReq: "90%",
      labels: ["sbom=spdx-2.3"],
      state: "Active",
      tone: "green",
    },
    {
      id: "staging",
      name: "staging",
      tier: 2,
      current: "0.31.2",
      candidate: "0.31.3-rc.1",
      envs: 6,
      upstream: "dev",
      soakDone: 14.2,
      soakReq: 4.0,
      health: "100%",
      healthReq: "95%",
      labels: ["sbom=spdx-2.3", "cve-critical=0"],
      state: "Active",
      tone: "green",
    },
    {
      id: "edge-cohorts",
      name: "edge cohorts",
      tier: 3,
      current: "0.31.2",
      candidate: null,
      envs: 22,
      upstream: "staging",
      soakDone: 6.4,
      soakReq: 6.0,
      health: "96.0%",
      healthReq: "95%",
      labels: ["sbom=spdx-2.3", "cve-critical=0", "signature=required"],
      state: "Active",
      tone: "green",
    },
    {
      id: "disconnected",
      name: "disconnected",
      tier: 4,
      current: "0.31.1",
      candidate: "0.31.2",
      envs: 16,
      upstream: "edge-cohorts",
      soakDone: 6.4,
      soakReq: 6.0,
      health: "96.0%",
      healthReq: "95%",
      labels: [
        "sbom=spdx-2.3",
        "cve-critical=0",
        "signature=required",
        "offline-bundle=signed",
      ],
      state: "Upgrade Ready",
      tone: "amber",
    },
  ];

  const RM_ACTION_SPECS = {
    execute_plan: {
      actionId: "execute_plan",
      label: "Execute Release Plan PLN-2026-0418",
      audit: "plan.execution_dispatched",
      blockingRule:
        "Blocked if maintenance window is closed without active Override; OR missing required Approval quorum; OR any linked Artifact is UNSIGNED; OR a conflicting Plan is currently ROLLING_OUT on target Environments",
    },
    promote_release_manual: {
      actionId: "promote_release_manual",
      label: "Promote uds-core 0.31.2 to disconnected",
      audit: "release_channel.manual_promoted",
      blockingRule:
        "Blocked if target Artifact is UNSIGNED; OR channel label requirements are unmet; OR missing required Approval quorum; OR conflicting Plan is ROLLING_OUT",
    },
  };

  const RM_CRS = [
    {
      id: "CR-308",
      title:
        "Raise ingress-gateway memory ceiling 768Mi -> 1Gi & narrow relay CIDR",
      env: "edge-west-02",
      author: "J. Alvarez",
      status: "Approved",
      tone: "green",
      add: 6,
      rem: 4,
      base: [
        "replicaCount: 2",
        "image:",
        "  repository: registry.conduit.internal/ingress-gateway",
        '  tag: "1.23.4"',
        "resources:",
        "  requests:",
        '    cpu: "250m"',
        '    memory: "512Mi"',
        "  limits:",
        '    cpu: "1000m"',
        '    memory: "768Mi"',
        "egressRelayPolicy:",
        '  allowedCidr: "10.240.0.0/16"',
        "  maxSpoolMegabytes: 1024",
      ],
      next: [
        "replicaCount: 2",
        "image:",
        "  repository: registry.conduit.internal/ingress-gateway",
        '  tag: "1.23.4"',
        "resources:",
        "  requests:",
        '    cpu: "250m"',
        '    memory: "512Mi"',
        "  limits:",
        '    cpu: "1000m"',
        '    memory: "1Gi"',
        "egressRelayPolicy:",
        '  allowedCidr: "10.240.12.0/24"',
        "  maxSpoolMegabytes: 2048",
      ],
    },
    {
      id: "CR-309",
      title:
        "Set spoke-diode-hall-b zstd frame slice to 16MB for optical diode",
      env: "offline-vault-04",
      author: "S. Kline",
      status: "In Review",
      tone: "amber",
      add: 3,
      rem: 1,
      base: [
        "diodeTransfer:",
        "  chunkMegabytes: 32",
        "  verifySha256Receipt: true",
      ],
      next: [
        "diodeTransfer:",
        "  chunkMegabytes: 16",
        "  verifySha256Receipt: true",
        "  retryWindowSeconds: 120",
      ],
    },
    {
      id: "CR-307",
      title: "Subscribe edge-east-01 to staging release channel",
      env: "edge-east-01",
      author: "M. Chen",
      status: "Included in Plan",
      tone: "blue",
      add: 1,
      rem: 1,
      base: ["environment:", "  releaseChannel: dev"],
      next: ["environment:", "  releaseChannel: staging"],
    },
  ];

  function rmApiStrip(statusBadge, sig, ontType) {
    const tone = statusBadge.includes("REAL") ? "green" : "blue";
    return `
        <div class="so-card px-3 py-1.5 flex flex-wrap items-center justify-between gap-2 shrink-0">
          <div class="flex flex-wrap items-center gap-2">
            ${pill(statusBadge, tone)}
            <span class="code-id text-[11px] text-[#dce4ee]">${sig}</span>
          </div>
          ${ontChip(ontType, 14, 1)}
        </div>
      `;
  }

  function rmEnvHeaderHtml(activeTab) {
    const tabs = [
      ["overview", "Overview"],
      ["activity", "Activity (5)"],
      ["installed", "Installed (16)"],
      ["changes", "Changes (3)"],
      ["config", "Config & Diff"],
      ["settings", "Settings"],
    ];
    return `
        <div class="so-panel px-3.5 pt-3 pb-0 shrink-0">
          <div class="flex flex-wrap items-start justify-between gap-3 pb-2.5">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <span class="code-id text-[16px] font-bold text-[#ffffff]">edge-west-02</span>
                ${pill("Drift Detected", "amber")}
                ${pill("Store-and-Forward", "blue")}
                <span class="so-pill code-id">channel: edge-cohorts</span>
                ${agentChip("Drift Monitor", 16, 1)}
              </div>
              <div class="text-[11.5px] text-[#8b96a5] mt-1">
                Spoke <span class="code-id text-[#ffffff]">spoke-west-relay</span> &middot; Kubernetes <span class="code-id text-[#ffffff]">v1.30.6</span> &middot; Last check-in <span class="code-id text-[#34d399]">42s ago</span> &middot; Spool lag <span class="code-id">14.2 MB</span>
              </div>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <button type="button" data-onclick="state.rmActivitySubnav='overrides'; switchScreen('rm02-env-activity')" class="so-btn">Create Override</button>
              <button type="button" data-onclick="switchScreen('rm06-promotion-pipe')" class="so-btn so-btn-emerald">&rarr; Promote Channel Release</button>
            </div>
          </div>

          <div role="tablist" aria-label="Environment resource tabs" class="flex items-center gap-1 border-t border-[#1c242c] overflow-x-auto no-scrollbar">
            ${tabs
              .map(
                ([tid, lbl]) => `
              <button type="button" role="tab" aria-selected="${activeTab === tid ? "true" : "false"}"
                data-onclick="if('${tid}'==='activity'){switchScreen('rm02-env-activity');}else if('${tid}'==='config'){switchScreen('s14-env-detail');}else if('${tid}'==='changes'){switchScreen('rm07-change-requests');}else{state.rmEnvResourceTab='${tid}';switchScreen('rm01-env-page');}"
                class="h-[32px] px-3 text-[12px] font-medium border-b-2 transition-colors whitespace-nowrap ${
                  activeTab === tid
                    ? "border-[#5c9ce6] text-[#5c9ce6] font-semibold"
                    : "border-transparent text-[#8b96a5] hover:text-white"
                }">${lbl}</button>
            `,
              )
              .join("")}
          </div>
        </div>
      `;
  }

  function renderRM01EnvPage(root) {
    const days = [
      ["Mon", "02:00–05:00Z", true],
      ["Tue", "02:00–05:00Z", true],
      ["Wed", "02:00–05:00Z", true],
      ["Thu", "02:00–05:00Z", true],
      ["Fri", "02:00–05:00Z", true],
      ["Sat", "Closed", false],
      ["Sun", "01:00–06:00Z", true],
    ];
    root.innerHTML = `
        <div class="space-y-2.5 flex flex-col">
          ${rmApiStrip("PROPOSED", "GET /v1/environments/{environmentId}", "ConduitEnvironment")}
          ${rmEnvHeaderHtml(state.rmEnvResourceTab)}

          <div class="so-panel p-3.5 space-y-2">
            <div class="flex flex-wrap justify-between items-center gap-2">
              <span class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Proportional Installation Health &middot; 16 Managed Entities</span>
              <span class="code-id text-[11px] text-[#34d399]">68.8% Synced &middot; 3 Drifted &middot; 1 Degraded</span>
            </div>
            <div class="flex h-[9px] rounded-[4px] overflow-hidden gap-[2px] bg-[#090e12]">
              <div style="width:68.75%;background:#2e9a62"></div>
              <div style="width:18.75%;background:#d99b26"></div>
              <div style="width:6.25%;background:#e05257"></div>
              <div style="width:6.25%;background:#4c90f0"></div>
            </div>
            <div class="flex flex-wrap gap-4 text-[11.5px] text-[#8b96a5] pt-0.5">
              <span><i class="inline-block w-2 h-2 rounded-[2px] bg-[#2e9a62] mr-1.5"></i>Synced <strong class="code-id text-white">11</strong></span>
              <span><i class="inline-block w-2 h-2 rounded-[2px] bg-[#d99b26] mr-1.5"></i>Drifted <strong class="code-id text-white">3</strong></span>
              <span><i class="inline-block w-2 h-2 rounded-[2px] bg-[#e05257] mr-1.5"></i>Degraded (OOM) <strong class="code-id text-white">1</strong></span>
              <span><i class="inline-block w-2 h-2 rounded-[2px] bg-[#4c90f0] mr-1.5"></i>Pending Window <strong class="code-id text-white">1</strong></span>
            </div>
          </div>

          <div class="so-grid-2 gap-2.5">
            <div class="so-opt-card p-3.5 space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#34d399]">Release Channel Subscription</span>
                <button type="button" data-onclick="switchScreen('rm06-promotion-pipe')" class="so-btn so-btn-sm so-btn-emerald">&rarr; Promotion Graph</button>
                <button type="button" data-onclick="switchScreen('rm11-debug-terminal')" class="so-btn so-btn-sm">Debug Terminal</button>
              </div>
              <div class="flex flex-wrap items-center gap-2">
                <span class="code-id text-[15px] font-bold text-white">edge-cohorts</span>
                ${pill("Active", "green")}
                ${pill("target: uds-core 0.31.2", "blue")}
              </div>
              <div class="text-[11.5px] text-[#8b96a5]">Subscribes cluster entities to approved Wave 2 releases after 6.0h soak on <code class="code-id text-white">staging</code>.</div>
              <div class="pt-2 border-t border-[#1a2e26] flex items-center justify-between text-[11px]">
                <span class="text-[#eab854]">1 entity pinned: <strong class="code-id">ingress-gateway</strong> (OVR-119)</span>
                ${ontChip("ConduitReleaseChannel", 4, 0)}
              </div>
            </div>

            <div class="so-panel p-3.5 space-y-2">
              <div class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Environment Labels &amp; Selector Facts</div>
              <div class="flex flex-wrap gap-1.5 pt-1">
                <span class="so-pill code-id">region=us-west-2</span>
                <span class="so-pill code-id">cohort=wave-2-regional</span>
                ${pill("signature=required", "blue")}
                <span class="so-pill code-id">mode=store-and-forward</span>
                <span class="so-pill code-id">k8s=1.30</span>
                ${pill("sbom=spdx-2.3", "green")}
              </div>
              <div class="text-[11px] text-[#596573] pt-1">Evaluated against <code class="code-id">ReleaseChannel.requiredLabels</code> prior to Plan creation.</div>
            </div>

            <div class="so-panel p-3.5 space-y-2">
              <div class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Owners &amp; Escalation Contacts</div>
              <div class="so-card p-2 flex items-center justify-between">
                <span class="text-[12px]"><strong class="code-id text-white">@platform-west-ops</strong> <span class="text-[#8b96a5]">(Owning RBAC Group)</span></span>
                ${pill("Environment Editor", "blue")}
              </div>
              <div class="so-card p-2 flex items-center justify-between text-[12px]">
                <span class="text-[#dce4ee]">J. Alvarez &middot; S. Kline</span>
                <span class="code-id text-[11px] text-[#8b96a5]">pager-west-k8s</span>
              </div>
            </div>

            <div class="so-panel p-3.5 space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Maintenance Window Calendar (UTC)</span>
                ${pill("Closed · Opens 02:00Z", "amber")}
              </div>
              <div class="grid grid-cols-7 gap-1 pt-1">
                ${days
                  .map(
                    ([d, w, on]) => `
                  <div class="so-card p-1.5 text-center" style="border-color:${on ? "#28426b" : "#1e262d"}">
                    <div class="text-[10px] font-semibold ${on ? "text-white" : "text-[#596573]"}">${d}</div>
                    <div class="code-id text-[9px] mt-0.5 ${on ? "text-[#60a5fa]" : "text-[#596573]"}">${w}</div>
                  </div>
                `,
                  )
                  .join("")}
              </div>
            </div>
          </div>

          <div class="so-panel overflow-hidden">
            <div class="px-3.5 py-2 bg-[#0e1418] border-b border-[#1c242c] flex flex-wrap items-center justify-between gap-2">
              <span class="text-[11.5px] font-semibold text-white">Managed Installations Snapshot</span>
              <button type="button" data-onclick="switchScreen('rm07-change-requests')" class="so-btn so-btn-sm">Inspect Config Changes (CR-308) &rarr;</button>
            </div>
            <div class="so-table-scroll"><table class="so-table">
              <thead><tr><th>Entity</th><th>Channel</th><th>Desired</th><th>Reported</th><th>Status</th><th>Last / Active Plan</th></tr></thead>
              <tbody>
                <tr data-onclick="switchScreen('rm03-plan-detail')" class="cursor-pointer"><td class="code-id font-semibold text-white">ingress-gateway</td><td class="code-id">edge-cohorts-hotfix</td><td class="code-id">1.23.4</td><td class="code-id text-[#eab854]">1.23.3</td><td>${pill("Degraded", "red")}</td><td class="code-id text-[#f87171]">PLN-2026-0416 (Failed)</td></tr>
                <tr data-onclick="switchScreen('rm04-constraints')" class="cursor-pointer"><td class="code-id font-semibold text-white">uds-core</td><td class="code-id">edge-cohorts</td><td class="code-id">0.31.2</td><td class="code-id text-[#eab854]">0.31.1</td><td>${pill("Drift Detected", "amber")}</td><td class="code-id text-[#60a5fa]">PLN-2026-0418 (4 Constraints)</td></tr>
                <tr><td class="code-id font-semibold text-white">cert-manager-crds</td><td class="code-id">edge-cohorts</td><td class="code-id">1.15.2</td><td class="code-id text-[#34d399]">1.15.2</td><td>${pill("Synced", "green")}</td><td class="code-id">PLN-2026-0409</td></tr>
              </tbody>
            </table></div>
          </div>
        </div>
      `;
  }

  function renderRM02EnvActivity(root) {
    const plans = RM_EXAMPLE_PLANS.filter(
      (p) =>
        (state.rmPlanStatus === "ALL" || p.status === state.rmPlanStatus) &&
        (!state.rmPlanSearch ||
          p.entity.toLowerCase().includes(state.rmPlanSearch.toLowerCase()) ||
          p.planId.toLowerCase().includes(state.rmPlanSearch.toLowerCase())),
    );
    root.innerHTML = `
        <div class="space-y-2.5 flex flex-col">
          ${rmApiStrip("PROPOSED", "GET /v1/environments/{environmentId}/activity", "ConduitPlan")}
          ${rmEnvHeaderHtml("activity")}

          <div class="so-resp-stack flex gap-3 items-start">
            <aside class="so-resp-full so-panel w-[192px] shrink-0 p-2 space-y-1" aria-label="Activity subnav">
              <div class="px-2 py-1 text-[9.5px] font-semibold uppercase tracking-[0.09em] text-[#596573]">Activity Streams</div>
              ${[
                ["plans", "Plans", RM_EXAMPLE_PLANS.length],
                ["commands", "Commands", RM_EXAMPLE_COMMANDS.length],
                ["overrides", "Overrides", RM_EXAMPLE_OVERRIDES.length],
              ]
                .map(
                  ([k, lbl, c]) => `
                <button type="button" data-onclick="state.rmActivitySubnav='${k}'; renderMainWorkspace()"
                  class="so-nav-row ${state.rmActivitySubnav === k ? "is-active" : ""}">
                  <span>${lbl}</span>
                  <span class="tnum text-[10.5px] px-1.5 rounded bg-[#161e26]">${c}</span>
                </button>
              `,
                )
                .join("")}
            </aside>

            <section class="so-panel flex-1 min-w-0 p-3 space-y-2.5">
              ${
                state.rmActivitySubnav === "plans"
                  ? `
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <div class="flex flex-wrap items-center gap-1.5">
                    <input type="search" value="${state.rmPlanSearch}" data-oninput="state.rmPlanSearch=this.value; renderMainWorkspace()"
                      placeholder="Filter entity or plan ID..." aria-label="Filter plans" class="so-input w-[200px]" />
                    ${[
                      "ALL",
                      "Failed",
                      "Rolling Out",
                      "Awaiting Approval",
                      "Succeeded",
                    ]
                      .map(
                        (st) => `
                      <button type="button" data-onclick="state.rmPlanStatus='${st}'; renderMainWorkspace()"
                        class="so-btn so-btn-sm ${state.rmPlanStatus === st ? "so-btn-blue" : ""}">${st}</button>
                    `,
                      )
                      .join("")}
                  </div>
                  <span class="text-[11px] text-[#596573]">Select any Plan row to inspect ordered tasks &amp; dispatch timeline &rarr;</span>
                </div>
                <div class="so-table-scroll"><table class="so-table" aria-label="Environment Plans table">
                  <thead><tr><th>Entity</th><th>Type</th><th>Status</th><th style="text-align:right">Duration</th><th>Started</th></tr></thead>
                  <tbody>
                    ${plans
                      .map(
                        (p) => `
                      <tr data-onclick="switchScreen('rm03-plan-detail')" class="cursor-pointer">
                        <td><div class="font-semibold text-white">${p.entity}</div><div class="code-id text-[10.5px] text-[#60a5fa]">${p.planId}</div></td>
                        <td><span class="so-pill">${p.type}</span></td>
                        <td>${pill(p.status, p.tone)}</td>
                        <td class="code-id" style="text-align:right">${p.duration}</td>
                        <td class="code-id text-[#8b96a5]">${p.started}</td>
                      </tr>
                    `,
                      )
                      .join("")}
                  </tbody>
                </table></div>
              `
                  : state.rmActivitySubnav === "commands"
                    ? `
                <div class="flex justify-between items-center"><span class="text-[12px] font-semibold text-white">Imperative Agent Commands</span>${ontChip("ConduitCommand", 3, 0)}</div>
                <div class="so-table-scroll"><table class="so-table">
                  <thead><tr><th>Command ID</th><th>Target Entity</th><th>Kind</th><th>Status</th><th style="text-align:right">Duration</th><th>Issued By</th><th>Started</th></tr></thead>
                  <tbody>
                    ${RM_EXAMPLE_COMMANDS.map((c) => `<tr><td class="code-id text-[#60a5fa]">${c.id}</td><td class="code-id">${c.entity}</td><td><span class="so-pill">${c.kind}</span></td><td>${pill(c.status, c.tone)}</td><td class="code-id" style="text-align:right">${c.duration}</td><td>${c.by}</td><td class="code-id">${c.started}</td></tr>`).join("")}
                  </tbody>
                </table></div>
              `
                    : `
                <div class="flex justify-between items-center"><span class="text-[12px] font-semibold text-white">Maintenance &amp; Suppression Overrides</span>${ontChip("ConduitOverride", 3, 1)}</div>
                <div class="so-table-scroll"><table class="so-table">
                  <thead><tr><th>Override ID</th><th>Scope</th><th>Kind</th><th>Status</th><th>Reason</th><th>Expires</th></tr></thead>
                  <tbody>
                    ${RM_EXAMPLE_OVERRIDES.map((o) => `<tr><td class="code-id text-[#eab854]">${o.id}</td><td class="code-id">${o.scope}</td><td>${pill(o.kind, "violet")}</td><td>${pill(o.status, o.tone)}</td><td class="text-[#8b96a5]">${o.reason}</td><td class="code-id">${o.expires}</td></tr>`).join("")}
                  </tbody>
                </table></div>
              `
              }
            </section>
          </div>
        </div>
      `;
  }

  function renderRM03PlanDetail(root) {
    const rawLog = [
      "[2026-11-13T02:04:11.082Z] INFO  hub.dispatcher     Issued plan=PLN-2026-0416 env=edge-west-02 window=mw-west-daily",
      "[2026-11-13T02:04:14.419Z] INFO  agent.receiver     Received signed envelope via spoke-west-relay (lag=3.33s)",
      "[2026-11-13T02:04:25.601Z] INFO  agent.task[01/05]  Validated 14 Kubernetes v1.30 manifests & CRD schemas",
      "[2026-11-13T02:04:31.890Z] INFO  agent.task[02/05]  Verified cosign keyId=prod-root-2026 sha256:9c71fe08...2d44",
      "[2026-11-13T02:04:45.912Z] INFO  agent.task[03/05]  Applied environment values override rev=r41 (limits.memory=768Mi)",
      "[2026-11-13T02:06:44.118Z] ERROR k8s.pod.watch      Pod conduit-system/ingress-gateway-7d9c4b8f-x92lm OOMKilled (exitCode=137)",
      "[2026-11-13T02:06:49.904Z] FATAL agent.task[04/05]  helm upgrade --atomic timed out; rolled back to v1.23.3 & engaged OVR-117",
    ].join("\n");

    const tasks = [
      [
        "01",
        "Validate Kubernetes v1.30 CRD & admission schemas",
        "Succeeded",
        "green",
        "11s",
        "14/14 manifests validated cleanly",
      ],
      [
        "02",
        "Verify hardware signature against prod-root-2026",
        "Succeeded",
        "green",
        "06s",
        "Digest sha256:9c71fe08...2d44 verified",
      ],
      [
        "03",
        "Stage ConfigMap & Secret overrides (revision r41)",
        "Succeeded",
        "green",
        "14s",
        "Rendered memory limit 768Mi",
      ],
      [
        "04",
        "Execute atomic Helm upgrade ingress-gateway -> 1.23.4",
        "Failed",
        "red",
        "127s",
        "Container ingress-proxy OOMKilled (exit 137) during replay burst",
      ],
      [
        "05",
        "Verify post-upgrade readiness & clear hold",
        "Skipped",
        "muted",
        "00s",
        "Skipped after task #04 non-zero exit; rolled back to v1.23.3",
      ],
    ];

    root.innerHTML = `
        <div class="space-y-2.5 flex flex-col">
          ${rmApiStrip("PROPOSED", "GET /v1/plans/{planId}", "ConduitPlan")}
          <div class="so-warn-card p-3.5 space-y-2.5" style="border-top-color:#e05257;">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div>
                <div class="flex flex-wrap items-center gap-2">
                  <span class="code-id text-[15.5px] font-bold text-white">PLN-2026-0416</span>
                  ${pill("Failed", "red")}
                  <span class="so-pill">Upgrade</span>
                  ${agentChip("Helm Executor", 5, 2)}
                </div>
                <div class="text-[13px] font-semibold text-white mt-1">ingress-gateway (1.23.3 &rarr; 1.23.4) on edge-west-02 &mdash; OOMKilled at Task 4/5</div>
              </div>
              <div class="flex flex-wrap gap-2">
                ${kpiBox("02m 38s", "DURATION", "#eab854")}
                ${kpiBox("Exit 137", "OOMKILLED", "#f87171")}
                ${kpiBox("OVR-117", "SUPPRESSED", "#60a5fa")}
              </div>
            </div>
            <div class="so-striped-bar"><div class="so-striped-fill" style="width:75%;background:repeating-linear-gradient(-45deg,#b8383c 0px,#b8383c 7px,#de5256 7px,#de5256 14px);"></div></div>
          </div>

          <div class="so-grid-2 gap-3 items-start">
            <div class="so-panel p-3.5 space-y-2">
              <div class="text-[11px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Ordered Task List (5 Tasks)</div>
              <div class="so-table-scroll"><table class="so-table">
                <thead><tr><th>#</th><th>Ordered Task</th><th>Status</th><th style="text-align:right">Duration</th></tr></thead>
                <tbody>
                  ${tasks.map((t) => `<tr style="${t[2] === "Failed" ? "background:rgba(231,106,110,0.10)" : ""}"><td class="code-id text-[#596573]">${t[0]}</td><td><div class="font-semibold text-white">${t[1]}</div><div class="text-[11px] text-[#8b96a5]">${t[5]}</div></td><td>${pill(t[2], t[3])}</td><td class="code-id" style="text-align:right">${t[4]}</td></tr>`).join("")}
                </tbody>
              </table></div>
            </div>

            <div class="so-panel p-3.5 space-y-2.5">
              <div class="text-[11px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Issued &rarr; Received &rarr; Failed Dispatch Timeline</div>
              <div class="so-card so-stream-item p-2.5"><div class="flex justify-between items-center">${pill("1. Issued", "blue")}<span class="code-id text-[11px] text-[#8b96a5]">2026-11-13T02:04:11.082Z</span></div><div class="text-[11.5px] text-[#8b96a5] mt-1">Dispatched by <strong class="text-white">conduit-hub-orchestrator</strong> in window <code class="code-id">mw-west-daily</code>.</div></div>
              <div class="so-card so-stream-item p-2.5"><div class="flex justify-between items-center">${pill("2. Received", "blue")}<span class="code-id text-[11px] text-[#8b96a5]">2026-11-13T02:04:14.419Z (+3.34s)</span></div><div class="text-[11.5px] text-[#8b96a5] mt-1">Verified by agent on <code class="code-id text-white">edge-west-02</code> via <code class="code-id">spoke-west-relay</code>.</div></div>
              <div class="so-card so-stream-item so-stream-red p-2.5" style="background:rgba(43,21,25,0.45)"><div class="flex justify-between items-center">${pill("3. Failed", "red")}<span class="code-id text-[11px] text-[#f87171]">2026-11-13T02:06:49.904Z (+158.8s)</span></div><div class="text-[11.5px] text-white mt-1">Container <code class="code-id">ingress-proxy</code> exceeded <code class="code-id">768Mi</code> limit; rolled back to v1.23.3.</div></div>

              <div class="flex items-center justify-between pt-1">
                <span class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Raw Agent Output</span>
                <div class="flex gap-1.5">
                  <button type="button" data-onclick="copyRmRawLog()" class="so-btn so-btn-sm">${state.rmCopiedRaw ? "&#10003; Copied" : "Copy raw output"}</button>
                  <button type="button" data-onclick="switchScreen('rm07-change-requests')" class="so-btn so-btn-sm so-btn-emerald">&rarr; Fix in CR-308</button>
                </div>
              </div>
              <pre id="rm-raw-output" class="p-2.5 rounded bg-[#080c0f] border border-[#1c242c] code-id text-[10.5px] text-[#dce4ee] overflow-x-auto leading-relaxed">${rawLog}</pre>
            </div>
          </div>
        </div>
      `;
  }

  function copyRmRawLog() {
    const el = document.getElementById("rm-raw-output");
    if (el && navigator.clipboard) {
      try {
        navigator.clipboard.writeText(el.textContent || "");
      } catch (e) {}
    }
    state.rmCopiedRaw = true;
    renderMainWorkspace();
    setTimeout(() => {
      state.rmCopiedRaw = false;
      renderMainWorkspace();
    }, 1500);
  }

  function renderRM04Constraints(root) {
    const spec =
      RM_ACTION_SPECS[state.rmActionId] || RM_ACTION_SPECS.execute_plan;
    const clauses = spec.blockingRule.split(";").map((s) => s.trim());
    const rows = [
      {
        k: "rmFixWindow",
        title: "Maintenance window",
        clause: clauses[0],
        pass: state.rmFixWindow,
        ok: "Active Override OVR-120 authorizes execution outside 02:00–05:00 UTC.",
        bad: "Current UTC (21:48Z) is outside edge-west-02 window (02:00–05:00 UTC) and no global Override is active.",
        fix: "Create maintenance override",
        go: "state.rmActivitySubnav='overrides'; switchScreen('rm02-env-activity')",
      },
      {
        k: "rmFixApproval",
        title: "Missing approval",
        clause: clauses[1],
        pass: state.rmFixApproval,
        ok: "2 of 2 @fleet-approvers cryptographic approvals recorded.",
        bad: "1 of 2 required @fleet-approvers approvals recorded (M. Chen signed; awaiting second reviewer).",
        fix: "Open manual promotion review",
        go: "state.rmPromoDialogOpen=true; switchScreen('rm06-promotion-pipe')",
      },
      {
        k: "rmFixUnsigned",
        title: "Unsigned artifact",
        clause: clauses[2],
        pass: state.rmFixUnsigned,
        ok: "All 4 release bundle artifacts carry verified prod-root-2026 signatures.",
        bad: "Candidate extension telemetry-collector-0.9.4.tgz in RUN-8844 is UNSIGNED (pending HSM slot).",
        fix: "Inspect bundle signatures & SBOM",
        go: "switchScreen('rm08-bundle-preview')",
      },
      {
        k: "rmFixRolling",
        title: "Plan rolling out",
        clause: clauses[3],
        pass: state.rmFixRolling,
        ok: "Zero conflicting Plans are ROLLING_OUT on target Environments.",
        bad: "Plan PLN-2026-0412 (runtime-admission) is currently ROLLING_OUT across 5 shared Environments.",
        fix: "View active rollout PLN-2026-0412",
        go: "state.rmActivitySubnav='plans'; switchScreen('rm02-env-activity')",
      },
    ];
    const blocked = rows.filter((r) => !r.pass).length;

    root.innerHTML = `
        <div class="space-y-2.5 flex flex-col">
          ${rmApiStrip("PROPOSED", "POST /v1/actions/{actionId}/evaluate-constraints", "ConduitRequirement")}
          <div class="${blocked > 0 ? "so-warn-card" : "so-opt-card"} p-3.5 space-y-2.5">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <div class="space-y-1">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="text-[10.5px] font-semibold uppercase tracking-[0.08em] ${blocked > 0 ? "text-[#eab854]" : "text-[#34d399]"}">Ontology Action Constraint Gate</span>
                  ${agentChip("Gate Evaluator", 8, 1)}
                  <span class="so-pill code-id">Audit: ${spec.audit}</span>
                </div>
                <div class="flex items-center gap-2 pt-1">
                  <span class="text-[12px] text-[#8b96a5]">Action:</span>
                  <select data-onchange="state.rmActionId=this.value; renderMainWorkspace()" class="so-select code-id">
                    <option value="execute_plan" ${state.rmActionId === "execute_plan" ? "selected" : ""}>execute_plan (Plan)</option>
                    <option value="promote_release_manual" ${state.rmActionId === "promote_release_manual" ? "selected" : ""}>promote_release_manual (Release channel)</option>
                  </select>
                </div>
              </div>
              <div class="flex flex-wrap items-center gap-2">
                <button type="button" data-onclick="state.rmFixWindow=true;state.rmFixApproval=true;state.rmFixUnsigned=true;state.rmFixRolling=true;renderMainWorkspace()" class="so-btn">Simulate all satisfied</button>
                ${governedBtn({ label: spec.label, role: "Release Approver", auditEvent: spec.audit, onClickJs: `state.rmExecNotice='Dispatched ${spec.actionId} and wrote ${spec.audit}'; renderMainWorkspace()`, cls: "so-btn so-btn-emerald", blockedReason: blocked > 0 ? `${blocked} constraint(s) blocking action` : "" })}
              </div>
            </div>
            <div class="p-2 rounded bg-[#080c0f] border border-[#1c242c] code-id text-[10.8px] text-[#8b96a5]">
              OntologyActionSpec.blockingRule = "${spec.blockingRule}"
            </div>
          </div>

          ${state.rmExecNotice ? `<div class="so-opt-card p-2.5 code-id text-[11.5px] text-[#34d399]">&#10003; ${state.rmExecNotice}</div>` : ""}

          <div class="so-panel overflow-hidden">
            <div class="so-table-scroll"><table class="so-table" aria-label="Constraint rows">
              <thead><tr><th>Status</th><th>Blocking Rule &amp; Ontology Clause</th><th>Evaluation Reason</th><th>Simulate</th><th style="text-align:right">Link to Fix</th></tr></thead>
              <tbody>
                ${rows
                  .map(
                    (r) => `
                  <tr>
                    <td>${pill(r.pass ? "Pass" : "Blocked", r.pass ? "green" : "red")}</td>
                    <td><div class="font-semibold text-white">${r.title}</div><div class="code-id text-[10px] text-[#596573]">${r.clause}</div></td>
                    <td class="text-[#8b96a5] max-w-[390px]">${r.pass ? r.ok : r.bad}</td>
                    <td><button type="button" data-onclick="state['${r.k}']=!state['${r.k}']; state.rmExecNotice=''; renderMainWorkspace()" class="so-btn so-btn-sm">${r.pass ? "Re-block" : "Mark fixed"}</button></td>
                    <td style="text-align:right;white-space:nowrap"><a href="#fix" data-onclick="event.preventDefault(); ${r.go}" class="text-[#60a5fa] font-semibold hover:underline">${r.fix} &rarr;</a></td>
                  </tr>
                `,
                  )
                  .join("")}
              </tbody>
            </table></div>
          </div>
        </div>
      `;
  }

  function renderRM05ReleaseRollout(root) {
    const envs = [
      {
        env: "edge-east-01",
        wave: "Wave 1 · Canary",
        ch: "staging",
        mode: "Online mTLS",
        desired: "0.31.2",
        reported: "0.31.2",
        drift: 0,
        st: "Succeeded",
        tone: "green",
      },
      {
        env: "edge-south-06",
        wave: "Wave 1 · Canary",
        ch: "staging",
        mode: "Online mTLS",
        desired: "0.31.2",
        reported: "0.31.2",
        drift: 0,
        st: "Succeeded",
        tone: "green",
      },
      {
        env: "edge-west-02",
        wave: "Wave 2 · Regional",
        ch: "edge-cohorts",
        mode: "Store-and-Forward",
        desired: "0.31.2",
        reported: "0.31.1",
        drift: 3,
        st: "Awaiting Approval",
        tone: "amber",
      },
      {
        env: "metro-core-03",
        wave: "Wave 2 · Regional",
        ch: "edge-cohorts",
        mode: "Store-and-Forward",
        desired: "0.31.2",
        reported: "0.31.2",
        drift: 0,
        st: "Rolling Out",
        tone: "blue",
      },
      {
        env: "edge-central-05",
        wave: "Wave 2 · Regional",
        ch: "edge-cohorts",
        mode: "Online mTLS",
        desired: "0.31.2",
        reported: "0.31.2",
        drift: 1,
        st: "Succeeded",
        tone: "green",
      },
      {
        env: "offline-vault-04",
        wave: "Wave 3 · Disconnected",
        ch: "disconnected",
        mode: "Optical Diode",
        desired: "0.31.2",
        reported: "0.31.1",
        drift: 0,
        st: "Proposed",
        tone: "amber",
      },
      {
        env: "forward-north-01",
        wave: "Wave 3 · Disconnected",
        ch: "disconnected",
        mode: "Courier Bundle",
        desired: "0.31.2",
        reported: "0.31.0",
        drift: 1,
        st: "Proposed",
        tone: "amber",
      },
    ].filter(
      (r) => state.rmWaveFilter === "ALL" || r.wave === state.rmWaveFilter,
    );

    root.innerHTML = `
        <div class="space-y-2.5 flex flex-col">
          ${rmApiStrip("REAL + PROPOSED", "GET /v1/packaging-runs + GET /v1/releases/{version}/rollout", "ConduitWorkProfile")}
          <div class="so-panel p-3.5 flex flex-wrap items-center justify-between gap-3">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <span class="code-id text-[16px] font-bold text-white">uds-core 0.31.2</span>
                ${pill("Signed", "green")}
                ${pill("Run: RUN-8842", "blue")}
                ${pill("Total Drift: 5", "amber")}
              </div>
              <div class="code-id text-[11px] text-[#8b96a5] mt-1">digest: sha256:9c71fe08a41d8c02b9e7a11f404d12c91a0f2d44 &middot; Published 2026-11-13 20:18Z</div>
            </div>
            <div class="flex flex-wrap gap-2">
              <button type="button" data-onclick="switchScreen('rm10-ramped-rollout')" class="so-btn">Ramped Rollouts</button>
              <button type="button" data-onclick="switchScreen('rm09-recalls')" class="so-btn">Recalls</button>
              <button type="button" data-onclick="switchScreen('rm08-bundle-preview')" class="so-btn">Offline Bundle Preview</button>
              <button type="button" data-onclick="switchScreen('rm06-promotion-pipe')" class="so-btn so-btn-emerald">&rarr; Promotion Pipeline</button>
            </div>
          </div>

          <div class="so-grid-3 gap-2.5">
            ${[
              [
                "Wave 1 · Canary",
                "4 / 4 Synced",
                100,
                "0 drifts · Soak complete",
                "#2e9a62",
                "so-opt-card",
              ],
              [
                "Wave 2 · Regional",
                "6 / 10 Synced",
                60,
                "4 drifts across 2 clusters · 02:00Z window",
                "#d99b26",
                "so-warn-card",
              ],
              [
                "Wave 3 · Disconnected",
                "2 / 6 Synced",
                33,
                "1 drift · 4 awaiting 63.8 MB zstd bundle",
                "#4c90f0",
                "so-panel",
              ],
            ]
              .map(
                ([w, ratio, pct, sub, col, cls]) => `
              <div data-onclick="state.rmWaveFilter=(state.rmWaveFilter==='${w}'?'ALL':'${w}'); renderMainWorkspace()" class="${cls} p-3 cursor-pointer space-y-1.5">
                <div class="flex justify-between items-center"><strong class="text-[12.5px] text-white">${w}</strong><span class="code-id text-[11.5px]">${ratio}</span></div>
                <div class="h-1.5 bg-[#090e12] rounded overflow-hidden"><div style="width:${pct}%;height:100%;background:${col}"></div></div>
                <div class="text-[11px] text-[#8b96a5]">${sub}</div>
              </div>
            `,
              )
              .join("")}
          </div>

          <div class="so-panel overflow-hidden">
            <div class="px-3.5 py-2 bg-[#0e1418] border-b border-[#1c242c] flex justify-between items-center">
              <span class="text-[11.5px] font-semibold text-white">Environment Rollout Matrix (Desired vs. Reported &amp; Drift Count)</span>
              ${state.rmWaveFilter !== "ALL" ? `<button type="button" data-onclick="state.rmWaveFilter='ALL'; renderMainWorkspace()" class="so-btn so-btn-sm">Clear filter</button>` : ""}
            </div>
            <div class="so-table-scroll"><table class="so-table">
              <thead><tr><th>Environment</th><th>Wave</th><th>Channel</th><th>Connection</th><th>Desired</th><th>Reported</th><th style="text-align:right">Drift Count</th><th>Plan Status</th></tr></thead>
              <tbody>
                ${envs.map((r) => `<tr data-onclick="switchScreen('rm01-env-page')" class="cursor-pointer"><td class="code-id font-semibold text-white">${r.env}</td><td>${r.wave}</td><td class="code-id"><span class="so-pill code-id">${r.ch}</span></td><td>${pill(r.mode, "blue")}</td><td class="code-id">${r.desired}</td><td class="code-id font-semibold ${r.desired === r.reported ? "text-[#34d399]" : "text-[#eab854]"}">${r.reported}</td><td class="code-id" style="text-align:right">${r.drift > 0 ? pill(r.drift + " drift", "amber") : pill("0", "green")}</td><td>${pill(r.st, r.tone)}</td></tr>`).join("")}
              </tbody>
            </table></div>
          </div>
        </div>
      `;
  }

  function renderRM06PromotionPipe(root) {
    liveHub.renderPromotion(root);
    return;
    const sel =
      RM_CHANNELS.find((c) => c.id === state.rmSelectedChannel) ||
      RM_CHANNELS[3];
    root.innerHTML = `
        <div class="space-y-2.5 flex flex-col">
          ${rmApiStrip("PROPOSED", "POST /v1/release-channels/{channelId}/promote", "ConduitReleaseChannel")}
          <div class="so-panel p-3.5 space-y-3">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <div>
                <div class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Channel-to-Channel Promotion Pipeline &middot; wp-edge-telemetry</div>
                <div class="text-[12px] text-[#8b96a5] mt-0.5">Select any channel stage to inspect soak criteria, label gates, and manual promotion quorum.</div>
              </div>
              <button type="button" data-onclick="state.rmPromoDialogOpen=true; renderConduitModals()" class="so-btn so-btn-emerald">&rarr; Manual Promote to ${sel.name}</button>
            </div>

            ${state.rmPromotedNotice ? `<div class="so-opt-card p-2.5 code-id text-[11.5px] text-[#34d399]">&#10003; ${state.rmPromotedNotice}</div>` : ""}

            <div class="so-grid-4 gap-2.5">
              ${RM_CHANNELS.map(
                (ch, idx) => `
                <button type="button" data-onclick="state.rmSelectedChannel='${ch.id}'; renderMainWorkspace()"
                  class="${ch.state === "Upgrade Ready" ? "so-opt-card" : "so-card"} p-3 text-left cursor-pointer space-y-1.5"
                  style="box-shadow:${ch.id === sel.id ? "0 0 0 1.5px #5c9ce6" : "none"}">
                  <div class="flex justify-between items-center gap-1"><strong class="text-[13px] text-white">${idx + 1}. ${ch.name}</strong>${pill(ch.state, ch.tone)}</div>
                  <div class="code-id text-[12px] text-white pt-1">current: <strong>${ch.current}</strong></div>
                  <div class="code-id text-[11px] ${ch.candidate ? "text-[#eab854]" : "text-[#596573]"}">${ch.candidate ? "candidate: " + ch.candidate : "converged"}</div>
                  <div class="pt-1.5 border-t border-[#1c242c] flex justify-between text-[11px] text-[#8b96a5]"><span>${ch.envs} Envs</span><span class="code-id text-[#34d399]">${ch.health}</span></div>
                </button>
              `,
              ).join("")}
            </div>
          </div>

          <div class="so-grid-2 gap-3">
            <div class="so-panel p-3.5 space-y-2">
              <div class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Selected Stage Transition &middot; ${sel.upstream ? sel.upstream + " &rarr; " + sel.name : sel.name}</div>
              <div class="so-table-scroll"><table class="so-table" style="min-width:360px">
                <thead><tr><th>Criterion</th><th>Required</th><th>Observed</th><th>Verdict</th></tr></thead>
                <tbody>
                  <tr><td>Upstream Soak Duration</td><td class="code-id">&ge; ${sel.soakReq.toFixed(1)}h</td><td class="code-id">${sel.soakDone.toFixed(1)}h</td><td>${pill("Pass", "green")}</td></tr>
                  <tr><td>Healthy Convergence Ratio</td><td class="code-id">&ge; ${sel.healthReq}</td><td class="code-id">${sel.health}</td><td>${pill("Pass", "green")}</td></tr>
                  <tr><td>Required Release Labels</td><td class="code-id" colspan="2">${sel.labels.join(", ")}</td><td>${pill("Pass", "green")}</td></tr>
                </tbody>
              </table></div>
            </div>

            <div class="so-panel p-3.5 flex flex-col justify-between gap-3">
              <div class="space-y-2">
                <div class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Channel Governance &amp; Approvers</div>
                <div class="so-card p-2.5 text-[12px] text-[#8b96a5] space-y-1">
                  <div>Subscribed Environments: <strong class="code-id text-white">${sel.envs}</strong></div>
                  <div>Automated Promotion Window: <strong class="code-id text-white">Daily 02:00–05:00 UTC</strong></div>
                  <div>Manual Promotion Quorum: <strong class="code-id text-[#60a5fa]">2 @fleet-approvers</strong></div>
                </div>
              </div>
              <div class="flex justify-end gap-2">
                <button type="button" data-onclick="switchScreen('rm05-release-rollout')" class="so-btn">Inspect 0.31.2 Rollout</button>
                <button type="button" data-onclick="state.rmPromoDialogOpen=true; renderConduitModals()" class="so-btn so-btn-emerald">&rarr; Review Manual Promotion</button>
              </div>
            </div>
          </div>
        </div>
      `;
  }

  // Change request review: queue, detail, and a governed approve/reject.
  // The decision needs a reason and a confirm; the author cannot review
  // their own request, and the Hub enforces the same rule.
  function renderChangeReview(root) {
    if (!root) return;
    const review = (state.crReview ||= {
      filter: "all",
      selectedId: "",
      draft: { decision: "", rationale: "" },
      pending: false,
      error: "",
    });
    const rows = Array.isArray(state.hub?.changes) ? state.hub.changes : [];
    root.innerHTML = `<div data-cr-root>${reviewMarkup(rows, {
      ...review,
      operatorName: state.operatorName,
    })}</div>`;
    const host = root.querySelector("[data-cr-root]");
    if (!host) return;
    const rerender = () => renderChangeReview(root);
    const selected = () =>
      review.selectedId ||
      host.querySelector("[data-cr-select]")?.getAttribute("data-cr-select") ||
      "";
    host.addEventListener("input", (event) => {
      const target = event.target instanceof Element ? event.target : null;
      if (!target || !target.matches("[data-cr-rationale]")) return;
      review.draft = { decision: "", rationale: target.value };
      const ok = target.value.trim() !== "" && !review.pending;
      host.querySelectorAll("[data-cr-decide]").forEach((button) => {
        if (button.getAttribute("data-can-review") !== "0") button.disabled = !ok;
      });
    });
    host.addEventListener("click", async (event) => {
      const target = event.target instanceof Element ? event.target : null;
      if (!target) return;
      const filter = target.closest("[data-cr-filter]");
      if (filter) {
        review.filter = filter.getAttribute("data-cr-filter") || "all";
        review.selectedId = "";
        return rerender();
      }
      const pick = target.closest("[data-cr-select]");
      if (pick) {
        review.selectedId = pick.getAttribute("data-cr-select") || "";
        review.draft = { decision: "", rationale: "" };
        review.error = "";
        return rerender();
      }
      const decide = target.closest("[data-cr-decide]");
      if (decide && !decide.disabled) {
        review.selectedId = selected();
        review.draft = {
          ...review.draft,
          decision: decide.getAttribute("data-cr-decide") || "",
        };
        return rerender();
      }
      if (target.closest("[data-cr-cancel]")) {
        review.draft = { ...review.draft, decision: "" };
        return rerender();
      }
      if (target.closest("[data-cr-confirm]")) {
        const id = selected();
        const { decision, rationale } = review.draft;
        if (!id || !decision || !rationale.trim()) return;
        review.pending = true;
        review.error = "";
        rerender();
        try {
          await liveHub.reviewChange(id, decision, rationale.trim());
          review.draft = { decision: "", rationale: "" };
        } catch (error) {
          review.error = error instanceof Error ? error.message : String(error);
        }
        review.pending = false;
        rerender();
      }
    });
  }

  function renderRM07ChangeRequests(root) {
    renderChangeReview(root);
    return;
    const sel = RM_CRS.find((c) => c.id === state.rmSelectedCrId) || RM_CRS[0];
    const inc = state.rmIncludedCrs.includes(sel.id);
    const canInc = sel.status === "Approved" && !inc;
    const maxL = Math.max(sel.base.length, sel.next.length);

    root.innerHTML = `
        <div class="space-y-2.5 flex flex-col">
          ${rmApiStrip("PROPOSED", "POST /v1/change-requests/{id}/include-in-plan", "ConduitChangeRequest")}
          <div class="so-resp-stack flex gap-3 items-start">
            <aside class="so-resp-full so-panel w-[292px] shrink-0 p-2.5 space-y-2">
              <div class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5] px-1">Change Requests (${RM_CRS.length})</div>
              ${RM_CRS.map((cr) => {
                const isInc = state.rmIncludedCrs.includes(cr.id);
                return `
                  <button type="button" data-onclick="state.rmSelectedCrId='${cr.id}'; renderMainWorkspace()"
                    class="w-full text-left so-card so-stream-item ${isInc ? "" : cr.tone === "green" ? "so-stream-emerald" : "so-stream-amber"} p-2.5 space-y-1 cursor-pointer"
                    style="box-shadow:${cr.id === sel.id ? "0 0 0 1.5px #5c9ce6" : "none"}">
                    <div class="flex justify-between items-center"><strong class="code-id text-[12px] text-white">${cr.id}</strong>${pill(isInc ? "Included in Plan" : cr.status, isInc ? "blue" : cr.tone)}</div>
                    <div class="text-[11.5px] text-[#dce4ee] leading-snug">${cr.title}</div>
                    <div class="code-id flex justify-between text-[10.5px] text-[#596573]"><span>${cr.env}</span><span><span class="text-[#34d399]">+${cr.add}</span> / <span class="text-[#f87171]">-${cr.rem}</span></span></div>
                  </button>
                `;
              }).join("")}
            </aside>

            <section class="so-panel flex-1 min-w-0 p-3.5 space-y-3">
              <div class="flex flex-wrap items-start justify-between gap-2 pb-2.5 border-b border-[#1c242c]">
                <div>
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="code-id text-[15px] font-bold text-white">${sel.id}</span>
                    ${pill(inc ? "Included in Plan" : sel.status, inc ? "blue" : sel.tone)}
                    <span class="so-pill code-id">env: ${sel.env}</span>
                    <span class="so-pill code-id">author: ${sel.author}</span>
                  </div>
                  <div class="text-[13px] font-semibold text-white mt-1">${sel.title}</div>
                </div>
                ${governedBtn({
                  label: inc
                    ? "Included in PLN-2026-0418"
                    : "Include in plan (PLN-2026-0418)",
                  role: "Operator",
                  auditEvent: "change_request.included_in_plan",
                  onClickJs: `state.rmIncludedCrs.push('${sel.id}'); renderMainWorkspace()`,
                  cls: "so-btn so-btn-emerald",
                  blockedReason: inc
                    ? "Already included in PLN-2026-0418"
                    : !canInc
                      ? "Change request must be Approved before plan inclusion"
                      : "",
                })}
              </div>

              <div class="so-grid-2 border border-[#1c242c] rounded-[5px] overflow-hidden">
                <div class="bg-[#080c0f] overflow-x-auto">
                  <div class="px-3 py-1.5 bg-[#0e1418] border-b border-[#1c242c] text-[10px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Base Configuration (rev r41)</div>
                  <div class="py-2 code-id text-[11px] leading-relaxed">
                    ${Array.from({ length: maxL })
                      .map((_, i) => {
                        const b = sel.base[i] ?? "",
                          n = sel.next[i] ?? "",
                          ch = b !== n && b !== "";
                        return `<div style="padding:0 10px;white-space:pre;background:${ch ? "rgba(201,64,68,0.22)" : "transparent"};color:${ch ? "#fca5a5" : "#dce4ee"}"><span style="color:#596573;display:inline-block;width:24px">${String(i + 1).padStart(2, "0")}</span>${ch ? "- " : "  "}${b}</div>`;
                      })
                      .join("")}
                  </div>
                </div>
                <div class="bg-[#080c0f] border-l border-[#1c242c] overflow-x-auto">
                  <div class="px-3 py-1.5 bg-[#0e1418] border-b border-[#1c242c] text-[10px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Proposed Override (${sel.id})</div>
                  <div class="py-2 code-id text-[11px] leading-relaxed">
                    ${Array.from({ length: maxL })
                      .map((_, i) => {
                        const b = sel.base[i] ?? "",
                          n = sel.next[i] ?? "",
                          ch = b !== n && n !== "";
                        return `<div style="padding:0 10px;white-space:pre;background:${ch ? "rgba(38,142,91,0.24)" : "transparent"};color:${ch ? "#6ee7b7" : "#dce4ee"}"><span style="color:#596573;display:inline-block;width:24px">${String(i + 1).padStart(2, "0")}</span>${ch ? "+ " : "  "}${n}</div>`;
                      })
                      .join("")}
                  </div>
                </div>
              </div>
            </section>
          </div>
        </div>
      `;
  }

  function renderRM08BundlePreview(root) {
    liveHub.renderBundle(root);
    return;
    const entries = [
      [
        "images/uds-core-controller-0.31.2-amd64.oci.tar",
        "oci-image",
        "237.4 MB",
        "sha256:3b19a441...70e1",
      ],
      [
        "images/ingress-gateway-1.23.4-amd64.oci.tar",
        "oci-image",
        "185.2 MB",
        "sha256:6e21f088...91a0",
      ],
      [
        "charts/uds-core-0.31.2.tgz",
        "helm-chart",
        "4.0 MB",
        "sha256:88a04c12...4f19",
      ],
      [
        "crds/conduit-admission-v1.yaml",
        "crd-manifest",
        "0.3 MB",
        "sha256:51c09e77...02ab",
      ],
      [
        "locks/values-resolved-disconnected.yaml",
        "values-lock",
        "18.5 KB",
        "sha256:19f3d811...cc08",
      ],
      [
        "attestations/sbom-uds-core-0.31.2.spdx.json",
        "spdx-sbom",
        "1.8 MB",
        "sha256:77e01c12...99a4",
      ],
    ];
    root.innerHTML = `
        <div class="space-y-2.5 flex flex-col">
          ${rmApiStrip("REAL + PROPOSED", "Connect PackagingRunService/GetPackagingRun + GET /v1/bundles/{id}/preview", "ConduitArtifact")}
          <div class="so-opt-card p-3.5 flex flex-wrap items-center justify-between gap-3">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <span class="code-id text-[15.5px] font-bold text-white">uds-core-0.31.2-offline-bundle.tar.zst</span>
                ${pill("Signed", "green")}
                ${pill("Run: RUN-8842", "blue")}
                ${agentChip("Chunk Signer", 8, 1)}
              </div>
              <div class="text-[11.5px] text-[#8b96a5] mt-1">Target disconnected relays: <code class="code-id text-white">spoke-diode-hall-b</code>, <code class="code-id text-white">spoke-courier-north</code></div>
            </div>
            ${governedBtn({ label: "Download bundle (63.8 MB delta)", role: "Operator", auditEvent: "artifact.bundle_downloaded", onClickJs: "state.rmBundleDownloaded='Verified SHA-256 manifest and started 63.8 MB differential zstd transfer.'; renderMainWorkspace()", cls: "so-btn so-btn-emerald", iconName: "download" })}
          </div>

          ${state.rmBundleDownloaded ? `<div class="so-opt-card p-2.5 code-id text-[11.5px] text-[#34d399]">&#10003; ${state.rmBundleDownloaded}</div>` : ""}

          <div class="so-grid-3 gap-2.5">
            <div class="so-panel p-3.5 space-y-1">
              <div class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Transfer &amp; Full Archive Size</div>
              <div class="code-id text-[19px] font-bold text-[#60a5fa]">63.8 MB <span class="text-[11px] font-normal text-[#8b96a5]">zstd delta</span></div>
              <div class="text-[11.5px] text-[#8b96a5]">Full standalone archive: <strong class="code-id text-white">488.6 MB</strong><br/>Diode framing: <strong class="code-id text-[#34d399]">4 &times; 16 MB</strong> slices</div>
            </div>
            <div class="so-panel p-3.5 space-y-1.5">
              <div class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Cryptographic Signatures (2/2)</div>
              <div class="so-card p-2 flex justify-between items-center"><span class="code-id text-[11px]">prod-root-2026 (ECDSA-P256)</span>${pill("Verified", "green")}</div>
              <div class="so-card p-2 flex justify-between items-center"><span class="code-id text-[11px]">slsa-builder-02 (SLSA v1.0)</span>${pill("Verified", "green")}</div>
            </div>
            <div class="so-panel p-3.5 space-y-2">
              <div class="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">SBOM Summary (SPDX-2.3)</div>
              <div class="flex flex-wrap gap-1.5">${pill("0 Critical", "green")}${pill("0 High", "green")}<span class="so-pill code-id">2 Medium</span>${pill("418 Packages", "blue")}</div>
              <div class="text-[11.5px] text-[#8b96a5]">License policy check: <strong class="text-[#34d399]">PASS</strong></div>
            </div>
          </div>

          <div class="so-panel overflow-hidden">
            <div class="px-3.5 py-2 bg-[#0e1418] border-b border-[#1c242c] text-[11.5px] font-semibold text-white">Bundle Contents &amp; Digest Manifest (6 Entries)</div>
            <div class="so-table-scroll"><table class="so-table">
              <thead><tr><th>Archive Path</th><th>Kind</th><th style="text-align:right">Compressed Size</th><th>SHA-256 Digest</th></tr></thead>
              <tbody>
                ${entries.map((e) => `<tr><td class="code-id font-medium text-white">${e[0]}</td><td><span class="so-pill code-id">${e[1]}</span></td><td class="code-id" style="text-align:right">${e[2]}</td><td class="code-id text-[#8b96a5]">${e[3]}</td></tr>`).join("")}
              </tbody>
            </table></div>
          </div>
        </div>
      `;
  }

  // Command palette (kit v7.1): views, Hub objects and console actions.
  function paletteHits() {
    return filterPalette(
      paletteItems(SCREENS, state.hub),
      state.cmdOmnibarQuery,
      state.cmdCategory,
    );
  }

  function renderPaletteResults() {
    const host = document.getElementById("conduit-palette-results");
    if (!host) return;
    const hits = paletteHits();
    if (state.cmdSelectedIdx >= hits.length) state.cmdSelectedIdx = 0;
    host.innerHTML = paletteResultsMarkup(hits, {
      query: state.cmdOmnibarQuery,
      category: state.cmdCategory,
      selected: state.cmdSelectedIdx,
    });
    host
      .querySelector(".kit-palette-row.is-active")
      ?.scrollIntoView({ block: "nearest" });
  }

  function paletteInput(value) {
    state.cmdOmnibarQuery = String(value ?? "");
    state.cmdSelectedIdx = 0;
    renderPaletteResults();
  }

  function paletteCategory(category) {
    state.cmdCategory = category;
    state.cmdSelectedIdx = 0;
    renderPaletteResults();
    document.getElementById("conduit-omnibar-inp")?.focus();
  }

  function pickPaletteItem(idx) {
    const hit = paletteHits()[idx];
    if (!hit) return;
    state.cmdOmnibarOpen = false;
    renderConduitModals();
    const target = hit.target || {};
    if (target.action === "refresh") {
      liveHub.refresh();
      return;
    }
    if (target.action === "theme") {
      window.toggleTheme();
      return;
    }
    if (target.action === "settings") {
      window.toggleSettingsDialog();
      return;
    }
    if (target.env) {
      state.envDetailId = target.env;
      state.envDetailTab = "state";
    }
    if (target.change && state.crReview) state.crReview.selectedId = target.change;
    else if (target.change) {
      state.crReview = {
        filter: "all",
        selectedId: target.change,
        draft: { decision: "", rationale: "" },
        pending: false,
        error: "",
      };
    }
    if (target.screen) switchScreen(target.screen);
  }

  function handleOmnibarKey(ev) {
    const hits = paletteHits();
    if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
      ev.preventDefault();
      if (!hits.length) return;
      const step = ev.key === "ArrowDown" ? 1 : -1;
      state.cmdSelectedIdx =
        (state.cmdSelectedIdx + step + hits.length) % hits.length;
      renderPaletteResults();
    } else if (ev.key === "Enter") {
      ev.preventDefault();
      pickPaletteItem(Math.min(state.cmdSelectedIdx, hits.length - 1));
    }
  }

  function openCmdOmnibar() {
    state.cmdOmnibarOpen = true;
    state.cmdOmnibarQuery = "";
    state.cmdSelectedIdx = 0;
    state.cmdCategory = "All";
    renderConduitModals();
    setTimeout(() => {
      const el = document.getElementById("conduit-omnibar-inp");
      if (el) el.focus();
    }, 20);
  }

  function renderConduitModals() {
    const root = document.getElementById("conduit-modal-root");
    if (!root) return;
    const selCh =
      RM_CHANNELS.find((c) => c.id === state.rmSelectedChannel) ||
      RM_CHANNELS[3];
    let out = "";

    if (state.rmPromoDialogOpen) {
      out += `
          <div data-onclick="if(event.target===this){state.rmPromoDialogOpen=false;renderConduitModals();}"
               class="so-modal-backdrop"
               style="position:fixed;inset:0;background:rgba(6,10,13,0.62);z-index:85;display:flex;align-items:flex-end;justify-content:center;padding:24px 16px 32px;">
            <div role="dialog" aria-modal="true" aria-label="Manual Promotion Review"
                 class="so-panel so-modal-card w-full max-w-[556px] overflow-hidden shadow-2xl" style="border-color:#283644;">
              <div class="px-4 py-2.5 bg-[#0d1317] border-b border-[#1e262d] flex justify-between items-center">
                <strong class="text-[13.5px] text-white">Manual Promotion Review &middot; ${selCh.name}</strong>
                <button type="button" data-onclick="state.rmPromoDialogOpen=false;renderConduitModals()" class="so-btn so-btn-sm" aria-label="Close dialog">&times;</button>
              </div>
              <div class="p-4 space-y-3">
                <div class="so-opt-card p-2.5 flex justify-between items-center">
                  <span>Promoting <strong class="code-id text-white">uds-core 0.31.2</strong> &rarr; <strong class="code-id text-[#34d399]">${selCh.name}</strong></span>
                  ${pill("PROPOSED POST", "blue")}
                </div>
                <div class="space-y-1">
                  <div class="text-[10px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Promotion Strategy</div>
                  <select data-onchange="state.rmPromoStrategy=this.value" class="so-select w-full">
                    <option value="FAST_TRACK_TO_CHANNEL">Fast track to channel (${selCh.name})</option>
                    <option value="COPY_EXISTING">Copy release across lagging channels</option>
                    <option value="CUSTOM_CHANNELS">Custom target channel set</option>
                  </select>
                </div>
                <div class="space-y-1.5">
                  <div class="text-[10px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Requirements &amp; Approvers</div>
                  <div class="so-card p-2 flex justify-between items-center text-[11.5px]"><span>Upstream soak (${selCh.soakDone}h &ge; ${selCh.soakReq}h) &amp; health (${selCh.health})</span>${pill("Pass", "green")}</div>
                  <div class="so-card p-2 flex justify-between items-center text-[11.5px]"><span>Label selector (${selCh.labels.join(", ")})</span>${pill("Pass", "green")}</div>
                  <div class="so-card p-2 flex justify-between items-center text-[11.5px]"><span>Required approvers (@fleet-approvers: M. Chen 1/2 + J. Alvarez)</span>${pill("Approved", "green")}</div>
                </div>
                <div class="space-y-1">
                  <div class="text-[10px] font-semibold uppercase tracking-[0.08em] text-[#8b96a5]">Audit Justification</div>
                  <textarea rows="2" data-oninput="state.rmPromoNote=this.value" class="w-full p-2 rounded bg-[#090e12] border border-[#1f2933] text-[11.5px] text-white">${state.rmPromoNote}</textarea>
                </div>
              </div>
              <div class="px-4 py-2.5 bg-[#0d1317] border-t border-[#1e262d] flex justify-end gap-2">
                <button type="button" data-onclick="state.rmPromoDialogOpen=false;renderConduitModals()" class="so-btn">Cancel</button>
                <button type="button" data-onclick="state.rmPromoDialogOpen=false; state.rmPromotedNotice='Promoted uds-core 0.31.2 to ${selCh.name} (Audit event recorded).'; renderConduitModals(); renderMainWorkspace();" class="so-btn so-btn-emerald">Approve &amp; Promote Release</button>
              </div>
            </div>
          </div>
        `;
    }

    if (state.cmdOmnibarOpen) {
      out += `
          <div data-onclick="if(event.target===this){state.cmdOmnibarOpen=false;renderConduitModals();}"
               class="so-modal-backdrop"
               style="position:fixed;inset:0;background:rgba(6,10,13,0.62);z-index:90;display:flex;align-items:flex-start;justify-content:center;padding:68px 16px 24px;">
            <div role="dialog" aria-modal="true" aria-label="Command palette" class="so-panel so-modal-card kit-palette w-full max-w-[600px] overflow-hidden shadow-2xl">
              <div class="p-2.5 border-b border-[#1e262d]">
                <input id="conduit-omnibar-inp" type="search" value="${escAttr(state.cmdOmnibarQuery)}"
                  placeholder="Search views, environments, products, runs, change requests, actions"
                  aria-label="Search" aria-controls="conduit-palette-results"
                  data-onkeydown="handleOmnibarKey(event)"
                  data-oninput="paletteInput(this.value)"
                  class="so-input w-full !h-[32px]" />
              </div>
              <div id="conduit-palette-results" class="p-1.5"></div>
            </div>
          </div>
        `;
    }

    root.innerHTML = out;
    if (state.cmdOmnibarOpen) renderPaletteResults();
  }

  let previousScreen = "";
  function renderMainWorkspace() {
    const root = document.getElementById("main-workspace");
    if (!root) return;
    if (AREAS[state.screen]) {
      renderArea(root);
      updateExampleBadge();
      settings.applyChrome();
      settings.ensureLoaded();
      return;
    }
    const screenChanged =
      previousScreen !== "" && previousScreen !== state.screen;
    previousScreen = state.screen;
    const m = {
      "s01-intake": renderS01Intake,
      "s02-workspaces": renderS02Workspaces,
      "s03-saved-views": renderS03SavedViews,
      "s04-scope-plan": renderS04ScopePlan,
      "s05-backlog": renderS05Backlog,
      "s06-active-work": renderS06ActiveWork,
      "s07-pipeline": renderS07Pipeline,
      "s08-staged-run": renderS08StagedRun,
      "au01-audit": renderAuditLedger,
      "s09-source": renderS09Source,
      "s10-diff": renderS10Diff,
      "s11-preview": renderS11Preview,
      "s12-lifecycle": renderS12Lifecycle,
      "s13-fleet-overview": renderS13FleetOverview,
      "s14-env-detail": renderS14EnvDetail,
      "s15-plan-detail": renderS15PlanDetail,
      "s16-approvals-board": renderS16ApprovalsBoard,
      "s17-spokes-audit": renderS17SpokesAudit,
      "s18-components": renderS18Components,
      "s19-ontology": renderS19Ontology,
      "rm01-env-page": renderRM01EnvPage,
      "rm02-env-activity": renderRM02EnvActivity,
      "rm03-plan-detail": renderRM03PlanDetail,
      "rm04-constraints": renderRM04Constraints,
      "rm05-release-rollout": renderRM05ReleaseRollout,
      "rm06-promotion-pipe": renderRM06PromotionPipe,
      "rm07-change-requests": renderRM07ChangeRequests,
      "rm08-bundle-preview": renderRM08BundlePreview,
      "ai01-logic": (root) => intelligence.renderLogic(root),
      "ai02-evals": (root) => intelligence.renderEvals(root),
      "ai03-events": (root) => intelligence.renderEvents(root),
      "ai04-courses": (root) => intelligence.renderCourses(root),
      ...releaseGates.renderers,
      ...ontologyGraph.renderers,
      ...settings.renderers,
    };
    (m[state.screen] || renderS13FleetOverview)(root);
    updateExampleBadge();
    settings.applyChrome();
    settings.ensureLoaded();
    if (screenChanged && root.firstElementChild) {
      root.scrollTop = 0;
      root.firstElementChild.classList.add("so-view-enter");
    }
  }

  function renderArea(root) {
    const area = AREAS[state.screen];
    const tabs = area.tabs;
    const current = tabs.find((tab) => tab.id === state.areaTab) || tabs[0];
    state.areaTab = current.id;
    state.detailScreen = current.screen;
    const header = objectHeader(areaObject(state.hub, state.screen));
    const extra = areaSupplement(state.hub, state.screen, current.id);
    root.innerHTML = `<div class="space-y-3">
      <div class="kit-crumb">${area.label} / ${current.label}</div>
      <div class="kit-subnav">${tabs
        .map(
          (tab) =>
            `<button type="button" data-area-tab="${tab.id}" class="kit-tab ${tab.id === current.id ? "is-active" : ""}">${tab.label}</button>`,
        )
        .join("")}</div>
      ${header}
      <div id="area-kit">${extra}</div>
      <div id="area-body"></div>
    </div>`;
    const renderer = {
      "s08-staged-run": renderS08StagedRun,
      "rm08-bundle-preview": renderRM08BundlePreview,
      "pt01-proposal-trace": releaseGates.renderers["pt01-proposal-trace"],
      "s14-env-detail": renderS14EnvDetail,
      "rm-channels": (node) => liveHub.renderChannels(node),
      "flt-environments": (node) => {
        const selected = state.envFacets || (state.envFacets = {});
        const paint = () => {
          if (state.envDetailId) {
            const row = (state.hub?.environments || []).find(
              (item) => item?.environment?.id === state.envDetailId,
            );
            node.innerHTML = envDetailMarkup(row, state.hub?.installations, {
              tab: state.envDetailTab,
            });
            return;
          }
          node.innerHTML = facetsMarkup(state.hub?.environments, selected, {
            note: state.hub?.environmentsNote || "",
          });
        };
        paint();
        node.onclick = (event) => {
          const target = event.target instanceof Element ? event.target : null;
          const open = target?.closest("[data-env-open]");
          if (open) {
            state.envDetailId = open.getAttribute("data-env-open");
            state.envDetailTab = "state";
            paint();
            return;
          }
          if (target?.closest("[data-env-back]")) {
            state.envDetailId = "";
            paint();
            return;
          }
          const envTab = target?.closest("[data-env-tab]");
          if (envTab) {
            state.envDetailTab = envTab.getAttribute("data-env-tab");
            paint();
            return;
          }
          if (target?.closest("[data-facet-clear]")) {
            for (const key of Object.keys(selected)) delete selected[key];
            paint();
            return;
          }
          const row = target?.closest("[data-facet]");
          if (!row) return;
          const key = row.getAttribute("data-facet");
          const value = row.getAttribute("data-facet-value");
          if (selected[key] === value) delete selected[key];
          else selected[key] = value;
          paint();
        };
      },
      "rel-products": (node) => {
        node.innerHTML = productsMarkup(
          productRows(state.hub?.products, state.hub?.installations),
          { note: state.hub?.productsNote || "" },
        );
      },
      "cc-overview": (node) => {
        node.innerHTML = overviewMarkup(state.hub);
      },
      "pkg-runs": (node) => {
        node.innerHTML = runsMarkup(
          state.hub?.packaging,
          state.hub?.packagingEventsByRun,
          { note: state.hub?.packagingNote || "" },
        );
        node.querySelectorAll("[data-open-run]").forEach((button) => {
          button.addEventListener("click", () => switchScreen("s08-staged-run"));
        });
      },
      "rm-waves": (node) => liveHub.renderWaves(node),
      "rm02-env-activity": renderRM02EnvActivity,
      "rm06-promotion-pipe": renderRM06PromotionPipe,
      "rm09-recalls": releaseGates.renderers["rm09-recalls"],
      "rm07-change-requests": renderRM07ChangeRequests,
      "s16-approvals-board": renderS16ApprovalsBoard,
      "ai01-logic": (node) => intelligence.renderLogic(node),
      "ai02-evals": (node) => intelligence.renderEvals(node),
      "ai04-courses": (node) => intelligence.renderCourses(node),
      "ai03-events": (node) => intelligence.renderEvents(node),
      "s13-fleet-overview": renderS13FleetOverview,
      "s15-plan-detail": renderS15PlanDetail,
      "s17-spokes-audit": renderS17SpokesAudit,
      "au01-audit": renderAuditLedger,
    }[current.screen];
    if (renderer) renderer(root.querySelector("#area-body"));
    root.querySelectorAll("[data-area-tab]").forEach((button) => {
      button.addEventListener("click", () => {
        state.areaTab = button.getAttribute("data-area-tab");
        renderMainWorkspace();
      });
    });
    const waves = wavesFromRecords(state.hub?.promotions);
    bindKit(root, waves.pucks.length ? { pucks: waves.pucks } : null);
  }

  function renderApp() {
    const mapped = areaFor(state.screen);
    if (mapped) {
      state.screen = mapped.area;
      state.areaTab = mapped.tab;
    }
    liveHub.ensure();
    renderLeftRail();
    renderCenterTabStrip();
    renderMainWorkspace();
    renderChatRail();
    renderConduitModals();
    if (typeof notifyOperator === "function") notifyOperator();
  }

  const settingsDialog = createSettingsDialog({
    getRoot: () => document.querySelector(".operator-root"),
    getOperatorName: () => state.operatorName,
    onDockReset: () => renderChatRail(),
  });
  window.toggleSettingsDialog = () => settingsDialog.toggle();

  const onKey = (ev) => {
    if (settingsDialog.onKey(ev)) return;
    if (releaseGates.onKey(ev)) return;
    if ((ev.metaKey || ev.ctrlKey) && ev.key.toLowerCase() === "k") {
      ev.preventDefault();
      openCmdOmnibar();
    } else if (ev.key === "Escape") {
      state.cmdOmnibarOpen = false;
      state.rmPromoDialogOpen = false;
      renderConduitModals();
      if (typeof notifyOperator === "function") notifyOperator();
    }
  };
  window.addEventListener("keydown", onKey);
  const onHash = () => {
    const id = (location.hash || "").replace(/^#/, "");
    const mapped = areaFor(id);
    if (!mapped && !AREAS[id] && !SCREENS.some((s) => s.id === id)) return;
    if (mapped) {
      state.screen = mapped.area;
      state.areaTab = mapped.tab;
    } else if (AREAS[id]) {
      state.screen = id;
      state.areaTab = AREAS[id].tabs[0].id;
    } else if (state.screen !== id) {
      state.screen = id;
    } else return;
    renderApp();
  };
  window.addEventListener("hashchange", onHash);
  const initialHash = (location.hash || "").replace(/^#/, "");
  if (SCREENS.some((s) => s.id === initialHash)) state.screen = initialHash;
  window.switchScreen = switchScreen;
  window.closeTab = closeTab;
  window.toggleMobileNav = toggleMobileNav;
  window.closeMobileNav = closeMobileNav;
  window.toggleLeftRailCollapse = toggleLeftRailCollapse;
  window.toggleChatRail = toggleChatRail;
  window.refreshHub = () => liveHub.refresh();
  window.toggleTheme = () => {
    const root = document.querySelector(".operator-root");
    if (!root) return;
    const next = root.getAttribute("data-theme") === "light" ? "dark" : "light";
    root.setAttribute("data-theme", next);
    writePref(THEME_KEY, next);
    const button = document.getElementById("theme-toggle");
    if (button) button.textContent = next === "light" ? "Dark" : "Light";
  };
  window.handleGlobalSearch = handleGlobalSearch;
  window.openCmdOmnibar = openCmdOmnibar;
  window.handleOmnibarKey = handleOmnibarKey;
  window.paletteInput = paletteInput;
  window.paletteCategory = paletteCategory;
  window.pickPaletteItem = pickPaletteItem;
  window.renderConduitModals = renderConduitModals;
  window.renderMainWorkspace = renderMainWorkspace;
  window.showTip = showTip;
  window.hideTip = hideTip;
  window.recordAudit = recordAudit;
  window.copyRmRawLog = copyRmRawLog;
  window.toggleSavedViewPin = (id) => {
    const view = state.savedViews.find((item) => item.id === id);
    if (view) view.pinned = !view.pinned;
    renderApp();
  };
  window.removeSavedView = (id) => {
    state.savedViews = state.savedViews.filter((item) => item.id !== id);
    renderApp();
  };
  window.state = state;
  const removeInlineActions = installInlineActions(document);
  releaseGates.bind(window);
  ontologyGraph.bind(window);
  settings.bind(window);
  settingsDialog.restore();
  renderApp();
  return () => {
    removeInlineActions();
    window.removeEventListener("keydown", onKey);
    window.removeEventListener("hashchange", onHash);
  };
}
