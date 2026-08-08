import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import { useState, type ComponentPropsWithoutRef } from "react";
import { Check, Copy } from "lucide-react";
import { cn } from "../lib/utils";

export function Markdown({
  content,
  className,
}: {
  content: string;
  className?: string;
}) {
  if (!content) return null;

  return (
    <div className={cn("md-content text-sm leading-relaxed", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          a: ({ href, children, ...props }) => (
            <a
              href={href}
              target="_blank"
              rel="noreferrer noopener"
              {...props}
            >
              {children}
            </a>
          ),
          pre: ({ children, ...props }) => (
            <PreBlock {...props}>{children}</PreBlock>
          ),
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
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}

function PreBlock({ children, className, ...props }: ComponentPropsWithoutRef<"pre">) {
  const [copied, setCopied] = useState(false);
  const text = extractText(children);

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1200);
    } catch {
      // clipboard unavailable
    }
  };

  return (
    <div className="md-pre-wrap group">
      <button
        type="button"
        onClick={onCopy}
        className="md-copy opacity-0 group-hover:opacity-100 focus:opacity-100"
        title="Copy"
      >
        {copied ? <Check size={12} /> : <Copy size={12} />}
      </button>
      <pre className={className} {...props}>
        {children}
      </pre>
    </div>
  );
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
