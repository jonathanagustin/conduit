"use client";

import type { ReactNode } from "react";
import { FileTextIcon } from "lucide-react";

import { ActivityGraph } from "@/components/assistant-ui/elements/activity-graph";
import {
  AgentCard,
  type AgentSkill,
} from "@/components/assistant-ui/elements/agent-card";
import { AgentHandoff } from "@/components/assistant-ui/elements/agent-handoff";
import { AgentPlan } from "@/components/assistant-ui/elements/agent-plan";
import { AgentStatus } from "@/components/assistant-ui/elements/agent-status";
import { ApprovalCard } from "@/components/assistant-ui/elements/approval-card";
import { ArtifactCard } from "@/components/assistant-ui/elements/artifact-card";
import {
  BackgroundInbox,
  type BackgroundRun,
} from "@/components/assistant-ui/elements/background-inbox";
import {
  CanvasSplit,
  CanvasSplitThread,
  CanvasSplitMessage,
  CanvasSplitDocument,
  CanvasSplitHeader,
  CanvasSplitBody,
  CanvasSplitLine,
} from "@/components/assistant-ui/elements/canvas-split";
import { Chart } from "@/components/assistant-ui/elements/chart";
import {
  ChatPanel,
  ChatPanelMessages,
  ChatPanelUserMessage,
  ChatPanelAssistantMessage,
  ChatPanelComposer,
} from "@/components/assistant-ui/elements/chat-panel";
import {
  CheckpointHistory,
  type Checkpoint,
} from "@/components/assistant-ui/elements/checkpoint-history";
import {
  CodeDiff,
  type DiffLine,
} from "@/components/assistant-ui/elements/code-diff";
import { CodeRunner } from "@/components/assistant-ui/elements/code-runner";
import {
  CommandPalette,
  type PaletteCommand,
} from "@/components/assistant-ui/elements/command-palette";
import {
  ComparisonCard,
  type ComparisonOption,
} from "@/components/assistant-ui/elements/comparison-card";
import {
  Composer,
  ComposerBar,
  ComposerInput,
  ComposerSend,
  ComposerCommandItem,
  type ComposerCommand,
  ComposerPersonItem,
  type ComposerPerson,
  ComposerAttachmentChip,
  type ComposerAttachment,
  ComposerModelTrigger,
  ComposerModelItem,
  type ComposerModel,
  ComposerVoice,
  ComposerVoiceButton,
  ComposerContext,
  type ComposerUsage,
} from "@/components/assistant-ui/elements/composer";
import {
  ComputerUse,
  type ComputerStep,
} from "@/components/assistant-ui/elements/computer-use";
import {
  ConfidenceMarker,
  type ConfidenceClaim,
} from "@/components/assistant-ui/elements/confidence-marker";
import { ConnectionState } from "@/components/assistant-ui/elements/connection-state";
import {
  ContextBreakdown,
  type ContextSegment,
} from "@/components/assistant-ui/elements/context-breakdown";
import {
  ConversationSearch,
  type SearchHit,
} from "@/components/assistant-ui/elements/conversation-search";
import {
  CostMeter,
  type CostLine,
} from "@/components/assistant-ui/elements/cost-meter";
import { DataTable } from "@/components/assistant-ui/elements/data-table";
import {
  DaySeparator,
  type DatedMessage,
} from "@/components/assistant-ui/elements/day-separator";
import { Diagram } from "@/components/assistant-ui/elements/diagram";
import {
  DocumentReference,
  type DocumentAnchor,
} from "@/components/assistant-ui/elements/document-reference";
import { DraftRestore } from "@/components/assistant-ui/elements/draft-restore";
import { EditMessage } from "@/components/assistant-ui/elements/edit-message";
import {
  ElicitationForm,
  type ElicitationField,
} from "@/components/assistant-ui/elements/elicitation-form";
import {
  EmptyState,
  EmptyStateGreeting,
  EmptyStateSuggestions,
  EmptyStateSuggestion,
  EmptyStateComposer,
} from "@/components/assistant-ui/elements/empty-state";
import { ErrorState } from "@/components/assistant-ui/elements/error-state";
import { FeedbackDialog } from "@/components/assistant-ui/elements/feedback-dialog";
import {
  FileTree,
  type FileTreeNode,
} from "@/components/assistant-ui/elements/file-tree";
import {
  FlowGraph,
  type FlowNode,
  type FlowEdge,
} from "@/components/assistant-ui/elements/flow-graph";
import { GuardrailNotice } from "@/components/assistant-ui/elements/guardrail-notice";
import { ImageGeneration } from "@/components/assistant-ui/elements/image-generation";
import {
  Citation,
  type Source,
} from "@/components/assistant-ui/elements/inline-citation";
import {
  JobProgress,
  type JobStage,
} from "@/components/assistant-ui/elements/job-progress";
import { LauncherBubble } from "@/components/assistant-ui/elements/launcher-bubble";
import { GenerationLoader } from "@/components/assistant-ui/elements/loading-state";
import {
  MapAnswer,
  type MapPin,
} from "@/components/assistant-ui/elements/map-answer";
import {
  MathBlock,
  type MathStep,
} from "@/components/assistant-ui/elements/math-block";
import {
  McpServerPanel,
  type McpServer,
} from "@/components/assistant-ui/elements/mcp-server-panel";
import {
  MemoryChips,
  type MemoryChip,
} from "@/components/assistant-ui/elements/memory-chips";
import { MessageActions } from "@/components/assistant-ui/elements/message-actions";
import {
  MessageAttachments,
  type MessageAttachmentItem,
} from "@/components/assistant-ui/elements/message-attachment";
import { MessageBranches } from "@/components/assistant-ui/elements/message-branches";
import { MessagePair } from "@/components/assistant-ui/elements/message-pair";
import {
  MessageQueue,
  type QueuedMessage,
} from "@/components/assistant-ui/elements/message-queue";
import {
  MessageTiming,
  type TimingStat,
} from "@/components/assistant-ui/elements/message-timing";
import { MobileComposer } from "@/components/assistant-ui/elements/mobile-composer";
import {
  ModelPicker,
  type PickableModel,
} from "@/components/assistant-ui/elements/model-picker";
import { NumberTicker } from "@/components/assistant-ui/elements/number-ticker";
import {
  Onboarding,
  type OnboardingStep,
} from "@/components/assistant-ui/elements/onboarding";
import { PermissionGrant } from "@/components/assistant-ui/elements/permission-grant";
import {
  PromptLibrary,
  type SavedPrompt,
} from "@/components/assistant-ui/elements/prompt-library";
import { QuotaBanner } from "@/components/assistant-ui/elements/quota-banner";
import {
  QuoteReply,
  type QuoteAction,
} from "@/components/assistant-ui/elements/quote-reply";
import { ReadAloud } from "@/components/assistant-ui/elements/read-aloud";
import {
  ReasoningEffort,
  type EffortLevel,
} from "@/components/assistant-ui/elements/reasoning-effort";
import {
  ReasoningPanel,
  type ReasoningStep,
} from "@/components/assistant-ui/elements/reasoning-panel";
import { RecommendationCard } from "@/components/assistant-ui/elements/recommendation-card";
import {
  RegenerateMenu,
  type RegenerateOption,
} from "@/components/assistant-ui/elements/regenerate-menu";
import {
  ResearchReport,
  type ReportSection,
} from "@/components/assistant-ui/elements/research-report";
import {
  RetrievalChunks,
  type RetrievalChunk,
} from "@/components/assistant-ui/elements/retrieval-chunks";
import {
  ReviewableDiff,
  type DiffHunk,
} from "@/components/assistant-ui/elements/reviewable-diff";
import {
  ScheduleCard,
  type ScheduleRun,
} from "@/components/assistant-ui/elements/schedule-card";
import {
  ScoreBreakdown,
  type ScoreCriterion,
} from "@/components/assistant-ui/elements/score-breakdown";
import {
  ScrollAnchor,
  type ScrollAnchorMessage,
} from "@/components/assistant-ui/elements/scroll-anchor";
import {
  SettingsPanel,
  type SettingToggle,
} from "@/components/assistant-ui/elements/settings-panel";
import {
  SharedConversation,
  type SharedTurn,
} from "@/components/assistant-ui/elements/shared-conversation";
import {
  Sources,
  type Source as Source_e498,
} from "@/components/assistant-ui/elements/sources";
import {
  SpeakerIdentity,
  type SpeakerTurn,
} from "@/components/assistant-ui/elements/speaker-identity";
import {
  SpecSheet,
  type SpecRow,
} from "@/components/assistant-ui/elements/spec-sheet";
import { StoppedRun } from "@/components/assistant-ui/elements/stopped-run";
import {
  StreamingText,
  type Segment,
} from "@/components/assistant-ui/elements/streaming-text";
import {
  SubagentList,
  type SubagentItem,
} from "@/components/assistant-ui/elements/subagent-list";
import { Suggestions } from "@/components/assistant-ui/elements/suggestions";
import { TerminalBlock } from "@/components/assistant-ui/elements/terminal-block";
import { ThinkingIndicator } from "@/components/assistant-ui/elements/thinking-indicator";
import {
  ThreadSearch,
  type SearchableThread,
} from "@/components/assistant-ui/elements/thread-search";
import {
  Timeline,
  type TimelineEvent,
} from "@/components/assistant-ui/elements/timeline";
import {
  TodoList,
  type TodoItem,
} from "@/components/assistant-ui/elements/todo-list";
import { ToolCall } from "@/components/assistant-ui/elements/tool-call";
import { ToolError } from "@/components/assistant-ui/elements/tool-error";
import {
  ToolGroup,
  type GroupedTool,
} from "@/components/assistant-ui/elements/tool-group";
import {
  ToolTimeline,
  type TimelineStep,
  type TimelineStat,
} from "@/components/assistant-ui/elements/tool-timeline";
import {
  TraceWaterfall,
  type TraceSpan,
} from "@/components/assistant-ui/elements/trace-waterfall";
import { TypingIndicator } from "@/components/assistant-ui/elements/typing-indicator";
import {
  VoiceConversation,
  type VoiceTurn,
} from "@/components/assistant-ui/elements/voice-conversation";
import { WebPreview } from "@/components/assistant-ui/elements/web-preview";
import {
  WebSearch,
  type WebSearchResult,
} from "@/components/assistant-ui/elements/web-search";

const noop = () => undefined;
const accept = noop;
const allowOnce = noop;
const alwaysAllow = noop;
const apply = noop;
const authorize = noop;
const cancelEdit = noop;
const cancelJob = noop;
const closeDoc = noop;
const collectRun = noop;
const connect = noop;
const continueThread = noop;
const copyDoc = noop;
const decline = noop;
const deny = noop;
const discard = noop;
const discardDraft = noop;
const endCall = noop;
const expand = noop;
const forget = noop;
const grantScope = noop;
const handleAction = noop;
const handleCopy = noop;
const handleFocus = noop;
const handleRegenerate = noop;
const handleSettled = noop;
const handleSuggestion = noop;
const insertPrompt = noop;
const interrupt = noop;
const jumpToPage = noop;
const keep = noop;
const nextStep = noop;
const openArtifact = noop;
const openAttachmentPicker = noop;
const openExternal = noop;
const openMenu = noop;
const openViewer = noop;
const reaction = noop;
const regenerateWith = noop;
const reload = noop;
const removeAttachment = noop;
const removeFromQueue = noop;
const resetZoom = noop;
const restoreCheckpoint = noop;
const restoreDraft = noop;
const resume = noop;
const retry = noop;
const retryConnection = noop;
const run = noop;
const runCommand = noop;
const runQuickAction = noop;
const saveEdit = noop;
const selectModel = noop;
const selectPin = noop;
const selectPrompt = noop;
const selectThread = noop;
const send = noop;
const sendMessage = noop;
const sendPrompt = noop;
const setActiveId = noop;
const setDraft = noop;
const setEditing = noop;
const setExpandedId = noop;
const setHoveredId = noop;
const setIndex = noop;
const setKey = noop;
const setModel = noop;
const setNote = noop;
const setOpen = noop;
const setOpenIndex = noop;
const setQuery = noop;
const setReaction = noop;
const setSystemPrompt = noop;
const setTemperature = noop;
const showAlternatives = noop;
const skip = noop;
const skipOnboarding = noop;
const startConversation = noop;
const stepHit = noop;
const stopRun = noop;
const submitFeedback = noop;
const toggleMute = noop;
const toggleOpen = noop;
const togglePlayback = noop;
const toggleReason = noop;
const toggleSchedule = noop;
const toggleSetting = noop;
const upgrade = noop;
const zoomIn = noop;
const zoomOut = noop;

const tick = 4;
const open = true;
const query = "";
const draft = "Can you also check the tests?";
const active = false;
const selected = false;
const streaming = false;
const recording = false;
const generating = false;
const copied = false;
const editing = false;
const running = false;
const dragActive = false;
const keyboardOpen = false;
const zoom = 1;
const cycle = 0;
const cycleRate = 1;
const note = "";
const quoted = "the quoted sentence";
const systemPrompt = "You are a demo assistant.";
const previewUrl = "https://example.com";
const activeId = "1";
const hoveredId = "1";
const expandedId = "1";
const openIndex = 0;
const FileIcon = () => null;

function Demo0() {
  return <GenerationLoader label="Thinking" tick={tick} variant="dots" />;
}

function Demo1() {
  return <ThinkingIndicator label="Searching the codebase" elapsed="4s" />;
}

function Demo2() {
  const segments: Segment[] = [
    { text: "Reading the schema, then writing the migration." },
  ];

  return <StreamingText segments={segments} count={4} streaming />;
}

function Demo3() {
  return <TypingIndicator variant="bubble" />;
}

function Demo4() {
  const levels: EffortLevel[] = [
    { key: "low", label: "Low", budget: 2000 },
    { key: "high", label: "High", budget: 20000 },
  ];

  return (
    <ReasoningEffort
      levels={levels}
      selectedKey="high"
      spent={9500}
      onSelect={setKey}
    />
  );
}

function Demo5() {
  return (
    <GuardrailNotice
      title="Can't help with that"
      explanation="This request falls outside what I can assist with."
      policy="content-policy"
      alternatives={["Ask about something else", "Rephrase the request"]}
      onPick={() => undefined}
    />
  );
}

function Demo6() {
  const steps: ReasoningStep[] = [
    { title: "Reading the request", body: "Working out what changed." },
  ];

  return (
    <ReasoningPanel
      steps={steps}
      visibleSteps={steps.length}
      streaming={false}
      open={open}
      onOpenChange={setOpen}
      restingLabel="Thought for 4s"
    />
  );
}

function Demo7() {
  return (
    <MessagePair
      userMessage="Summarize this PR"
      words={["It", "renames", "the", "config", "key."]}
      visibleWords={3}
      streaming
      variant="bubble"
    />
  );
}

function Demo8() {
  return (
    <MessageBranches
      variants={["First answer", "Second answer"]}
      index={0}
      onIndexChange={setIndex}
    />
  );
}

function Demo9() {
  return (
    <MessageActions
      copied={copied}
      reaction={null}
      regenerating={false}
      onCopy={handleCopy}
      onReactionChange={setReaction}
      onRegenerate={handleRegenerate}
      onMore={openMenu}
    />
  );
}

function Demo10() {
  return (
    <ErrorState
      title="Something went wrong"
      detail="The request timed out after 30 seconds."
      retrying={false}
      onRetry={retry}
    />
  );
}

function Demo11() {
  const queued: QueuedMessage[] = [{ id: "1", text: "Also check the tests" }];

  return (
    <MessageQueue
      running="Reading the schema..."
      queued={queued}
      onCancel={removeFromQueue}
    />
  );
}

function Demo12() {
  return (
    <EditMessage
      value={draft}
      discardedReplies={2}
      editing={editing}
      onValueChange={setDraft}
      onSave={saveEdit}
      onCancel={cancelEdit}
      onStartEdit={() => undefined}
    />
  );
}

function Demo13() {
  return (
    <FeedbackDialog
      reasons={["Inaccurate", "Unhelpful", "Unsafe"]}
      selected={[]}
      note={note}
      sent={false}
      onToggleReason={toggleReason}
      onNoteChange={setNote}
      onSubmit={submitFeedback}
    />
  );
}

function Demo14() {
  return (
    <StoppedRun
      words={["The", "migration", "adds"]}
      reason="Stopped by user"
      onContinue={resume}
      onDiscard={discard}
    />
  );
}

function Demo15() {
  const messages: DatedMessage[] = [
    {
      id: "1",
      day: "Today",
      time: "9:41 AM",
      role: "user",
      text: "Can you review this?",
    },
  ];

  return <DaySeparator messages={messages} />;
}

function Demo16() {
  const turns: SpeakerTurn[] = [
    {
      id: "1",
      kind: "subagent",
      name: "Search agent",
      detail: "gpt-5.6-luna",
      text: "Found 3 matches.",
    },
  ];

  return <SpeakerIdentity turns={turns} />;
}

function Demo17() {
  const options: RegenerateOption[] = [
    { id: "sonnet", label: "Claude Sonnet", detail: "balanced" },
  ];

  return (
    <RegenerateMenu
      options={options}
      open={open}
      currentId="sonnet"
      onOpenChange={setOpen}
      onPick={regenerateWith}
    />
  );
}

function Demo18() {
  const claims: ConfidenceClaim[] = [
    {
      id: "1",
      text: "The API returns JSON",
      confidence: "grounded",
      basis: "docs.example.com",
    },
  ];

  return (
    <ConfidenceMarker
      claims={claims}
      hoveredId={hoveredId}
      onHover={setHoveredId}
    />
  );
}

function Demo19() {
  const attachments: MessageAttachmentItem[] = [
    { id: "1", name: "spec.pdf", size: "340 KB", kind: "document", pages: 12 },
  ];

  return <MessageAttachments attachments={attachments} onOpen={openViewer} />;
}

function Demo20() {
  const actions: QuoteAction[] = [
    { key: "quote", label: "Quote", icon: "quote" },
  ];

  return (
    <QuoteReply
      before="The treaty was signed in "
      selection="1648"
      after=", ending the war."
      actions={actions}
      toolbarVisible
      quoted={quoted}
      onAction={handleAction}
    />
  );
}

function Demo21() {
  const stats: TimingStat[] = [
    { label: "ttft", value: "0.4s" },
    { label: "total", value: "2.6s" },
  ];

  return <MessageTiming stats={stats} streaming={false} />;
}

function Demo22() {
  return (
    <ToolCall
      label="Ran search"
      activeLabel="Searching..."
      query="assistant-ui elements"
      request='{"query":"assistant-ui elements"}'
      result="12 matches"
      running={false}
      open={open}
      onOpenChange={setOpen}
    />
  );
}

function Demo23() {
  const steps: TimelineStep[] = [
    { verb: "Read", chip: "src/index.ts", icon: FileTextIcon },
  ];
  const stats: TimelineStat[] = [
    { file: "src/index.ts", added: 4, removed: 1 },
  ];

  return (
    <ToolTimeline
      steps={steps}
      visibleSteps={steps.length}
      streaming={false}
      open={open}
      onOpenChange={setOpen}
      restingLabel="Ran 1 step"
      activeLabel="Working..."
      stats={stats}
    />
  );
}

function Demo24() {
  return (
    <TerminalBlock
      command="pnpm test"
      lines={["PASS src/index.test.ts", "5 passed, 0 failed"]}
      visibleCount={2}
      done
      variant="ink"
    />
  );
}

function Demo25() {
  const lines: DiffLine[] = [
    { kind: "removed", text: "-const x = 1;" },
    { kind: "added", text: "+const x = 2;" },
  ];

  return (
    <CodeDiff
      filename="src/index.ts"
      additions={1}
      deletions={1}
      lines={lines}
      cycle={0}
    />
  );
}

function Demo26() {
  const hunks: DiffHunk[] = [
    { id: "1", range: "L10-L14", decision: "pending", lines: [] },
  ];

  return (
    <ReviewableDiff
      filename="src/index.ts"
      hunks={hunks}
      onKeep={keep}
      onDiscard={discard}
      onApply={apply}
    />
  );
}

function Demo27() {
  const nodes: FileTreeNode[] = [
    { path: "src", name: "src", depth: 0, kind: "folder" },
    {
      path: "src/index.ts",
      name: "index.ts",
      depth: 1,
      kind: "file",
      additions: 4,
      deletions: 1,
    },
  ];

  return (
    <FileTree
      nodes={nodes}
      visibleCount={nodes.length}
      totalAdditions={4}
      totalDeletions={1}
    />
  );
}

function Demo28() {
  const fields: ElicitationField[] = [
    {
      name: "env",
      label: "Environment",
      value: "",
      kind: "text",
      required: true,
    },
  ];

  return (
    <ElicitationForm
      server="deploy-server"
      message="Which environment should this deploy to?"
      fields={fields}
      state="request"
      onAccept={accept}
      onDecline={decline}
    />
  );
}

function Demo29() {
  const servers: McpServer[] = [
    {
      id: "linear",
      name: "Linear",
      transport: "http",
      status: "connected",
      tools: ["create_issue"],
    },
  ];

  return (
    <McpServerPanel
      servers={servers}
      expandedId={expandedId}
      onToggle={setExpandedId}
      onAuthorize={authorize}
    />
  );
}

function Demo30() {
  return (
    <ToolError
      name="run_migration"
      target="002_add_index.sql"
      message="Connection refused"
      attempt={1}
      maxAttempts={3}
      retrying={false}
      onRetry={retry}
      onSkip={skip}
    />
  );
}

function Demo31() {
  return (
    <PermissionGrant
      capability="Write files"
      requester="deploy_tool"
      reach={["src/**", "package.json"]}
      scope="pending"
      onGrant={grantScope}
    />
  );
}

function Demo32() {
  const steps: ComputerStep[] = [
    { id: "1", action: "click", target: "Submit button", x: 50, y: 80 },
  ];

  return (
    <>
      <ComputerUse url="https://example.com" steps={steps} activeIndex={0}>
        <img src="/screenshot.png" alt="" />
      </ComputerUse>
    </>
  );
}

function Demo33() {
  return (
    <CodeRunner
      language="ts"
      code="console.log(1 + 1)"
      state="ok"
      output={["2"]}
      durationMs={12}
      onRun={run}
    />
  );
}

function Demo34() {
  const tools: GroupedTool[] = [
    {
      id: "1",
      name: "read_file",
      target: "src/index.ts",
      state: "done",
      durationMs: 120,
    },
  ];

  return (
    <ToolGroup
      label="1 tool call"
      tools={tools}
      open={open}
      onOpenChange={setOpen}
    />
  );
}

function Demo35() {
  const results: WebSearchResult[] = [
    { title: "assistant-ui docs", domain: "assistant-ui.com" },
  ];

  return (
    <WebSearch
      query="assistant-ui elements"
      results={results}
      visibleResults={1}
      searching={false}
      cycle={0}
    />
  );
}

function Demo36() {
  const source: Source = {
    domain: "assistant-ui.com",
    title: "Elements",
    snippet: "The catalog of styled chat elements.",
  };
  return <Citation index={1} source={source} />;
}

function Demo37() {
  return (
    <ImageGeneration
      prompt="a watercolor fox in a forest"
      generating={generating}
    />
  );
}

function Demo38() {
  const chunks: RetrievalChunk[] = [
    {
      id: "1",
      source: "handbook.md",
      locator: "p. 4",
      score: 0.92,
      text: "...",
    },
  ];

  return (
    <RetrievalChunks
      query="refund policy"
      chunks={chunks}
      visibleCount={1}
      searching={false}
    />
  );
}

function Demo39() {
  const anchors: DocumentAnchor[] = [
    { page: 4, quote: "Refunds are processed within 5 business days." },
  ];

  return (
    <DocumentReference
      title="Refund policy"
      pages={12}
      anchors={anchors}
      activePage={4}
      onJump={jumpToPage}
    />
  );
}

function Demo40() {
  const chips: MemoryChip[] = [
    { id: "1", text: "Prefers TypeScript", change: "added" },
  ];

  return <MemoryChips chips={chips} onForget={forget} />;
}

function Demo41() {
  const sections: ReportSection[] = [
    {
      id: "1",
      heading: "Market size",
      state: "done",
      sources: 4,
      preview: "...",
    },
  ];

  return (
    <ResearchReport
      title="Q3 market research"
      sections={sections}
      sourcesRead={9}
    />
  );
}

function Demo42() {
  const pins: MapPin[] = [
    { id: "1", label: "Cafe", detail: "0.3 mi", x: 40, y: 60 },
  ];

  return <MapAnswer pins={pins} activeId="1" route onSelect={selectPin} />;
}

function Demo43() {
  const sources: Source_e498[] = [
    { domain: "assistant-ui.com", title: "Runtime drafts API" },
  ];

  return <Sources sources={sources} open={open} onOpenChange={setOpen} />;
}

function Demo44() {
  return (
    <DataTable
      caption="Demo model usage"
      columns={[
        { key: "name", label: "Model" },
        { key: "context", label: "Context" },
        { key: "cost", label: "Cost" },
      ]}
      rows={[{ name: "Codex", context: "200k", cost: "$3 / 1M" }]}
      rowKey="name"
    />
  );
}

function Demo45() {
  return <NumberTicker value={1284} label="tokens used" />;
}

function Demo46() {
  return (
    <Chart
      label="Requests"
      value="1,204"
      delta="+12%"
      points={[10, 14, 9, 18, 22]}
      visibleCount={5}
      variant="area"
    />
  );
}

function Demo47() {
  return (
    <>
      <WebPreview
        origin="preview.example.com"
        loading={false}
        onReload={reload}
        onOpenExternal={openExternal}
      >
        <iframe src={previewUrl} className="h-full w-full" />
      </WebPreview>
    </>
  );
}

function Demo48() {
  return (
    <>
      <Diagram
        title="Request flow"
        zoom={zoom}
        onZoomIn={zoomIn}
        onZoomOut={zoomOut}
        onReset={resetZoom}
        onExpand={expand}
      >
        <svg>{/* rendered graphic */}</svg>
      </Diagram>
    </>
  );
}

function Demo49() {
  const nodes: FlowNode[] = [
    { id: "a", label: "Start", column: 0, row: 0, state: "done" },
  ];
  const edges: FlowEdge[] = [{ from: "a", to: "b" }];

  return <FlowGraph nodes={nodes} edges={edges} visibleCount={nodes.length} />;
}

function Demo50() {
  const data = [{ date: "2026-08-01", count: 6 }];

  return (
    <ActivityGraph
      data={data}
      start="2026-02-01"
      end="2026-09-01"
      title="Runs"
      total="212 runs"
    />
  );
}

function Demo51() {
  const steps: MathStep[] = [
    { expression: "d/dx[x^2] = 2x", note: "power rule" },
  ];

  return (
    <MathBlock label="Derivative" steps={steps} visibleSteps={steps.length} />
  );
}

function Demo52() {
  const rows: SpecRow[] = [
    { label: "context", value: "200k tokens", emphasis: true },
  ];

  return (
    <SpecSheet
      title="Claude Sonnet"
      subtitle="Anthropic"
      rows={rows}
      visibleCount={rows.length}
    />
  );
}

function Demo53() {
  const options: ComparisonOption[] = [
    { id: "a", name: "Plan A", headline: "Cheaper", traits: ["Fast", false] },
    {
      id: "b",
      name: "Plan B",
      headline: "Faster",
      traits: ["Fast", "Reliable"],
    },
  ];

  return (
    <ComparisonCard
      traitLabels={["Speed", "Reliability"]}
      options={options}
      recommendedId="b"
      reason="Plan B wins on reliability."
    />
  );
}

function Demo54() {
  const events: TimelineEvent[] = [
    { id: "1", when: "past", time: "09:14", title: "Deploy started" },
  ];

  return <Timeline events={events} visibleCount={events.length} />;
}

function Demo55() {
  const stages: JobStage[] = [
    { name: "build", weight: 2 },
    { name: "test", weight: 1 },
  ];

  return (
    <JobProgress
      title="CI run"
      stages={stages}
      stageIndex={0}
      stageProgress={0.4}
      eta="2m left"
      onCancel={cancelJob}
    />
  );
}

function Demo56() {
  const criteria: ScoreCriterion[] = [
    {
      label: "Correctness",
      score: 8.5,
      weight: 2,
      note: "one edge case missed",
    },
  ];

  return (
    <ScoreBreakdown
      verdict="Pass"
      total={8.5}
      outOf={10}
      criteria={criteria}
      visibleCount={criteria.length}
    />
  );
}

function Demo57() {
  return (
    <AgentPlan
      steps={["Read the schema", "Write the migration", "Run the tests"]}
      activeIndex={1}
    />
  );
}

function Demo58() {
  const agents: SubagentItem[] = [
    { name: "Search agent", model: "gpt-5.6-luna" },
  ];

  return (
    <SubagentList
      agents={agents}
      completedCount={0}
      progress={[40]}
      showSummary={false}
      summaryAgent={{ name: "Summarizer", model: "claude-sonnet-4-6" }}
    />
  );
}

function Demo59() {
  return (
    <AgentStatus state="working" label="Reading the schema" elapsed="12s" />
  );
}

function Demo60() {
  return (
    <ApprovalCard
      state="request"
      command="rm -rf dist/"
      title="Run this command?"
      subtitle="Requested by the build agent"
      onAllowOnce={allowOnce}
      onAlwaysAllow={alwaysAllow}
      onDeny={deny}
    />
  );
}

function Demo61() {
  return (
    <>
      <RecommendationCard
        state="idle"
        question="Rename `data` to `payload`?"
        confidenceLabel="High confidence"
        acceptedLabel="Renamed"
        onAccept={accept}
        onAlternatives={showAlternatives}
      >
        Every call site already treats it as a request payload.
      </RecommendationCard>
    </>
  );
}

function Demo62() {
  return (
    <ArtifactCard
      title="Migration plan"
      meta="Draft"
      generating={generating}
      words={214}
      onClick={openArtifact}
    />
  );
}

function Demo63() {
  const items: TodoItem[] = [
    { id: "1", text: "Read the schema", status: "done" },
  ];

  return <TodoList items={items} revision={3} />;
}

function Demo64() {
  const skills: AgentSkill[] = [
    {
      name: "Refactor",
      description: "Restructures code without changing behavior",
    },
  ];

  return (
    <AgentCard
      name="Code agent"
      description="Handles refactors and bug fixes."
      provider="Acme"
      version="1.4.0"
      model="claude-sonnet-4-6"
      endpoint="https://agents.example.com/code"
      skills={skills}
      connected={false}
      onConnect={connect}
    />
  );
}

function Demo65() {
  return (
    <AgentHandoff
      from="Triage agent"
      to="Billing agent"
      reason="Refund request"
      carried={["order id", "email"]}
      settled
    />
  );
}

function Demo66() {
  const runs: BackgroundRun[] = [
    {
      id: "1",
      title: "Nightly report",
      state: "ready",
      elapsed: "6m",
      summary: "12 rows",
    },
  ];

  return <BackgroundInbox runs={runs} onCollect={collectRun} />;
}

function Demo67() {
  const checkpoints: Checkpoint[] = [
    { id: "1", label: "Before refactor", at: "10:02 AM", files: 6 },
  ];

  return (
    <CheckpointHistory
      checkpoints={checkpoints}
      currentId="1"
      onRestore={restoreCheckpoint}
    />
  );
}

function Demo68() {
  const history: ScheduleRun[] = [{ id: "1", at: "Yesterday, 9am", ok: true }];

  return (
    <ScheduleCard
      name="Daily digest"
      cadence="Every day at 9am"
      nextRun="Tomorrow, 9am"
      enabled
      history={history}
      onToggle={toggleSchedule}
    />
  );
}

function Demo69() {
  const spans: TraceSpan[] = [
    {
      id: "1",
      name: "search",
      depth: 0,
      startMs: 0,
      durationMs: 320,
      status: "completed",
    },
  ];

  return (
    <TraceWaterfall spans={spans} totalMs={1200} visibleCount={spans.length} />
  );
}

function Demo70() {
  const lines: CostLine[] = [
    {
      model: "Claude Sonnet",
      inputTokens: 12,
      outputTokens: 3,
      cost: "$0.09",
      share: 0.7,
    },
  ];

  return <CostMeter runCost="$0.09" sessionCost="$1.42" lines={lines} />;
}

function Demo71() {
  return (
    <QuotaBanner
      used={92}
      limit={100}
      unit="messages"
      resetsIn="4h"
      upgradeLabel="Upgrade"
      onUpgrade={upgrade}
    />
  );
}

function Demo72() {
  return (
    <>
      <Composer>
        <ComposerBar dragActive={dragActive}>
          <ComposerInput onSubmit={sendMessage} />
          <ComposerSend streaming={streaming} idle={!streaming} />
        </ComposerBar>
      </Composer>
    </>
  );
}

function Demo73() {
  const command: ComposerCommand = {
    name: "clear",
    description: "Clear the conversation",
    icon: FileTextIcon,
  };

  return <ComposerCommandItem command={command} active={active} />;
}

function Demo74() {
  const person: ComposerPerson = { name: "Search agent", role: "agent" };

  return <ComposerPersonItem person={person} active={active} />;
}

function Demo75() {
  const attachment: ComposerAttachment = {
    name: "spec.pdf",
    meta: "340 KB",
    state: "done",
    kind: "text",
  };

  return (
    <ComposerAttachmentChip
      attachment={attachment}
      onRemove={removeAttachment}
    />
  );
}

function Demo76() {
  const model: ComposerModel = { name: "Claude Sonnet", meta: "$3 / 1M" };

  return (
    <>
      <ComposerModelTrigger model={model.name} open={open} />;
      <ComposerModelItem entry={model} selected />
    </>
  );
}

function Demo77() {
  return (
    <>
      <ComposerVoice recording={recording} seconds={12} />;
      <ComposerVoiceButton active={recording} />
    </>
  );
}

function Demo78() {
  const usage: ComposerUsage = {
    system: 1.2,
    tools: 0.8,
    messages: 4.1,
    total: 200,
  };

  return <ComposerContext usage={usage} />;
}

function Demo79() {
  return (
    <DraftRestore
      draft="Can you also check the..."
      savedAt="2 hours ago"
      onRestore={restoreDraft}
      onDiscard={discardDraft}
    />
  );
}

function Demo80() {
  const segments: ContextSegment[] = [
    { label: "Conversation", tokens: 42000, tint: "bg-blue-500" },
  ];

  return <ContextBreakdown segments={segments} limit={200000} />;
}

function Demo81() {
  const prompts: SavedPrompt[] = [
    {
      id: "1",
      name: "Write tests",
      body: "Write tests for {{file}}",
      variables: ["file"],
    },
  ];

  return (
    <PromptLibrary
      prompts={prompts}
      query={query}
      selectedId="1"
      onQueryChange={setQuery}
      onSelect={selectPrompt}
      onInsert={insertPrompt}
    />
  );
}

function Demo82() {
  const commands: PaletteCommand[] = [
    { id: "new", label: "New thread", group: "Thread", keys: ["Cmd", "N"] },
  ];

  return (
    <CommandPalette
      commands={commands}
      query={query}
      activeId={activeId}
      onQueryChange={setQuery}
      onActiveChange={setActiveId}
      onRun={runCommand}
    />
  );
}

function Demo83() {
  const models: PickableModel[] = [
    {
      id: "sonnet",
      name: "Claude Sonnet",
      family: "Anthropic",
      context: "200k",
      price: "$3 / 1M",
      capabilities: ["vision", "tools"],
    },
  ];

  return (
    <ModelPicker models={models} selectedId="sonnet" onSelect={selectModel} />
  );
}

function Demo84() {
  const transcript: VoiceTurn[] = [
    { id: "1", role: "user", text: "What's on my calendar?" },
  ];

  return (
    <VoiceConversation
      mode="listening"
      amplitude={0.4}
      transcript={transcript}
      muted={false}
      onToggleMute={toggleMute}
      onInterrupt={interrupt}
      onEnd={endCall}
    />
  );
}

function Demo85() {
  return (
    <ReadAloud
      words={["The", "answer", "is", "42."]}
      spokenIndex={1}
      playing
      rate={1}
      elapsed="0:02"
      duration="0:06"
      onToggle={togglePlayback}
      onRateChange={() => undefined}
    />
  );
}

function Demo86() {
  return (
    <>
      <ChatPanel>
        <ChatPanelMessages>
          <ChatPanelUserMessage>Can you review this?</ChatPanelUserMessage>
          <ChatPanelAssistantMessage>
            Looks good overall.
          </ChatPanelAssistantMessage>
        </ChatPanelMessages>
        <ChatPanelComposer
          placeholder="Ask a question..."
          onSend={sendMessage}
        />
      </ChatPanel>
    </>
  );
}

function Demo87() {
  return (
    <>
      <EmptyState>
        <EmptyStateGreeting>Ask me anything</EmptyStateGreeting>
        <EmptyStateSuggestions>
          <EmptyStateSuggestion index={0}>
            Summarize this repo
          </EmptyStateSuggestion>
        </EmptyStateSuggestions>
        <EmptyStateComposer
          placeholder="Send a message..."
          onSend={sendMessage}
        />
      </EmptyState>
    </>
  );
}

function Demo88() {
  const messages: ScrollAnchorMessage[] = [
    { role: "assistant", text: "Here is the summary." },
  ];

  return (
    <ScrollAnchor
      messages={messages}
      paused={false}
      onSettled={handleSettled}
    />
  );
}

function Demo89() {
  return (
    <>
      <CanvasSplit>
        <CanvasSplitThread>
          <CanvasSplitMessage speaker="user">
            Draft the README
          </CanvasSplitMessage>
        </CanvasSplitThread>
        <CanvasSplitDocument>
          <CanvasSplitHeader
            title="README.md"
            version={2}
            saved={false}
            onCopy={copyDoc}
            onClose={closeDoc}
          />
          <CanvasSplitBody writing>
            <CanvasSplitLine heading>Overview</CanvasSplitLine>
          </CanvasSplitBody>
        </CanvasSplitDocument>
      </CanvasSplit>
    </>
  );
}

function Demo90() {
  return (
    <ConnectionState
      phase="reconnecting"
      attempt={2}
      onRetry={retryConnection}
    />
  );
}

function Demo91() {
  const turns: SharedTurn[] = [
    { id: "1", role: "user", text: "How do I deploy this?" },
  ];

  return (
    <SharedConversation
      title="Deploy walkthrough"
      sharedBy="Jamie"
      sharedAt="Yesterday"
      turns={turns}
      onContinue={continueThread}
    />
  );
}

function Demo92() {
  const hits: SearchHit[] = [
    {
      id: "1",
      before: "the ",
      match: "migration",
      after: " runs nightly",
      position: 40,
    },
  ];

  return (
    <ConversationSearch
      query={query}
      hits={hits}
      activeIndex={0}
      onQueryChange={setQuery}
      onStep={stepHit}
    />
  );
}

function Demo93() {
  const threads: SearchableThread[] = [
    {
      id: "1",
      title: "Refund flow",
      group: "Today",
      preview: "...",
      pinned: true,
    },
  ];

  return (
    <ThreadSearch
      threads={threads}
      query={query}
      activeId="1"
      onQueryChange={setQuery}
      onActiveChange={selectThread}
      onSelect={selectThread}
    />
  );
}

function Demo94() {
  return (
    <LauncherBubble
      open={open}
      unread={2}
      greeting="Need help?"
      prompts={["Track my order", "Talk to support"]}
      onToggle={toggleOpen}
      onPick={sendPrompt}
      onStart={startConversation}
    />
  );
}

function Demo95() {
  const toggles: SettingToggle[] = [
    { key: "web", label: "Web search", detail: "Allow live lookups", on: true },
  ];

  return (
    <SettingsPanel
      model="claude-sonnet-4-6"
      models={["claude-sonnet-4-6", "gpt-5.6-luna"]}
      systemPrompt={systemPrompt}
      temperature={0.7}
      toggles={toggles}
      onModelChange={setModel}
      onSystemPromptChange={setSystemPrompt}
      onTemperatureChange={setTemperature}
      onToggle={toggleSetting}
    />
  );
}

function Demo96() {
  const steps: OnboardingStep[] = [
    {
      title: "Ask anything",
      body: "Type a question to get started.",
      example: "Summarize this doc",
    },
  ];

  return (
    <Onboarding
      steps={steps}
      index={0}
      onNext={nextStep}
      onSkip={skipOnboarding}
    />
  );
}

function Demo97() {
  return (
    <MobileComposer
      value={draft}
      keyboardOpen={keyboardOpen}
      running={running}
      actions={["Summarize", "Translate"]}
      onAction={runQuickAction}
      onAttach={openAttachmentPicker}
      onValueChange={setDraft}
      onSend={sendMessage}
      onStop={stopRun}
      onFocus={handleFocus}
    />
  );
}

function Demo98() {
  return (
    <Suggestions
      suggestions={["Explain that differently", "Show an example"]}
      selectedSuggestion={null}
      cycle={cycle}
      onSuggestion={handleSuggestion}
      variant="pills"
    />
  );
}

export const galleryEntries: {
  id: string;
  title: string;
  backing: "Demo data";
  node: ReactNode;
}[] = [
  {
    id: "loading-state-0",
    title: "loading-state",
    backing: "Demo data",
    node: <Demo0 />,
  },
  {
    id: "thinking-indicator-1",
    title: "thinking-indicator",
    backing: "Demo data",
    node: <Demo1 />,
  },
  {
    id: "streaming-text-2",
    title: "streaming-text",
    backing: "Demo data",
    node: <Demo2 />,
  },
  {
    id: "typing-indicator-3",
    title: "typing-indicator",
    backing: "Demo data",
    node: <Demo3 />,
  },
  {
    id: "reasoning-effort-4",
    title: "reasoning-effort",
    backing: "Demo data",
    node: <Demo4 />,
  },
  {
    id: "guardrail-notice-5",
    title: "guardrail-notice",
    backing: "Demo data",
    node: <Demo5 />,
  },
  {
    id: "reasoning-panel-6",
    title: "reasoning-panel",
    backing: "Demo data",
    node: <Demo6 />,
  },
  {
    id: "message-pair-7",
    title: "message-pair",
    backing: "Demo data",
    node: <Demo7 />,
  },
  {
    id: "message-branches-8",
    title: "message-branches",
    backing: "Demo data",
    node: <Demo8 />,
  },
  {
    id: "message-actions-9",
    title: "message-actions",
    backing: "Demo data",
    node: <Demo9 />,
  },
  {
    id: "error-state-10",
    title: "error-state",
    backing: "Demo data",
    node: <Demo10 />,
  },
  {
    id: "message-queue-11",
    title: "message-queue",
    backing: "Demo data",
    node: <Demo11 />,
  },
  {
    id: "edit-message-12",
    title: "edit-message",
    backing: "Demo data",
    node: <Demo12 />,
  },
  {
    id: "feedback-dialog-13",
    title: "feedback-dialog",
    backing: "Demo data",
    node: <Demo13 />,
  },
  {
    id: "stopped-run-14",
    title: "stopped-run",
    backing: "Demo data",
    node: <Demo14 />,
  },
  {
    id: "day-separator-15",
    title: "day-separator",
    backing: "Demo data",
    node: <Demo15 />,
  },
  {
    id: "speaker-identity-16",
    title: "speaker-identity",
    backing: "Demo data",
    node: <Demo16 />,
  },
  {
    id: "regenerate-menu-17",
    title: "regenerate-menu",
    backing: "Demo data",
    node: <Demo17 />,
  },
  {
    id: "confidence-marker-18",
    title: "confidence-marker",
    backing: "Demo data",
    node: <Demo18 />,
  },
  {
    id: "message-attachment-19",
    title: "message-attachment",
    backing: "Demo data",
    node: <Demo19 />,
  },
  {
    id: "quote-reply-20",
    title: "quote-reply",
    backing: "Demo data",
    node: <Demo20 />,
  },
  {
    id: "message-timing-21",
    title: "message-timing",
    backing: "Demo data",
    node: <Demo21 />,
  },
  {
    id: "tool-call-22",
    title: "tool-call",
    backing: "Demo data",
    node: <Demo22 />,
  },
  {
    id: "tool-timeline-23",
    title: "tool-timeline",
    backing: "Demo data",
    node: <Demo23 />,
  },
  {
    id: "terminal-block-24",
    title: "terminal-block",
    backing: "Demo data",
    node: <Demo24 />,
  },
  {
    id: "code-diff-25",
    title: "code-diff",
    backing: "Demo data",
    node: <Demo25 />,
  },
  {
    id: "reviewable-diff-26",
    title: "reviewable-diff",
    backing: "Demo data",
    node: <Demo26 />,
  },
  {
    id: "file-tree-27",
    title: "file-tree",
    backing: "Demo data",
    node: <Demo27 />,
  },
  {
    id: "elicitation-form-28",
    title: "elicitation-form",
    backing: "Demo data",
    node: <Demo28 />,
  },
  {
    id: "mcp-server-panel-29",
    title: "mcp-server-panel",
    backing: "Demo data",
    node: <Demo29 />,
  },
  {
    id: "tool-error-30",
    title: "tool-error",
    backing: "Demo data",
    node: <Demo30 />,
  },
  {
    id: "permission-grant-31",
    title: "permission-grant",
    backing: "Demo data",
    node: <Demo31 />,
  },
  {
    id: "computer-use-32",
    title: "computer-use",
    backing: "Demo data",
    node: <Demo32 />,
  },
  {
    id: "code-runner-33",
    title: "code-runner",
    backing: "Demo data",
    node: <Demo33 />,
  },
  {
    id: "tool-group-34",
    title: "tool-group",
    backing: "Demo data",
    node: <Demo34 />,
  },
  {
    id: "web-search-35",
    title: "web-search",
    backing: "Demo data",
    node: <Demo35 />,
  },
  {
    id: "inline-citation-36",
    title: "inline-citation",
    backing: "Demo data",
    node: <Demo36 />,
  },
  {
    id: "image-generation-37",
    title: "image-generation",
    backing: "Demo data",
    node: <Demo37 />,
  },
  {
    id: "retrieval-chunks-38",
    title: "retrieval-chunks",
    backing: "Demo data",
    node: <Demo38 />,
  },
  {
    id: "document-reference-39",
    title: "document-reference",
    backing: "Demo data",
    node: <Demo39 />,
  },
  {
    id: "memory-chips-40",
    title: "memory-chips",
    backing: "Demo data",
    node: <Demo40 />,
  },
  {
    id: "research-report-41",
    title: "research-report",
    backing: "Demo data",
    node: <Demo41 />,
  },
  {
    id: "map-answer-42",
    title: "map-answer",
    backing: "Demo data",
    node: <Demo42 />,
  },
  {
    id: "sources-43",
    title: "sources",
    backing: "Demo data",
    node: <Demo43 />,
  },
  {
    id: "data-table-44",
    title: "data-table",
    backing: "Demo data",
    node: <Demo44 />,
  },
  {
    id: "number-ticker-45",
    title: "number-ticker",
    backing: "Demo data",
    node: <Demo45 />,
  },
  { id: "chart-46", title: "chart", backing: "Demo data", node: <Demo46 /> },
  {
    id: "web-preview-47",
    title: "web-preview",
    backing: "Demo data",
    node: <Demo47 />,
  },
  {
    id: "diagram-48",
    title: "diagram",
    backing: "Demo data",
    node: <Demo48 />,
  },
  {
    id: "flow-graph-49",
    title: "flow-graph",
    backing: "Demo data",
    node: <Demo49 />,
  },
  {
    id: "activity-graph-50",
    title: "activity-graph",
    backing: "Demo data",
    node: <Demo50 />,
  },
  {
    id: "math-block-51",
    title: "math-block",
    backing: "Demo data",
    node: <Demo51 />,
  },
  {
    id: "spec-sheet-52",
    title: "spec-sheet",
    backing: "Demo data",
    node: <Demo52 />,
  },
  {
    id: "comparison-card-53",
    title: "comparison-card",
    backing: "Demo data",
    node: <Demo53 />,
  },
  {
    id: "timeline-54",
    title: "timeline",
    backing: "Demo data",
    node: <Demo54 />,
  },
  {
    id: "job-progress-55",
    title: "job-progress",
    backing: "Demo data",
    node: <Demo55 />,
  },
  {
    id: "score-breakdown-56",
    title: "score-breakdown",
    backing: "Demo data",
    node: <Demo56 />,
  },
  {
    id: "agent-plan-57",
    title: "agent-plan",
    backing: "Demo data",
    node: <Demo57 />,
  },
  {
    id: "subagent-list-58",
    title: "subagent-list",
    backing: "Demo data",
    node: <Demo58 />,
  },
  {
    id: "agent-status-59",
    title: "agent-status",
    backing: "Demo data",
    node: <Demo59 />,
  },
  {
    id: "approval-card-60",
    title: "approval-card",
    backing: "Demo data",
    node: <Demo60 />,
  },
  {
    id: "recommendation-card-61",
    title: "recommendation-card",
    backing: "Demo data",
    node: <Demo61 />,
  },
  {
    id: "artifact-card-62",
    title: "artifact-card",
    backing: "Demo data",
    node: <Demo62 />,
  },
  {
    id: "todo-list-63",
    title: "todo-list",
    backing: "Demo data",
    node: <Demo63 />,
  },
  {
    id: "agent-card-64",
    title: "agent-card",
    backing: "Demo data",
    node: <Demo64 />,
  },
  {
    id: "agent-handoff-65",
    title: "agent-handoff",
    backing: "Demo data",
    node: <Demo65 />,
  },
  {
    id: "background-inbox-66",
    title: "background-inbox",
    backing: "Demo data",
    node: <Demo66 />,
  },
  {
    id: "checkpoint-history-67",
    title: "checkpoint-history",
    backing: "Demo data",
    node: <Demo67 />,
  },
  {
    id: "schedule-card-68",
    title: "schedule-card",
    backing: "Demo data",
    node: <Demo68 />,
  },
  {
    id: "trace-waterfall-69",
    title: "trace-waterfall",
    backing: "Demo data",
    node: <Demo69 />,
  },
  {
    id: "cost-meter-70",
    title: "cost-meter",
    backing: "Demo data",
    node: <Demo70 />,
  },
  {
    id: "quota-banner-71",
    title: "quota-banner",
    backing: "Demo data",
    node: <Demo71 />,
  },
  {
    id: "composer-and-its-variants-72",
    title: "composer (and its variants)",
    backing: "Demo data",
    node: <Demo72 />,
  },
  {
    id: "slash-commands-73",
    title: "Slash commands",
    backing: "Demo data",
    node: <Demo73 />,
  },
  {
    id: "mentions-74",
    title: "Mentions",
    backing: "Demo data",
    node: <Demo74 />,
  },
  {
    id: "attachments-75",
    title: "Attachments",
    backing: "Demo data",
    node: <Demo75 />,
  },
  { id: "models-76", title: "Models", backing: "Demo data", node: <Demo76 /> },
  {
    id: "dictation-77",
    title: "Dictation",
    backing: "Demo data",
    node: <Demo77 />,
  },
  {
    id: "context-78",
    title: "Context",
    backing: "Demo data",
    node: <Demo78 />,
  },
  {
    id: "draft-restore-79",
    title: "draft-restore",
    backing: "Demo data",
    node: <Demo79 />,
  },
  {
    id: "context-breakdown-80",
    title: "context-breakdown",
    backing: "Demo data",
    node: <Demo80 />,
  },
  {
    id: "prompt-library-81",
    title: "prompt-library",
    backing: "Demo data",
    node: <Demo81 />,
  },
  {
    id: "command-palette-82",
    title: "command-palette",
    backing: "Demo data",
    node: <Demo82 />,
  },
  {
    id: "model-picker-83",
    title: "model-picker",
    backing: "Demo data",
    node: <Demo83 />,
  },
  {
    id: "voice-conversation-84",
    title: "voice-conversation",
    backing: "Demo data",
    node: <Demo84 />,
  },
  {
    id: "read-aloud-85",
    title: "read-aloud",
    backing: "Demo data",
    node: <Demo85 />,
  },
  {
    id: "chat-panel-86",
    title: "chat-panel",
    backing: "Demo data",
    node: <Demo86 />,
  },
  {
    id: "empty-state-87",
    title: "empty-state",
    backing: "Demo data",
    node: <Demo87 />,
  },
  {
    id: "scroll-anchor-88",
    title: "scroll-anchor",
    backing: "Demo data",
    node: <Demo88 />,
  },
  {
    id: "canvas-split-89",
    title: "canvas-split",
    backing: "Demo data",
    node: <Demo89 />,
  },
  {
    id: "connection-state-90",
    title: "connection-state",
    backing: "Demo data",
    node: <Demo90 />,
  },
  {
    id: "shared-conversation-91",
    title: "shared-conversation",
    backing: "Demo data",
    node: <Demo91 />,
  },
  {
    id: "conversation-search-92",
    title: "conversation-search",
    backing: "Demo data",
    node: <Demo92 />,
  },
  {
    id: "thread-search-93",
    title: "thread-search",
    backing: "Demo data",
    node: <Demo93 />,
  },
  {
    id: "launcher-bubble-94",
    title: "launcher-bubble",
    backing: "Demo data",
    node: <Demo94 />,
  },
  {
    id: "settings-panel-95",
    title: "settings-panel",
    backing: "Demo data",
    node: <Demo95 />,
  },
  {
    id: "onboarding-96",
    title: "onboarding",
    backing: "Demo data",
    node: <Demo96 />,
  },
  {
    id: "mobile-composer-97",
    title: "mobile-composer",
    backing: "Demo data",
    node: <Demo97 />,
  },
  {
    id: "suggestions-98",
    title: "suggestions",
    backing: "Demo data",
    node: <Demo98 />,
  },
];
