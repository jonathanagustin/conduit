import {
  ActionBarMorePrimitive,
  ActionBarPrimitive,
  BranchPickerPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  SelectionToolbarPrimitive,
  SuggestionPrimitive,
  ThreadPrimitive,
  useAui,
  useAuiState,
  useMessageTiming,
  type ImageMessagePartComponent,
  type FileMessagePartComponent,
  type SourceMessagePartComponent,
} from "@assistant-ui/react";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CopyIcon,
  MicIcon,
  MoreHorizontalIcon,
  PencilIcon,
  RefreshCwIcon,
  SquareIcon,
  ThumbsDownIcon,
  ThumbsUpIcon,
  Volume2Icon,
} from "lucide-react";

import { STARTERS } from "../../api/runtime.js";
import { Button } from "../ui/button.js";
import { MarkdownText } from "./markdown-text.js";
import { Reasoning } from "./reasoning.js";
import { ToolFallback } from "./tool-fallback.js";
import { TooltipIconButton } from "./tooltip-icon-button.js";

const ImagePart: ImageMessagePartComponent = ({ image }) => (
  <img src={image} alt="" className="max-h-64 rounded-md border border-border" />
);

const FilePart: FileMessagePartComponent = ({ filename, mimeType }) => (
  <div className="rounded-md border border-border px-2 py-1 text-xs">
    {filename ?? "file"} <span className="text-muted-foreground">{mimeType}</span>
  </div>
);

const SourcePart: SourceMessagePartComponent = ({ url, title }) => (
  <a href={url} target="_blank" rel="noreferrer" className="text-sm underline">
    {title ?? url}
  </a>
);

function Timing() {
  const timing = useMessageTiming();
  if (!timing) return null;
  const seconds = timing.totalStreamTime === undefined ? undefined : (timing.totalStreamTime / 1000).toFixed(1);
  return (
    <span className="text-xs text-muted-foreground" data-testid="message-timing">
      {timing.toolCallCount} tools{seconds ? ` · ${seconds}s` : ""}
    </span>
  );
}

function Usage() {
  const usage = useAuiState((state) => {
    const custom = state.message.metadata?.custom;
    return typeof custom?.["usage"] === "string" ? custom["usage"] : undefined;
  });
  if (!usage) return null;
  return (
    <span className="text-xs text-muted-foreground" data-testid="context-usage">
      Context {usage}
    </span>
  );
}

function AssistantMessage() {
  return (
    <MessagePrimitive.Root className="py-3" data-role="assistant">
      <MessagePrimitive.Parts
        components={{
          Text: MarkdownText,
          Reasoning,
          tools: { Fallback: ToolFallback },
          Image: ImagePart,
          File: FilePart,
          Source: SourcePart,
        }}
      />
      <div className="mt-1 flex items-center gap-1">
        <Timing />
        <Usage />
      </div>
      <ActionBarPrimitive.Root hideWhenRunning autohide="not-last" className="mt-1 flex gap-1">
        <ActionBarPrimitive.Copy asChild>
          <TooltipIconButton tip="Copy">
            <CopyIcon />
          </TooltipIconButton>
        </ActionBarPrimitive.Copy>
        <ActionBarPrimitive.Reload asChild>
          <TooltipIconButton tip="Regenerate">
            <RefreshCwIcon />
          </TooltipIconButton>
        </ActionBarPrimitive.Reload>
        <ActionBarPrimitive.Speak asChild>
          <TooltipIconButton tip="Speak">
            <Volume2Icon />
          </TooltipIconButton>
        </ActionBarPrimitive.Speak>
        <ActionBarPrimitive.FeedbackPositive asChild>
          <TooltipIconButton tip="Good response">
            <ThumbsUpIcon />
          </TooltipIconButton>
        </ActionBarPrimitive.FeedbackPositive>
        <ActionBarPrimitive.FeedbackNegative asChild>
          <TooltipIconButton tip="Bad response">
            <ThumbsDownIcon />
          </TooltipIconButton>
        </ActionBarPrimitive.FeedbackNegative>
        <ActionBarMorePrimitive.Root>
          <ActionBarMorePrimitive.Trigger asChild>
            <TooltipIconButton tip="More">
              <MoreHorizontalIcon />
            </TooltipIconButton>
          </ActionBarMorePrimitive.Trigger>
          <ActionBarMorePrimitive.Content className="z-50 rounded-md border border-border bg-popover p-1">
            <ActionBarPrimitive.ExportMarkdown asChild>
              <ActionBarMorePrimitive.Item className="block w-full rounded px-2 py-1 text-left text-sm">
                Export markdown
              </ActionBarMorePrimitive.Item>
            </ActionBarPrimitive.ExportMarkdown>
          </ActionBarMorePrimitive.Content>
        </ActionBarMorePrimitive.Root>
        <BranchPicker />
      </ActionBarPrimitive.Root>
      <MessagePrimitive.Error>
        <p className="mt-2 text-sm text-destructive" role="alert" />
      </MessagePrimitive.Error>
    </MessagePrimitive.Root>
  );
}

function UserMessage() {
  return (
    <MessagePrimitive.Root className="py-3" data-role="user">
      <MessagePrimitive.Attachments>
        {({ attachment }) => <span className="text-xs">{attachment.name}</span>}
      </MessagePrimitive.Attachments>
      <MessagePrimitive.Parts components={{ Image: ImagePart, File: FilePart }} />
      <ActionBarPrimitive.Root hideWhenRunning className="flex gap-1">
        <ActionBarPrimitive.Edit asChild>
          <TooltipIconButton tip="Edit">
            <PencilIcon />
          </TooltipIconButton>
        </ActionBarPrimitive.Edit>
        <ActionBarPrimitive.Copy asChild>
          <TooltipIconButton tip="Copy">
            <CopyIcon />
          </TooltipIconButton>
        </ActionBarPrimitive.Copy>
        <BranchPicker />
      </ActionBarPrimitive.Root>
    </MessagePrimitive.Root>
  );
}

function EditComposer() {
  const editing = useAuiState((state) => state.message.composer.isEditing);
  if (!editing) return null;
  return (
    <ComposerPrimitive.Root className="rounded-lg border border-border p-2">
      <ComposerPrimitive.Input className="w-full resize-none bg-transparent text-sm outline-none" />
      <div className="flex justify-end gap-2">
        <ComposerPrimitive.Cancel asChild>
          <Button variant="ghost" size="sm">
            Cancel
          </Button>
        </ComposerPrimitive.Cancel>
        <ComposerPrimitive.Send asChild>
          <Button size="sm">Update</Button>
        </ComposerPrimitive.Send>
      </div>
    </ComposerPrimitive.Root>
  );
}

function BranchPicker() {
  return (
    <BranchPickerPrimitive.Root hideWhenSingleBranch className="inline-flex items-center text-xs text-muted-foreground">
      <BranchPickerPrimitive.Previous asChild>
        <TooltipIconButton tip="Previous branch">
          <ChevronLeftIcon />
        </TooltipIconButton>
      </BranchPickerPrimitive.Previous>
      <BranchPickerPrimitive.Number /> / <BranchPickerPrimitive.Count />
      <BranchPickerPrimitive.Next asChild>
        <TooltipIconButton tip="Next branch">
          <ChevronRightIcon />
        </TooltipIconButton>
      </BranchPickerPrimitive.Next>
    </BranchPickerPrimitive.Root>
  );
}

function StarterPrompts() {
  const aui = useAui();
  return (
    <>
      {STARTERS.map((starter) => (
        <Button
          key={starter.title}
          variant="outline"
          className="h-auto justify-start px-3 py-2 text-left"
          onClick={() => aui.thread.append(starter.prompt)}
        >
          <span>{starter.title}</span>
          <span className="text-muted-foreground">{starter.label}</span>
        </Button>
      ))}
    </>
  );
}

function SuggestionItem() {
  return (
    <SuggestionPrimitive.Trigger send asChild>
      <Button variant="outline" className="h-auto justify-start px-3 py-2 text-left">
        <SuggestionPrimitive.Title />
        <SuggestionPrimitive.Description className="text-muted-foreground" />
      </Button>
    </SuggestionPrimitive.Trigger>
  );
}

function Composer() {
  return (
    <ComposerPrimitive.Root className="rounded-(--composer-radius) border border-border bg-background p-(--composer-padding)">
      <ComposerPrimitive.AttachmentDropzone>
        <ComposerPrimitive.Quote className="mb-2 flex items-center justify-between rounded bg-muted px-2 py-1 text-xs">
          <ComposerPrimitive.QuoteText />
          <ComposerPrimitive.QuoteDismiss>Dismiss</ComposerPrimitive.QuoteDismiss>
        </ComposerPrimitive.Quote>
        <ComposerPrimitive.Attachments>
          {({ attachment }) => <span className="text-xs">{attachment.name}</span>}
        </ComposerPrimitive.Attachments>
        <ComposerPrimitive.Input
          placeholder="Ask Codex about this workspace"
          aria-label="Message input"
          className="max-h-48 min-h-10 w-full resize-none bg-transparent px-2 py-1 text-base outline-none"
          rows={1}
          autoFocus
        />
        <div className="flex items-center justify-between">
          <ComposerPrimitive.AddAttachment asChild>
            <Button variant="ghost" size="sm" type="button">
              Attach
            </Button>
          </ComposerPrimitive.AddAttachment>
          <div className="flex gap-1">
            <ComposerPrimitive.Dictate asChild>
              <TooltipIconButton tip="Dictate">
                <MicIcon />
              </TooltipIconButton>
            </ComposerPrimitive.Dictate>
            <ComposerPrimitive.Send asChild>
              <TooltipIconButton tip="Send" data-testid="send-message">
                <ArrowUpIcon />
              </TooltipIconButton>
            </ComposerPrimitive.Send>
            <ComposerPrimitive.Cancel asChild>
              <TooltipIconButton tip="Stop" data-testid="stop-message">
                <SquareIcon />
              </TooltipIconButton>
            </ComposerPrimitive.Cancel>
          </div>
        </div>
      </ComposerPrimitive.AttachmentDropzone>
    </ComposerPrimitive.Root>
  );
}

export function Thread() {
  return (
    <ThreadPrimitive.Root className="flex h-full flex-col bg-background" style={{ maxWidth: "var(--thread-max-width)" }}>
      <SelectionToolbarPrimitive.Root className="flex gap-1 rounded-md border border-border bg-popover p-1">
        <SelectionToolbarPrimitive.Quote asChild>
          <Button size="sm" variant="ghost">
            Quote
          </Button>
        </SelectionToolbarPrimitive.Quote>
      </SelectionToolbarPrimitive.Root>
      <ThreadPrimitive.Viewport className="flex flex-1 flex-col gap-2 overflow-y-auto px-4 pt-4">
        <ThreadPrimitive.Empty>
          <div className="mx-auto flex max-w-lg flex-col gap-3 py-16" data-testid="assistant-welcome">
            <h1 className="text-2xl font-semibold">How can I help you today?</h1>
            <p className="text-sm text-muted-foreground">
              This is a local Codex session over the Agent Client Protocol. Approving a tool runs it in the workspace.
              It does not approve a Conduit plan or deployment.
            </p>
            <div className="flex flex-col gap-2">
              <StarterPrompts />
              <ThreadPrimitive.Suggestions>
                {() => <SuggestionItem />}
              </ThreadPrimitive.Suggestions>
            </div>
          </div>
        </ThreadPrimitive.Empty>
        <ThreadPrimitive.LoadEarlier asChild>
          <Button variant="ghost" size="sm">
            Load earlier
          </Button>
        </ThreadPrimitive.LoadEarlier>
        <ThreadPrimitive.Messages
          components={{ UserMessage, AssistantMessage, EditComposer, SystemMessage: () => null }}
        />
        <ThreadPrimitive.Suggestions>
          {() => <SuggestionItem />}
        </ThreadPrimitive.Suggestions>
      </ThreadPrimitive.Viewport>
      <div className="sticky bottom-0 bg-background px-4 pb-4">
        <ThreadPrimitive.ScrollToBottom asChild>
          <TooltipIconButton tip="Scroll to bottom" className="mb-2">
            <ArrowDownIcon />
          </TooltipIconButton>
        </ThreadPrimitive.ScrollToBottom>
        <Composer />
      </div>
    </ThreadPrimitive.Root>
  );
}
