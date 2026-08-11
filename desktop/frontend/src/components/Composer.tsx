import { useEffect, useRef, useState, type ClipboardEvent } from "react";
import { AtSign, Command, Paperclip, Send, Square, X } from "lucide-react";
import { CompletePath, PickFiles } from "../../wailsjs/go/main/App";
import type { ImageAttachment, PathSuggestion } from "../types";

export type ComposerAttachment = ImageAttachment & { id: string };

let attachmentSeq = 0;
function nextAttachmentId() {
  attachmentSeq += 1;
  return `att-${attachmentSeq}`;
}

function quotePathIfNeeded(path: string) {
  return /\s/.test(path) ? `"${path}"` : path;
}

/** Find the active @mention prefix ending at cursor. */
function atPrefixAt(text: string, cursor: number): { start: number; query: string } | null {
  const before = text.slice(0, cursor);
  const match = before.match(/@(?:"[^"]*|[^"\s]*)$/);
  if (!match || match.index === undefined) return null;
  return { start: match.index, query: match[0].slice(1) };
}

async function fileToAttachment(file: File): Promise<ComposerAttachment | null> {
  if (!file.type.startsWith("image/")) return null;
  const buf = await file.arrayBuffer();
  const bytes = new Uint8Array(buf);
  let binary = "";
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
  return {
    id: nextAttachmentId(),
    mimeType: file.type || "image/png",
    data: btoa(binary),
    name: file.name || "paste.png",
  };
}

export function Composer({
  streaming,
  disabled,
  onSend,
  onAbort,
}: {
  streaming: boolean;
  disabled?: boolean;
  onSend: (text: string, images: ImageAttachment[]) => void;
  onAbort: () => void;
}) {
  const [value, setValue] = useState("");
  const [attachments, setAttachments] = useState<ComposerAttachment[]>([]);
  const [suggestions, setSuggestions] = useState<PathSuggestion[]>([]);
  const [suggestIndex, setSuggestIndex] = useState(0);
  const [atRange, setAtRange] = useState<{ start: number; end: number } | null>(null);
  const ref = useRef<HTMLTextAreaElement>(null);
  const suggestReq = useRef(0);
  const suggestListRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 180) + "px";
  }, [value]);

  // Keep the arrow-key-highlighted suggestion visible inside the scroll list,
  // with extra bottom padding so the next file is also peekable.
  useEffect(() => {
    const list = suggestListRef.current;
    if (!list || suggestions.length === 0) return;
    const item = list.children[suggestIndex] as HTMLElement | undefined;
    if (!item) return;
    const itemTop = item.offsetTop;
    const itemBottom = itemTop + item.offsetHeight;
    const pad = 24;
    if (itemTop < list.scrollTop) {
      list.scrollTop = itemTop;
    } else if (itemBottom + pad > list.scrollTop + list.clientHeight) {
      list.scrollTop = itemBottom + pad - list.clientHeight;
    }
  }, [suggestIndex, suggestions]);

  const refreshSuggestions = async (text: string, cursor: number) => {
    const prefix = atPrefixAt(text, cursor);
    if (!prefix || disabled) {
      setSuggestions([]);
      setAtRange(null);
      return;
    }
    const req = ++suggestReq.current;
    try {
      const items = (await CompletePath(prefix.query)) || [];
      if (req !== suggestReq.current) return;
      setSuggestions(items);
      setSuggestIndex(0);
      setAtRange({ start: prefix.start, end: cursor });
    } catch {
      if (req !== suggestReq.current) return;
      setSuggestions([]);
      setAtRange(null);
    }
  };

  const applySuggestion = (item: PathSuggestion) => {
    const el = ref.current;
    if (!el || !atRange) return;
    const before = value.slice(0, atRange.start);
    const after = value.slice(atRange.end);
    const insert = item.isDirectory ? item.value : `${item.value} `;
    const next = before + insert + after;
    const cursor = before.length + insert.length;
    setValue(next);
    setSuggestions([]);
    setAtRange(null);
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(cursor, cursor);
    });
  };

  const addImages = (imgs: ComposerAttachment[]) => {
    if (imgs.length === 0) return;
    setAttachments((prev) => [...prev, ...imgs]);
  };

  const onPaste = async (e: ClipboardEvent<HTMLTextAreaElement>) => {
    const items = Array.from(e.clipboardData?.items || []);
    const imageItems = items.filter((it) => it.type.startsWith("image/"));
    if (imageItems.length === 0) return;
    e.preventDefault();
    const files = imageItems
      .map((it) => it.getAsFile())
      .filter((f): f is File => !!f);
    const attached = (await Promise.all(files.map(fileToAttachment))).filter(
      (a): a is ComposerAttachment => !!a,
    );
    addImages(attached);
  };

  const onPickFiles = async () => {
    if (disabled || streaming) return;
    try {
      const picked = (await PickFiles()) || [];
      const imgs: ComposerAttachment[] = [];
      const mentions: string[] = [];
      for (const f of picked) {
        if (f.isImage && f.data && f.mimeType) {
          imgs.push({
            id: nextAttachmentId(),
            mimeType: f.mimeType,
            data: f.data,
            name: f.name,
          });
        } else if (f.relPath) {
          mentions.push("@" + quotePathIfNeeded(f.relPath));
        }
      }
      addImages(imgs);
      if (mentions.length > 0) {
        setValue((prev) => {
          const sep = prev && !prev.endsWith(" ") && !prev.endsWith("\n") ? " " : "";
          return prev + sep + mentions.join(" ") + " ";
        });
        ref.current?.focus();
      }
    } catch {
      // dialog cancelled / unavailable
    }
  };

  const submit = () => {
    const text = value.trim();
    if ((!text && attachments.length === 0) || streaming || disabled) return;
    const images = attachments.map(({ mimeType, data, name }) => ({ mimeType, data, name }));
    setValue("");
    setAttachments([]);
    setSuggestions([]);
    setAtRange(null);
    onSend(text, images);
  };

  const canSend = (!!value.trim() || attachments.length > 0) && !disabled;

  return (
    <div className="composer-dock relative z-30 px-5 pt-3 pb-4">
      <div className="relative mx-auto max-w-[760px]">
        {suggestions.length > 0 && (
          <div
            ref={suggestListRef}
            className="composer-suggestions absolute bottom-full left-0 right-0 z-20 mb-2 max-h-52 overflow-y-auto py-1"
          >
            {suggestions.map((s, i) => (
              <button
                key={s.value + s.label}
                type="button"
                onMouseDown={(e) => {
                  e.preventDefault();
                  applySuggestion(s);
                }}
                className={
                  "flex w-full items-center gap-2 px-3 py-1.5 text-left font-mono text-xs " +
                  (i === suggestIndex
                    ? "bg-[var(--color-accent)]/14 text-[var(--color-text)]"
                    : "text-[var(--color-muted)] hover:bg-white/[.045]")
                }
              >
                <span className="truncate">{s.label}</span>
                {s.isDirectory && <span className="ml-auto text-[10px] opacity-60">dir</span>}
              </button>
            ))}
          </div>
        )}

        <div className="composer-shell">
          {attachments.length > 0 && (
            <div className="flex flex-wrap gap-2 border-b border-[var(--color-line)] px-3 pt-3 pb-2">
              {attachments.map((att) => (
                <div
                  key={att.id}
                  className="group relative h-14 w-14 overflow-hidden rounded-xl border border-[var(--color-line)]"
                >
                  <img
                    src={`data:${att.mimeType};base64,${att.data}`}
                    alt={att.name || "attachment"}
                    className="h-full w-full object-cover"
                  />
                  <button
                    type="button"
                    title="Remove"
                    onClick={() => setAttachments((prev) => prev.filter((a) => a.id !== att.id))}
                    className="absolute top-0.5 right-0.5 rounded bg-black/70 p-0.5 text-white opacity-0 transition-opacity group-hover:opacity-100"
                  >
                    <X size={10} />
                  </button>
                </div>
              ))}
            </div>
          )}

          <div className="flex items-end gap-1.5 px-2.5 pt-2.5 pb-1.5">
            <button
              type="button"
              onClick={onPickFiles}
              disabled={disabled || streaming}
              className="composer-icon-button mb-0.5"
              title="Attach files"
            >
              <Paperclip size={16} />
            </button>
            <textarea
              ref={ref}
              rows={1}
              value={value}
              disabled={disabled}
              placeholder={
                disabled
                  ? "Open a folder to start…"
                  : "Message maiku… (@file to inject, paste images)"
              }
              onChange={(e) => {
                const next = e.target.value;
                setValue(next);
                void refreshSuggestions(next, e.target.selectionStart ?? next.length);
              }}
              onClick={(e) => {
                const el = e.currentTarget;
                void refreshSuggestions(el.value, el.selectionStart ?? el.value.length);
              }}
              onKeyUp={(e) => {
                if (["ArrowUp", "ArrowDown", "Enter", "Tab", "Escape"].includes(e.key)) return;
                const el = e.currentTarget;
                void refreshSuggestions(el.value, el.selectionStart ?? el.value.length);
              }}
              onPaste={onPaste}
              onKeyDown={(e) => {
                if (suggestions.length > 0 && atRange) {
                  if (e.key === "ArrowDown") {
                    e.preventDefault();
                    setSuggestIndex((i) => (i + 1) % suggestions.length);
                    return;
                  }
                  if (e.key === "ArrowUp") {
                    e.preventDefault();
                    setSuggestIndex((i) => (i - 1 + suggestions.length) % suggestions.length);
                    return;
                  }
                  if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey)) {
                    e.preventDefault();
                    applySuggestion(suggestions[suggestIndex] || suggestions[0]);
                    return;
                  }
                  if (e.key === "Escape") {
                    e.preventDefault();
                    setSuggestions([]);
                    setAtRange(null);
                    return;
                  }
                }
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  submit();
                }
              }}
              className="max-h-[180px] min-h-[28px] flex-1 resize-none bg-transparent py-1.5 text-[14px] leading-6 outline-none placeholder:text-[var(--color-muted)] disabled:opacity-50"
            />
            {streaming ? (
              <button
                type="button"
                onClick={onAbort}
                className="mb-0.5 rounded-xl bg-[var(--color-danger)]/15 p-2.5 text-[var(--color-danger)] hover:bg-[var(--color-danger)]/25"
                title="Stop"
              >
                <Square size={14} fill="currentColor" />
              </button>
            ) : (
              <button
                type="button"
                onClick={submit}
                disabled={!canSend}
                className="composer-send mb-0.5"
                title="Send"
              >
                <Send size={14} />
              </button>
            )}
          </div>
          <div className="flex items-center justify-between px-3 pb-2 text-[10px] text-[var(--color-muted)]">
            <span className="flex items-center gap-2.5">
              <span className="flex items-center gap-1"><AtSign size={10} /> files</span>
              <span className="flex items-center gap-1"><Command size={10} /> actions</span>
            </span>
            <span>↵ send <span className="opacity-55">·</span> ⇧↵ new line</span>
          </div>
        </div>
      </div>
    </div>
  );
}
