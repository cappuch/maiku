import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";
import { ArrowDown, Check, ChevronDown, Copy, FolderOpen, Pencil } from "lucide-react";
import type { UIMessage } from "../types";
import { cn } from "../lib/utils";
import { Markdown, copyText } from "./Markdown";
import { ThinkingLive } from "./ThinkingLive";
import { ToolCallCard } from "./ToolCallCard";
import { LoadingGrid } from "./LoadingGrid";
import { StreamingText } from "./StreamingText";

const JUMP_THRESHOLD = 48;
const HISTORY_PAGE_SIZE = 120;

export function Transcript({
  messages,
  scrollRef,
  onScroll,
  onReleaseFollow,
  onPinFollow,
  programmaticScrollRef,
  streamText,
  streamThinking,
  thinkingStartedAt,
  streaming,
  hasWorkspace = true,
  onOpenFolder,
  openFolderShortcut = "⌘O",
  onEditMessage,
  lastError,
  onDismissError,
}: {
  messages: UIMessage[];
  scrollRef: RefObject<HTMLDivElement | null>;
  onScroll?: () => void;
  onReleaseFollow?: () => void;
  onPinFollow?: () => void;
  programmaticScrollRef?: RefObject<boolean>;
  streamText?: string;
  streamThinking?: string;
  thinkingStartedAt?: number | null;
  streaming?: boolean;
  hasWorkspace?: boolean;
  onOpenFolder?: () => void;
  openFolderShortcut?: string;
  onEditMessage?: (rawIndex: number, text: string) => void;
  lastError?: string | null;
  onDismissError?: () => void;
}) {
  const [showJump, setShowJump] = useState(false);
  const [completionAnnouncement, setCompletionAnnouncement] = useState("");
  const [historyLimit, setHistoryLimit] = useState(HISTORY_PAGE_SIZE);
  const prependScrollRef = useRef<{ height: number; top: number } | null>(null);
  const loadingEarlierRef = useRef(false);
  const wasStreaming = useRef(!!streaming);
  const touchYRef = useRef<number | null>(null);
  const prevScrollTopRef = useRef(0);
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

  const visibleStart = Math.max(0, messages.length - historyLimit);

  const showEarlier = useCallback(() => {
    const element = scrollRef.current;
    if (!element || loadingEarlierRef.current || visibleStart === 0) return;
    loadingEarlierRef.current = true;
    prependScrollRef.current = { height: element.scrollHeight, top: element.scrollTop };
    setHistoryLimit((current) => current + HISTORY_PAGE_SIZE);
  }, [scrollRef, visibleStart]);

  const handleScroll = () => {
    const element = scrollRef.current;
    if (!element) return;
    const top = element.scrollTop;
    const distance = element.scrollHeight - element.clientHeight - top;
    setShowJump(distance > JUMP_THRESHOLD);
    const userMovedUp = !programmaticScrollRef?.current && top < prevScrollTopRef.current - 1;
    prevScrollTopRef.current = top;
    if (userMovedUp && top < 320 && visibleStart > 0) showEarlier();
    onScroll?.();
  };

  const jumpToLatest = () => {
    const element = scrollRef.current;
    if (!element) return;
    if (programmaticScrollRef) programmaticScrollRef.current = true;
    element.scrollTop = element.scrollHeight;
    prevScrollTopRef.current = element.scrollTop;
    setShowJump(false);
    onPinFollow?.();
    requestAnimationFrame(() => {
      if (programmaticScrollRef) programmaticScrollRef.current = false;
    });
  };

  useLayoutEffect(() => {
    const pending = prependScrollRef.current;
    const element = scrollRef.current;
    if (!pending || !element) return;
    if (programmaticScrollRef) programmaticScrollRef.current = true;
    const nextTop = pending.top + element.scrollHeight - pending.height;
    element.scrollTop = nextTop;
    prevScrollTopRef.current = element.scrollTop;
    prependScrollRef.current = null;
    loadingEarlierRef.current = false;
    requestAnimationFrame(() => {
      if (programmaticScrollRef) programmaticScrollRef.current = false;
    });
  });

  // Stream text changes frequently while finalized history usually does not.
  // Reuse the historical element tree instead of remapping a long transcript
  // for every generated chunk.
  const renderedMessages = useMemo(() => {
      const render = (items: UIMessage[], offset: number) =>
        items.map((message, index) => (
        <MessageRow
          key={message.id || (message.toolCallId ? `tool-${message.toolCallId}` : `${message.role}-${offset + index}`)}
          message={message}
          onEditMessage={onEditMessage}
          canEdit={!streaming && typeof message.rawIndex === "number"}
        />
      ));
    const visibleActivityStart = Math.max(visibleStart, liveActivityStart);
    return {
      before: render(messages.slice(visibleStart, visibleActivityStart), visibleStart),
      after: render(messages.slice(visibleActivityStart), visibleActivityStart),
    };
  }, [messages, liveActivityStart, visibleStart, onEditMessage, streaming]);

  const isEmpty = messages.length === 0 && !showThinking && !showStream && !streaming;

  return (
    <div className="relative min-h-0 flex-1">
      <span className="sr-only" role="status" aria-live="polite">{completionAnnouncement}</span>
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        onWheel={(event) => {
          const element = scrollRef.current;
          if (!element || event.deltaY >= 0 || element.scrollTop <= 0) return;
          onReleaseFollow?.();
        }}
        onTouchStart={(event) => {
          touchYRef.current = event.touches[0]?.clientY ?? null;
        }}
        onTouchMove={(event) => {
          const y = event.touches[0]?.clientY;
          if (y == null || touchYRef.current == null) return;
          if (y > touchYRef.current + 4) onReleaseFollow?.();
          touchYRef.current = y;
        }}
        className="transcript h-full overflow-y-auto px-6 py-7"
      >
        {isEmpty && !hasWorkspace && onOpenFolder ? (
          <div className="empty-state mx-auto mt-[16vh] max-w-xl text-center">
            <button type="button" className="empty-primary" onClick={onOpenFolder}>
              <FolderOpen size={15} />
              Open folder
              <kbd>{openFolderShortcut}</kbd>
            </button>
          </div>
        ) : null}
        <div className="mx-auto flex max-w-[760px] flex-col gap-5">
          {visibleStart > 0 ? (
            <button
              type="button"
              className="mx-auto rounded-full border border-[var(--color-line)] bg-[var(--color-panel)] px-3 py-1.5 text-xs text-[var(--color-muted)] hover:border-[var(--color-accent-dim)] hover:text-[var(--color-text)]"
              onClick={showEarlier}
            >
              Load {Math.min(HISTORY_PAGE_SIZE, visibleStart)} earlier messages
            </button>
          ) : null}
          {renderedMessages.before}
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
              <div className="assistant-message w-full">
                <StreamingText content={streamText ?? ""} />
              </div>
            </div>
          )}
          {renderedMessages.after}
          {lastError ? (
            <div role="alert" className="flex items-start justify-between gap-3 rounded-xl border border-[color-mix(in_srgb,var(--color-danger)_28%,transparent)] bg-[color-mix(in_srgb,var(--color-danger)_8%,transparent)] px-3 py-2.5 text-sm text-[var(--color-danger)]">
              <p className="min-w-0 flex-1 whitespace-pre-wrap">{lastError}</p>
              {onDismissError ? (
                <button type="button" className="shrink-0 underline" onClick={onDismissError}>
                  dismiss
                </button>
              ) : null}
            </div>
          ) : null}
        </div>
      </div>

      {showJump && (
        <button
          type="button"
          className="jump-latest"
          onClick={jumpToLatest}
          aria-label="Jump to latest message"
          title="Latest"
        >
          <ArrowDown size={15} />
        </button>
      )}
    </div>
  );
}

const COLLAPSED_LINES = 6;

function UserMessageText({ text }: { text: string }) {
  const bodyRef = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);

  useLayoutEffect(() => {
    const el = bodyRef.current;
    if (!el || text.length === 0) return;
    const lineHeight = Number.parseFloat(getComputedStyle(el).lineHeight) || 23;
    const previous = el.style.maxHeight;
    el.style.maxHeight = "none";
    const full = el.scrollHeight;
    el.style.maxHeight = previous;
    setOverflows(full > lineHeight * COLLAPSED_LINES + 1);
  }, [text]);

  const clamped = overflows && !expanded;

  return (
    <div>
      <div className="relative">
        <div ref={bodyRef} className={cn("user-message-text", clamped && "is-clamped")}>
          {text}
        </div>
        {clamped ? <div className="user-message-fade" /> : null}
      </div>
      {overflows ? (
        <button
          type="button"
          className="mt-1.5 inline-flex items-center gap-1 text-[12px] text-[var(--color-muted)] hover:text-[var(--color-text)]"
          aria-expanded={expanded}
          onClick={() => setExpanded((open) => !open)}
        >
          {expanded ? "Show less" : "Read more"}
          <ChevronDown size={13} className={cn("transition-transform duration-150", expanded && "rotate-180")} />
        </button>
      ) : null}
    </div>
  );
}

function UserImages({ message }: { message: UIMessage }) {
  if (!message.images || message.images.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-2">
      {message.images.map((image) => (
        <img
          key={`${image.name || "img"}-${image.mimeType}-${image.data.slice(0, 32)}`}
          src={`data:${image.mimeType};base64,${image.data}`}
          alt={image.name || "attachment"}
          className="max-h-40 max-w-full rounded-lg border border-[var(--color-line)] object-contain"
          loading="lazy"
        />
      ))}
    </div>
  );
}

function UserTurn({
  message,
  canEdit,
  onEditMessage,
}: {
  message: UIMessage;
  canEdit?: boolean;
  onEditMessage?: (rawIndex: number, text: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(message.text || "");
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const editable = !!canEdit && typeof message.rawIndex === "number" && !!onEditMessage;
  const canSubmit = draft.trim().length > 0 || (message.images?.length ?? 0) > 0;

  useLayoutEffect(() => {
    if (!editing) return;
    const el = inputRef.current;
    if (!el) return;
    el.focus();
    const end = el.value.length;
    el.setSelectionRange(end, end);
  }, [editing]);

  useLayoutEffect(() => {
    const el = inputRef.current;
    if (!el || !editing) return;
    const lineCount = draft.split("\n").length;
    el.style.height = "auto";
    el.style.height = `${Math.min(Math.max(el.scrollHeight, lineCount), 280)}px`;
  }, [editing, draft]);

  const cancel = () => {
    setDraft(message.text || "");
    setEditing(false);
  };

  const submit = () => {
    if (!canSubmit || typeof message.rawIndex !== "number" || !onEditMessage) return;
    onEditMessage(message.rawIndex, draft.trim());
    setEditing(false);
  };

  if (editing) {
    return (
      <div className="user-turn flex w-full min-w-0 justify-end">
        <div className="user-message is-editing space-y-2 px-4 py-2.5 text-sm leading-relaxed">
          <UserImages message={message} />
          <textarea
            ref={inputRef}
            value={draft}
            rows={1}
            aria-label="Edit message"
            className="user-edit-input w-full resize-none bg-transparent text-sm leading-relaxed text-[var(--color-text)] outline-none"
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                event.preventDefault();
                cancel();
                return;
              }
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                submit();
              }
            }}
          />
          <div className="flex items-center justify-end gap-1.5">
            <button
              type="button"
              className="rounded-lg px-2.5 py-1 text-[12px] text-[var(--color-muted)] hover:bg-white/[0.06] hover:text-[var(--color-text)]"
              onClick={cancel}
            >
              Cancel
            </button>
            <button
              type="button"
              className="rounded-lg bg-zinc-100 px-2.5 py-1 text-[12px] font-medium text-zinc-950 hover:bg-white disabled:opacity-40"
              disabled={!canSubmit}
              onClick={submit}
            >
              Send
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="user-turn flex w-full min-w-0 justify-end">
      <div className="user-message space-y-2 px-4 py-2.5 text-sm leading-relaxed">
        {editable ? (
          <button type="button" className="user-edit" title="Edit" aria-label="Edit message" onClick={() => {
            setDraft(message.text || "");
            setEditing(true);
          }}>
            <Pencil size={13} />
          </button>
        ) : null}
        <UserImages message={message} />
        {message.text ? <UserMessageText text={message.text} /> : null}
      </div>
    </div>
  );
}

const MessageRow = memo(function MessageRow({
  message,
  onEditMessage,
  canEdit,
}: {
  message: UIMessage;
  onEditMessage?: (rawIndex: number, text: string) => void;
  canEdit?: boolean;
}) {
  let content: ReactNode;
  if (message.role === "notice") {
    content = (
      <div className="chat-notice" role="status">
        <span className="chat-notice-rule" aria-hidden />
        <span className="chat-notice-text">{message.text || "Done"}</span>
        <span className="chat-notice-rule" aria-hidden />
      </div>
    );
  } else if (message.role === "user") {
    content = <UserTurn message={message} canEdit={canEdit} onEditMessage={onEditMessage} />;
  } else if (message.role === "tool" || message.role === "toolResult") {
    content = <ToolCallCard message={message} />;
  } else if (message.isError) {
    content = (
      <div className="flex justify-start">
        <div className="w-full max-w-[90%] rounded-lg border border-[color-mix(in_srgb,var(--color-danger)_35%,transparent)] bg-[color-mix(in_srgb,var(--color-danger)_10%,transparent)] px-3 py-2 text-sm text-[var(--color-danger)]">
          {message.text || "Request failed"}
        </div>
      </div>
    );
  } else {
    content = (
      <>
        {message.thinking ? <ThinkingLive thinking={message.thinking} live={false} /> : null}
        {(message.text || message.streaming) && (
          <div className="assistant-response group flex justify-start">
            <div className="assistant-message w-full">
              <Markdown content={message.text || ""} streaming={message.streaming} />
              {message.text && !message.streaming ? <ResponseActions text={message.text} /> : null}
            </div>
          </div>
        )}
      </>
    );
  }

  return (
    <div
      className={
        message.role === "notice"
          ? "transcript-row transcript-row-notice"
          : "transcript-row flex flex-col gap-5"
      }
    >
      {content}
    </div>
  );
});

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
