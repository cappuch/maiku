import { memo, useState, type ComponentPropsWithoutRef } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import rehypeHighlight from "rehype-highlight";
import remarkGfm from "remark-gfm";
import { Check, Copy } from "lucide-react";
import { BrowserOpenURL, ClipboardSetText } from "../../wailsjs/runtime/runtime";
import { cn } from "../lib/utils";

const REMARK_PLUGINS = [remarkGfm];
const REHYPE_PLUGINS = [rehypeHighlight];
const NO_PLUGINS: [] = [];

export const Markdown = memo(function Markdown({
  content,
  className,
  streaming = false,
}: {
  content: string;
  className?: string;
  streaming?: boolean;
}) {
  if (!content) return null;

  return (
    <div
      className={cn(
        "md-content text-sm leading-relaxed",
        streaming && "md-content-streaming",
        className,
      )}
      aria-busy={streaming || undefined}
    >
      <ReactMarkdown
        remarkPlugins={REMARK_PLUGINS}
        // Highlighting every token is expensive and can flicker. The live
        // surface still renders Markdown, then receives syntax color on finish.
        rehypePlugins={streaming ? NO_PLUGINS : REHYPE_PLUGINS}
        components={MARKDOWN_COMPONENTS}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
});

const MARKDOWN_COMPONENTS: Components = {
  a: ({ href, children, onClick, ...props }) => (
    <a
      href={href}
      target="_blank"
      rel="noreferrer noopener"
      onClick={(event) => {
        onClick?.(event);
        if (event.defaultPrevented || !href) return;
        event.preventDefault();
        openExternalURL(href);
      }}
      {...props}
    >
      {children}
    </a>
  ),
  pre: ({ children, ...props }) => <PreBlock {...props}>{children}</PreBlock>,
  code: ({ className, children, ...props }) => {
    const isBlock = typeof className === "string" && className.includes("language-");
    if (isBlock) {
      return (
        <code className={className} {...props}>
          {children}
        </code>
      );
    }
    return (
      <code className="md-inline-code" {...props}>
        {children}
      </code>
    );
  },
};

function PreBlock({ children, className, ...props }: ComponentPropsWithoutRef<"pre">) {
  const [copied, setCopied] = useState(false);
  const text = extractText(children).replace(/\n$/, "");
  const language = extractLanguage(children);

  const onCopy = async () => {
    if (!(await copyText(text))) return;
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  };

  return (
    <div className="md-pre-wrap group">
      <div className="md-code-bar">
        <span className="md-code-language">{language || "code"}</span>
        <button
          type="button"
          onClick={onCopy}
          className="md-copy"
          title={copied ? "Copied" : "Copy code"}
          aria-label={copied ? "Code copied" : "Copy code"}
        >
          {copied ? <Check size={12} /> : <Copy size={12} />}
          <span>{copied ? "Copied" : "Copy"}</span>
        </button>
      </div>
      <pre className={className} {...props}>
        {children}
      </pre>
    </div>
  );
}

export async function copyText(text: string): Promise<boolean> {
  try {
    if (await ClipboardSetText(text)) return true;
  } catch {
    // Browser preview mode does not expose the Wails runtime.
  }
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

function openExternalURL(url: string) {
  try {
    BrowserOpenURL(url);
  } catch {
    window.open(url, "_blank", "noopener,noreferrer");
  }
}

function extractLanguage(node: React.ReactNode): string {
  if (Array.isArray(node)) {
    for (const item of node) {
      const language = extractLanguage(item);
      if (language) return language;
    }
  }
  if (node && typeof node === "object" && "props" in node) {
    const props = (node as { props?: { className?: string; children?: React.ReactNode } }).props;
    const match = props?.className?.match(/(?:^|\s)language-([^\s]+)/);
    return match?.[1] || extractLanguage(props?.children);
  }
  return "";
}

function extractText(node: React.ReactNode): string {
  if (node == null || typeof node === "boolean") return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(extractText).join("");
  if (typeof node === "object" && "props" in node) {
    return extractText((node as { props?: { children?: React.ReactNode } }).props?.children);
  }
  return "";
}
