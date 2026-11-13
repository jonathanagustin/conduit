import { MarkdownTextPrimitive, type SyntaxHighlighterProps } from "@assistant-ui/react-markdown";
import { CheckIcon, CopyIcon } from "lucide-react";
import { useState, type FC } from "react";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";
import remarkGfm from "remark-gfm";

import { TooltipIconButton } from "./tooltip-icon-button.js";

const Highlighter: FC<SyntaxHighlighterProps> = ({ code, language }) => (
  <SyntaxHighlighter language={language ?? "text"} style={oneDark} PreTag="div" customStyle={{ margin: 0, borderRadius: "0.5rem" }}>
    {code}
  </SyntaxHighlighter>
);

function CodeHeader({ language, code }: { language: string | undefined; code: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex items-center justify-between px-3 py-1 text-xs text-muted-foreground">
      <span>{language ?? "text"}</span>
      <TooltipIconButton
        tip={copied ? "Copied" : "Copy code"}
        onClick={() => {
          void navigator.clipboard.writeText(code).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
          });
        }}
      >
        {copied ? <CheckIcon /> : <CopyIcon />}
      </TooltipIconButton>
    </div>
  );
}

export function MarkdownText() {
  return (
    <MarkdownTextPrimitive
      remarkPlugins={[remarkGfm]}
      className="aui-md prose prose-neutral max-w-none text-sm leading-6"
      components={{
        SyntaxHighlighter: Highlighter,
        CodeHeader,
      }}
    />
  );
}
