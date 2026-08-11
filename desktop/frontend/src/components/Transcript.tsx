import type { RefObject } from "react";
import type { UIMessage } from "../types";
import { Markdown } from "./Markdown";
import { ThinkingLive } from "./ThinkingLive";
import { ToolCallCard } from "./ToolCallCard";
import { LoadingGrid } from "./LoadingGrid";
import { StreamingText } from "./StreamingText";

export function Transcript({
  messages,
  scrollRef,
  streamText,
  streamThinking,
  thinkingStartedAt,
  streaming,
  greeting,
}: {
  messages: UIMessage[];
  scrollRef: RefObject<HTMLDivElement | null>;
  streamText?: string;
  streamThinking?: string;
  thinkingStartedAt?: number | null;
  streaming?: boolean;
  greeting?: string;
}) {
  const showStream = !!(streamText && streamText.length > 0);
  // Show the live thinking panel while reasoning is streaming and before
  // visible response text arrives.
  const showThinking = !!(streamThinking && streamThinking.trim()) && !showStream;

  return (
    <div ref={scrollRef} className="transcript min-h-0 flex-1 overflow-y-auto px-6 py-7">
      {messages.length === 0 && !showThinking && !showStream && !streaming && (
        <div className="empty-state mx-auto mt-[16vh] max-w-xl text-center">
          <p className="empty-greeting">{greeting || "Hey there"}</p>
        </div>
      )}
      <div className="mx-auto flex max-w-[760px] flex-col gap-5">
        {messages.map((m, i) => (
          <MessageRow
            key={m.toolCallId ? `tool-${m.toolCallId}` : `${m.role}-${i}`}
            message={m}
          />
        ))}
        {showThinking && (
          <ThinkingLive thinking={streamThinking!} startedAt={thinkingStartedAt ?? null} />
        )}
        {streaming && !showThinking && !showStream && <LoadingGrid />}
        {showStream && (
          <div className="flex justify-start">
            <div className="assistant-message max-w-[90%]">
              <StreamingText content={streamText!} />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function MessageRow({ message }: { message: UIMessage }) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="user-message max-w-[85%] space-y-2 px-4 py-2.5 text-sm leading-relaxed">
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
    <>
      {message.thinking ? (
        <ThinkingLive thinking={message.thinking} live={false} />
      ) : null}
      <div className="flex justify-start">
        <div className="assistant-message max-w-[90%]">
          <Markdown content={message.text || ""} />
          {message.streaming && (
            <span className="ml-0.5 inline-block h-3.5 w-1.5 animate-pulse bg-[var(--color-accent)] align-middle" />
          )}
        </div>
      </div>
    </>
  );
}
