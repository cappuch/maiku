import type { RefObject } from "react";
import type { UIMessage } from "../types";
import { Markdown } from "./Markdown";
import { ToolCallCard } from "./ToolCallCard";

export function Transcript({
  messages,
  scrollRef,
}: {
  messages: UIMessage[];
  scrollRef: RefObject<HTMLDivElement | null>;
}) {
  return (
    <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto px-6 py-6">
      {messages.length === 0 && (
        <div className="mx-auto mt-24 max-w-lg text-center">
          <p className="text-sm text-[var(--color-muted)]">
            Open a folder, pick a model, then ask it to read, edit, or run commands.
          </p>
        </div>
      )}
      <div className="mx-auto flex max-w-3xl flex-col gap-4">
        {messages.map((m, i) => (
          <MessageRow
            key={m.toolCallId ? `tool-${m.toolCallId}` : `${m.role}-${i}`}
            message={m}
          />
        ))}
      </div>
    </div>
  );
}

function MessageRow({ message }: { message: UIMessage }) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="max-w-[85%] space-y-2 rounded-2xl rounded-br-md bg-[var(--color-panel-2)] px-4 py-2.5 text-sm leading-relaxed">
          {message.images && message.images.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {message.images.map((img, i) => (
                <img
                  key={`${img.name || "img"}-${i}`}
                  src={`data:${img.mimeType};base64,${img.data}`}
                  alt={img.name || "attachment"}
                  className="max-h-40 max-w-full rounded-lg border border-[var(--color-line)] object-contain"
                />
              ))}
            </div>
          )}
          {message.text ? <div className="whitespace-pre-wrap">{message.text}</div> : null}
        </div>
      </div>
    );
  }

  if (message.role === "tool" || message.role === "toolResult") {
    return <ToolCallCard message={message} />;
  }

  return (
    <div className="flex justify-start">
      <div className="max-w-[90%]">
        <Markdown content={message.text || ""} />
        {message.streaming && (
          <span className="ml-0.5 inline-block h-3.5 w-1.5 animate-pulse bg-[var(--color-accent)] align-middle" />
        )}
      </div>
    </div>
  );
}
