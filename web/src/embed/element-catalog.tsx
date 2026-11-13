"use client";

import { Component, type ReactNode } from "react";
import { AssistantModal } from "@/components/assistant-ui/elements/assistant-modal.aui";
import { AssistantSidebar } from "@/components/assistant-ui/elements/assistant-sidebar.aui";
import {
  ClaudeLogo,
  GeminiLogo,
  OpenAILogo,
} from "@/components/assistant-ui/elements/logos";
import { McpConfigDialog } from "@/components/assistant-ui/elements/mcp-config.aui";
import { ModelSelector } from "@/components/assistant-ui/elements/model-selector.aui";
import { SyntaxHighlighter as ShikiHighlighter } from "@/components/assistant-ui/elements/shiki-highlighter";
import { MermaidDiagram } from "@/components/assistant-ui/elements/mermaid-diagram";
import { VoiceOrb } from "@/components/assistant-ui/elements/voice";
import { HeatGraph } from "@/components/assistant-ui/elements/heat-graph";
import { GeoMap } from "@/components/assistant-ui/elements/geo-map";
import { ImageGallery } from "@/components/assistant-ui/elements/image-gallery";
import { LinkPreview } from "@/components/assistant-ui/elements/link-preview";
import { OptionList } from "@/components/assistant-ui/elements/option-list";
import { QuestionFlow } from "@/components/assistant-ui/elements/question-flow";
import { RunActivity } from "@/components/assistant-ui/elements/run-activity";
import { TaskCard } from "@/components/assistant-ui/elements/task-card";
import { GenerativeMarkdown } from "@/components/assistant-ui/elements/generative-markdown";
import { ThreadList as StandaloneThreadList } from "@/components/assistant-ui/elements/thread-list";
import { createDirectiveText } from "@/components/assistant-ui/elements/directive-text";
import { defaultDirectiveFormatter } from "@assistant-ui/react";
import { galleryEntries } from "@/src/embed/element-gallery";

const DemoDirective = createDirectiveText(defaultDirectiveFormatter);

class DemoBoundary extends Component<
  { children: ReactNode },
  { message: string }
> {
  state = { message: "" };

  static getDerivedStateFromError(error: unknown) {
    return { message: error instanceof Error ? error.message : String(error) };
  }

  render() {
    if (this.state.message) {
      return (
        <p role="alert" className="text-destructive text-sm">
          {this.state.message}
        </p>
      );
    }
    return this.props.children;
  }
}

function Section({
  id,
  title,
  backing,
  children,
}: {
  id: string;
  title: string;
  backing: string;
  children: ReactNode;
}) {
  return (
    <section
      id={id}
      data-element={id}
      className="flex flex-col gap-2 border-b pb-6"
    >
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="text-sm font-medium">{title}</h2>
        <p className="text-muted-foreground text-xs">{backing}</p>
      </div>
      <DemoBoundary>{children}</DemoBoundary>
    </section>
  );
}

const DEMO_MODELS = [
  { id: "codex", name: "Codex", description: "Demo model" },
  { id: "demo", name: "Demo", description: "Sample data" },
];

export function ElementCatalog() {
  return (
    <div className="bg-background h-full overflow-y-auto">
      <div className="mx-auto flex w-full max-w-[44rem] flex-col gap-6 px-4 py-6">
        <header className="flex flex-col gap-1">
          <h1 className="text-lg font-medium">Elements</h1>
          <p className="text-muted-foreground text-sm">
            Every assistant-ui registry element. The chat thread keeps the
            ACP-backed pieces. This page shows the rest with labeled demo data.
          </p>
        </header>

        <Section id="model-selector" title="Model selector" backing="Demo data">
          <ModelSelector models={DEMO_MODELS} />
        </Section>
        <Section id="voice-orb" title="Voice orb" backing="Demo data">
          <div className="h-40 w-40">
            <VoiceOrb state="listening" />
          </div>
        </Section>
        <Section
          id="shiki-highlighter"
          title="Shiki highlighter"
          backing="Demo data"
        >
          <ShikiHighlighter code={"export const answer = 42;"} language="ts" />
        </Section>
        <Section
          id="mermaid-diagram"
          title="Mermaid diagram"
          backing="ACP-backed in the thread; demo here"
        >
          <MermaidDiagram
            code={"graph TD\n  Ask --> Read\n  Read --> Answer"}
          />
        </Section>
        <Section
          id="directive-text"
          title="Directive text"
          backing="Mounted on user messages; demo chip here"
        >
          <p className="text-sm">
            <DemoDirective
              text={"Ask :agent[Search]{name=search} about the plan."}
            />
          </p>
        </Section>
        <Section id="logos" title="Model logos" backing="Demo data">
          <div className="flex items-center gap-3">
            <OpenAILogo className="size-6" />
            <ClaudeLogo className="size-6" />
            <GeminiLogo className="size-6" />
          </div>
        </Section>
        <Section id="heat-graph" title="Heat graph" backing="Demo data">
          <HeatGraph
            data={[
              { date: "2026-11-01", count: 2 },
              { date: "2026-11-02", count: 5 },
            ]}
          />
        </Section>
        <Section id="geo-map" title="Geo map" backing="Demo data">
          <GeoMap
            places={[{ id: "hq", lat: 37.77, lng: -122.42, label: "Demo pin" }]}
          />
        </Section>
        <Section id="image-gallery" title="Image gallery" backing="Demo data">
          <ImageGallery
            images={[
              {
                id: "1",
                src: "https://example.com/demo.png",
                alt: "Demo image",
              },
            ]}
          />
        </Section>
        <Section id="link-preview" title="Link preview" backing="Demo data">
          <LinkPreview
            href="https://example.com"
            title="Demo link"
            description="Sample preview card."
            siteName="Example"
          />
        </Section>
        <Section id="option-list" title="Option list" backing="Demo data">
          <OptionList
            options={[
              { id: "a", label: "Keep the change" },
              { id: "b", label: "Discard it" },
            ]}
          />
        </Section>
        <Section id="question-flow" title="Question flow" backing="Demo data">
          <QuestionFlow
            steps={[
              {
                id: "q1",
                question: "Which demo should run?",
                options: [
                  { id: "a", label: "Thread" },
                  { id: "b", label: "Gallery" },
                ],
              },
            ]}
          />
        </Section>
        <Section id="run-activity" title="Run activity" backing="Demo data">
          <RunActivity
            status="complete"
            statusLabel="Demo run finished"
            open
            onOpenChange={() => undefined}
            entries={[
              {
                id: "1",
                kind: "commentary",
                label: "Read",
                content: "Demo entry",
              },
            ]}
          >
            Demo result
          </RunActivity>
        </Section>
        <Section id="task-card" title="Task card" backing="Demo data">
          <TaskCard label="Read the workspace" state="done" meta="Demo" />
        </Section>
        <Section id="generative-ui" title="Generative UI" backing="Demo data">
          <GenerativeMarkdown
            value={"**Demo** markdown from the generative UI library."}
          />
        </Section>
        <Section
          id="thread-list-standalone"
          title="Thread list (standalone)"
          backing="Demo data"
        >
          <StandaloneThreadList
            threads={[{ id: "1", title: "Demo thread", time: "Today" }]}
            activeIndex={0}
          />
        </Section>
        <Section
          id="mcp-config"
          title="MCP config"
          backing="Demo connector, not connected"
        >
          <McpConfigDialog />
        </Section>
        <Section
          id="assistant-modal"
          title="Assistant modal"
          backing="Demo data"
        >
          <div className="relative h-24">
            <AssistantModal />
          </div>
        </Section>
        <Section
          id="assistant-sidebar"
          title="Assistant sidebar"
          backing="Demo data"
        >
          <div className="h-80 overflow-hidden rounded-md border">
            <AssistantSidebar>
              <p className="p-4 text-sm">Demo document beside the thread.</p>
            </AssistantSidebar>
          </div>
        </Section>

        {galleryEntries.map((entry) => (
          <Section
            key={entry.id}
            id={entry.id}
            title={entry.title}
            backing={entry.backing}
          >
            {entry.node}
          </Section>
        ))}
      </div>
    </div>
  );
}
