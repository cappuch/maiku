import { useEffect, useRef, useState, type RefObject } from "react";
import { ArrowDown, Check, Copy, FolderOpen } from "lucide-react";
import type { UIMessage } from "../types";
import { Markdown, copyText } from "./Markdown";
import { ThinkingLive } from "./ThinkingLive";
import { ToolCallCard } from "./ToolCallCard";
import { LoadingGrid } from "./LoadingGrid";
import { StreamingText } from "./StreamingText";

const JUMP_THRESHOLD = 48;

export function Transcript({
  messages,
  scrollRef,
  onScroll,
  streamText,
  streamThinking,
  thinkingStartedAt,
  streaming,
  greeting,
  hasWorkspace = true,
  onOpenFolder,
  openFolderShortcut = "⌘O",
}: {
  messages: UIMessage[];
  scrollRef: RefObject<HTMLDivElement | null>;
  onScroll?: () => void;
  streamText?: string;
  streamThinking?: string;
  thinkingStartedAt?: number | null;
  streaming?: boolean;
  greeting?: string;
  hasWorkspace?: boolean;
  onOpenFolder?: () => void;
  openFolderShortcut?: string;
}) {
  const [showJump, setShowJump] = useState(false);
  const [completionAnnouncement, setCompletionAnnouncement] = useState("");
  const wasStreaming = useRef(!!streaming);
  const showStream = !!streamText?.length;
  const showThinking = !!streamThinking?.trim();

  useEffect(() => {
    if (wasStreaming.current && !streaming) {
      setCompletionAnnouncement("Response complete");
    } else if (streaming) {
      setCompletionAnnouncement("");
    }
    wasStreaming.current = !!streaming;
  }, [streaming]);

  // A partial assistant message can expose tool calls before message_end.
  // Keep the live answer in the same chronological position it will occupy
  // when finalized, immediately before its trailing in-flight tool cards.
  let liveActivityStart = messages.length;
  if (showThinking || showStream) {
    while (liveActivityStart > 0) {
      const message = messages[liveActivityStart - 1];
      if (
        !message.streaming ||
        (message.role !== "tool" && message.role !== "toolResult")
      ) {
        break;
      }
      liveActivityStart -= 1;
    }
  }

  const handleScroll = () => {
    const element = scrollRef.current;
    if (element) {
      const distance = element.scrollHeight - element.clientHeight - element.scrollTop;
      setShowJump(distance > JUMP_THRESHOLD);
    }
    onScroll?.();
  };

  const jumpToLatest = () => {
    const element = scrollRef.current;
    if (!element) return;
    element.scrollTop = element.scrollHeight;
    setShowJump(false);
    onScroll?.();
  };

  const renderMessages = (items: UIMessage[], offset = 0) =>
    items.map((message, index) => (
      <MessageRow
        key={message.id || (message.toolCallId ? `tool-${message.toolCallId}` : `${message.role}-${offset + index}`)}
        message={message}
      />
    ));

  const isEmpty = messages.length === 0 && !showThinking && !showStream && !streaming;

  return (
    <div className="relative min-h-0 flex-1">
      <span className="sr-only" role="status" aria-live="polite">{completionAnnouncement}</span>
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="transcript h-full overflow-y-auto px-6 py-7"
      >
        {isEmpty && (
          <div className="empty-state mx-auto mt-[16vh] max-w-xl text-center">
            <p className="empty-greeting">{greeting || "Hey there"}</p>
            <p className="empty-subtitle">
              {hasWorkspace
                ? "Ask a question, plan a change, or point me at a file."
                : "Open a project and we’ll get to work."}
            </p>
            {!hasWorkspace && onOpenFolder ? (
              <button type="button" className="empty-primary" onClick={onOpenFolder}>
                <FolderOpen size={15} />
                Open folder
                <kbd>{openFolderShortcut}</kbd>
              </button>
            ) : null}
          </div>
        )}
        <div className="mx-auto flex max-w-[760px] flex-col gap-5">
          {renderMessages(messages.slice(0, liveActivityStart))}
          {showThinking && (
            <ThinkingLive
              thinking={streamThinking ?? ""}
              startedAt={thinkingStartedAt ?? null}
              live={!showStream}
            />
          )}
          {streaming && !showThinking && !showStream && <LoadingGrid />}
          {showStream && (
            <div className="flex justify-start" aria-live="off">
              <div className="assistant-message w-full max-w-[90%]">
                <StreamingText content={streamText ?? ""} />
              </div>
            </div>
          )}
          {renderMessages(messages.slice(liveActivityStart), liveActivityStart)}
        </div>
      </div>

      {showJump && (
        <button
          type="button"
          className="jump-latest"
          onClick={jumpToLatest}
          aria-label="Jump to latest message"
        >
          <ArrowDown size={14} />
          Latest
        </button>
      )}
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
              {message.images.map((image) => (
                <img
                  key={`${image.name || "img"}-${image.mimeType}-${image.data.slice(0, 32)}`}
                  src={`data:${image.mimeType};base64,${image.data}`}
                  alt={image.name || "attachment"}
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
      {message.thinking ? <ThinkingLive thinking={message.thinking} live={false} /> : null}
      {(message.text || message.streaming) && (
        <div className="assistant-response group flex justify-start">
          <div className="assistant-message w-full max-w-[90%]">
            <Markdown content={message.text || ""} streaming={message.streaming} />
            {message.text && !message.streaming ? <ResponseActions text={message.text} /> : null}
          </div>
        </div>
      )}
    </>
  );
}

function ResponseActions({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    if (!(await copyText(text))) return;
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  };

  return (
    <div className="response-actions">
      <button
        type="button"
        onClick={copy}
        className="response-action"
        aria-label={copied ? "Response copied" : "Copy response"}
        title={copied ? "Copied" : "Copy response"}
      >
        {copied ? <Check size={13} /> : <Copy size={13} />}
        <span>{copied ? "Copied" : "Copy"}</span>
      </button>
    </div>
  );
}
