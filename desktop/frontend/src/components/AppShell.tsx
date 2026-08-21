import { useEffect, useMemo, useRef, useState, type RefObject } from "react";
import {
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  FolderOpen,
  Pencil,
  Plus,
  Settings,
  Square,
} from "lucide-react";
import type {
  APIKeyStatus,
  AppState,
  ImageAttachment,
  ModelInfo,
  SessionSummary,
  UIMessage,
  UsageTotals,
} from "../types";
import { cn, formatCacheRate, formatCost, formatTokens, greetingFor } from "../lib/utils";
import { ModelSelector } from "./ModelSelector";
import { ClickAway } from "./ClickAway";
import { useClickAway } from "./useClickAway";
import { SettingsDialog, type CodexLoginHandlers } from "./SettingsDialog";
import { Transcript } from "./Transcript";
import { Composer } from "./Composer";

type Props = {
  state: AppState;
  usage: UsageTotals;
  messages: UIMessage[];
  sessions: SessionSummary[];
  models: ModelInfo[];
  keys: APIKeyStatus[];
  streaming: boolean;
  streamingSessionIds: string[];
  streamText: string;
  streamThinking: string;
  thinkingStartedAt: number | null;
  tokensPerSec: number;
  sidebarOpen: boolean;
  settingsOpen: boolean;
  error: string | null;
  scrollRef: RefObject<HTMLDivElement | null>;
  onTranscriptScroll: () => void;
  recentDirs: string[];
  onToggleSidebar: () => void;
  onToggleSettings: () => void;
  onSend: (text: string, images: ImageAttachment[]) => Promise<boolean>;
  onCommand: (command: string) => void;
  onAbort: () => Promise<boolean>;
  onNewSession: () => void;
  onOpenFolder: () => void;
  onOpenRecentFolder: (path: string) => void;
  onOpenSession: (path: string) => void;
  onRenameSession: (path: string, name: string) => void;
  onSetModel: (provider: string, id: string) => void;
  onSetThinking: (level: string) => void;
  onSaveKey: (provider: string, key: string) => void;
  codexLogin?: CodexLoginHandlers;
  onDismissError: () => void;
};

export function AppShell(props: Props) {
  const {
    state,
    usage,
    messages,
    sessions,
    models,
    keys,
    streaming,
    streamingSessionIds,
    streamText,
    streamThinking,
    thinkingStartedAt,
    tokensPerSec,
    sidebarOpen,
    settingsOpen,
    error,
    scrollRef,
  } = props;

  const streamingIdSet = useMemo(
    () => new Set(streamingSessionIds),
    [streamingSessionIds],
  );

  // Personalized empty-state greeting — computed once per user, so the random
  // variant doesn't flicker across re-renders.
  const greeting = useMemo(() => greetingFor(state.userName), [state.userName]);
  const isMac = useMemo(() => /mac|iphone|ipad/i.test(navigator.platform), []);
  const shortcutPrefix = isMac ? "⌘" : "Ctrl+";

  const [dirMenuOpen, setDirMenuOpen] = useState(false);
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; path: string } | null>(null);
  const [editingPath, setEditingPath] = useState<string | null>(null);
  const dirMenuRef = useRef<HTMLDivElement>(null);

  // Clicking anywhere outside the folder menu closes it.
  useClickAway(dirMenuOpen, dirMenuRef, () => setDirMenuOpen(false));

  // Close popovers on Escape.
  useEffect(() => {
    if (!dirMenuOpen && !ctxMenu && !editingPath) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setDirMenuOpen(false);
      setCtxMenu(null);
      setEditingPath(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [dirMenuOpen, ctxMenu, editingPath]);

  // Desktop shortcuts keep the most common workspace actions one keystroke away.
  useEffect(() => {
    const onShortcut = (event: KeyboardEvent) => {
      const primaryModifier = isMac
        ? event.metaKey && !event.ctrlKey
        : event.ctrlKey && !event.metaKey;
      if (!primaryModifier || event.altKey) return;
      const target = event.target;
      const editing = target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || (target instanceof HTMLElement && target.isContentEditable);
      if (editing && !isMac) return;
      const key = event.key.toLowerCase();
      if (settingsOpen && key !== ",") return;
      if (key === "n") {
        event.preventDefault();
        props.onNewSession();
      } else if (key === "o") {
        event.preventDefault();
        props.onOpenFolder();
      } else if (key === "b") {
        event.preventDefault();
        props.onToggleSidebar();
      } else if (key === ",") {
        event.preventDefault();
        props.onToggleSettings();
      }
    };
    window.addEventListener("keydown", onShortcut);
    return () => window.removeEventListener("keydown", onShortcut);
  }, [props, settingsOpen, isMac]);

  const folderSessions = sessions.filter(
    (s) => !state.cwd || s.cwd === state.cwd || s.path.includes(encodeCwd(state.cwd)),
  );

  const openCtxMenu = (e: React.MouseEvent, path: string) => {
    e.preventDefault();
    setCtxMenu({
      x: Math.min(e.clientX, window.innerWidth - 190),
      y: Math.min(e.clientY, window.innerHeight - 140),
      path,
    });
  };

  const startRename = (path: string) => {
    setCtxMenu(null);
    setEditingPath(path);
  };

  const commitRename = (path: string, value: string) => {
    const name = value.trim();
    const current = sessions.find((s) => s.path === path);
    if (name !== (current?.name || "")) {
      props.onRenameSession(path, name);
    }
    setEditingPath(null);
  };

  return (
    <div className="app-shell flex h-full flex-col bg-[var(--color-ink)] text-[var(--color-text)]">
      {/* Title bar — brand/folder left, model controls right. Vertically centered. */}
      <header
        data-wails-drag
        className="titlebar-drag relative z-40 flex h-12 shrink-0 items-center justify-between border-b border-[var(--color-line)] pr-3 pl-[96px]"
      >
        <div className="titlebar-no-drag flex min-w-0 items-center gap-1.5" data-wails-no-drag>
          <div className="relative min-w-0">
            <button
              type="button"
              onClick={() => setDirMenuOpen((v) => !v)}
              className={cn(
                "flex max-w-[280px] items-center gap-1 rounded-md px-1.5 py-1 text-sm font-semibold leading-none tracking-tight transition-colors hover:bg-[var(--color-panel-2)]",
                dirMenuOpen && "bg-[var(--color-panel-2)]",
              )}
              title={state.cwd || "Open a folder"}
              aria-expanded={dirMenuOpen}
            >
              <span className="shrink-0">maiku</span>
              <span className="shrink-0 text-[var(--color-muted)]">/</span>
              <span className="truncate text-[var(--color-muted)]">
                {state.folderName || "no folder"}
              </span>
              <ChevronDown
                size={13}
                strokeWidth={2}
                className={cn(
                  "shrink-0 text-[var(--color-muted)] transition-transform",
                  dirMenuOpen && "rotate-180",
                )}
                aria-hidden
              />
            </button>
            {dirMenuOpen && (
              <div
                ref={dirMenuRef}
                className="titlebar-no-drag absolute left-0 top-full z-50 mt-1 w-80 overflow-hidden rounded-lg border border-[var(--color-line)] bg-[var(--color-panel)] py-1 shadow-xl"
              >
                  <button
                    type="button"
                    onClick={() => {
                      setDirMenuOpen(false);
                      props.onOpenFolder();
                    }}
                    className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs font-medium transition-colors hover:bg-[var(--color-panel-2)]"
                  >
                    <FolderOpen size={14} className="text-[var(--color-accent)]" />
                    Open folder…
                    <kbd className="ml-auto text-[10px] text-[var(--color-muted)]">{shortcutPrefix}O</kbd>
                  </button>
                  <div className="mx-2 border-t border-[var(--color-line)]" />
                  <p className="px-3 pt-2 pb-1 text-[10px] font-medium tracking-wide text-[var(--color-muted)]">
                    Recent folders
                  </p>
                  {state.recentDirs.length === 0 && (
                    <p className="px-3 py-2 text-xs text-[var(--color-muted)]">
                      No recent folders yet
                    </p>
                  )}
                  {state.recentDirs.map((d: string) => {
                    const active = d === state.cwd;
                    return (
                      <button
                        key={d}
                        type="button"
                        onClick={() => {
                          setDirMenuOpen(false);
                          if (!active) props.onOpenRecentFolder(d);
                        }}
                        className={cn(
                          "flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors hover:bg-[var(--color-panel-2)]",
                          active && "bg-[var(--color-panel-2)]",
                        )}
                      >
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-xs font-medium">
                            {basename(d) || d}
                          </span>
                          <span className="block truncate font-mono text-[10px] text-[var(--color-muted)]">
                            {d}
                          </span>
                        </span>
                        {active && (
                          <Check size={13} className="shrink-0 text-[var(--color-accent)]" />
                        )}
                      </button>
                    );
                  })}
                </div>
            )}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <ModelSelector
            models={models}
            provider={state.provider}
            modelId={state.modelId}
            thinking={state.thinking}
            onSetModel={props.onSetModel}
            onSetThinking={props.onSetThinking}
          />
          <button
            type="button"
            data-wails-no-drag
            onClick={props.onToggleSettings}
            className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--color-muted)] hover:bg-[var(--color-panel-2)] hover:text-[var(--color-text)]"
            title={`Settings (${shortcutPrefix},)`}
            aria-label="Open settings"
          >
            <Settings size={15} />
          </button>
        </div>
      </header>

      <div className="flex min-h-0 flex-1">
        {/* Sidebar — toggle pinned to the outer (left) edge; actions only when open. */}
        <aside
          className={cn(
            "app-sidebar flex shrink-0 flex-col overflow-hidden border-r border-[var(--color-line)] transition-[width] duration-200",
            sidebarOpen ? "w-64" : "w-12",
          )}
        >
          {/* Toggle row is always left-anchored so the collapse button stays in place. */}
          <div className="flex shrink-0 items-center gap-1 border-b border-[var(--color-line)] px-2 py-2.5">
            <IconBtn
              title={sidebarOpen ? "Collapse sidebar" : "Expand sidebar"}
              onClick={props.onToggleSidebar}
            >
              {sidebarOpen ? <ChevronLeft size={16} /> : <ChevronRight size={16} />}
            </IconBtn>
            {sidebarOpen && (
              <>
                <IconBtn title={`New session (${shortcutPrefix}N)`} onClick={props.onNewSession}>
                  <Plus size={16} />
                </IconBtn>
                <IconBtn title={`Open folder (${shortcutPrefix}O)`} onClick={props.onOpenFolder}>
                  <FolderOpen size={16} />
                </IconBtn>
              </>
            )}
          </div>
          {!sidebarOpen && (
            <div className="flex flex-col items-center gap-1 py-2">
              <IconBtn title={`New session (${shortcutPrefix}N)`} onClick={props.onNewSession}>
                <Plus size={16} />
              </IconBtn>
              <IconBtn title={`Open folder (${shortcutPrefix}O)`} onClick={props.onOpenFolder}>
                <FolderOpen size={16} />
              </IconBtn>
            </div>
          )}
          {sidebarOpen && (
            <div className="min-h-0 flex-1 overflow-y-auto p-2">
              <p className="mb-2 px-1 text-[10px] font-semibold tracking-[0.12em] text-[var(--color-muted)]">
                Sessions
              </p>
              {folderSessions.length === 0 && (
                <p className="px-1 text-xs text-[var(--color-muted)]">No sessions yet</p>
              )}
              {folderSessions.map((s) => {
                const active = s.id === state.sessionId;
                const isStreaming = streamingIdSet.has(s.id);
                const editing = editingPath === s.path;
                return (
                  <div
                    key={s.path}
                    className={cn(
                      "group relative mb-1 w-full rounded-lg transition-colors",
                      active && "bg-[var(--color-panel-2)] shadow-[inset_0_1px_rgba(255,255,255,.06)] ring-1 ring-[var(--color-line)]",
                      isStreaming && "session-streaming",
                    )}
                  >
                    {editing ? (
                      <input
                        ref={(input) => input?.focus()}
                        defaultValue={s.name || s.preview || s.id.slice(0, 8)}
                        aria-label="Session name"
                        className="w-full rounded-md border border-[var(--color-accent-dim)] bg-[var(--color-panel-2)] px-2 py-1.5 text-xs text-[var(--color-text)] outline-none"
                        onKeyDown={(e) => {
                          if (e.key === "Enter") {
                            e.currentTarget.blur();
                          } else if (e.key === "Escape") {
                            e.currentTarget.dataset.cancel = "1";
                            e.currentTarget.blur();
                          }
                        }}
                        onBlur={(e) => {
                          if (e.currentTarget.dataset.cancel) {
                            setEditingPath(null);
                            return;
                          }
                          commitRename(s.path, e.currentTarget.value);
                        }}
                      />
                    ) : (
                      <>
                        <button
                          type="button"
                          onClick={() => props.onOpenSession(s.path)}
                          onContextMenu={(e) => openCtxMenu(e, s.path)}
                          className="flex w-full items-center rounded-lg py-2 pr-9 pl-2 text-left transition-colors hover:bg-[var(--color-panel-2)]"
                          title={s.path}
                        >
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-xs font-medium">
                              {s.name || s.preview || s.id.slice(0, 8)}
                            </span>
                            <span className="mt-0.5 block truncate font-mono text-[10px] text-[var(--color-muted)]">
                              {formatTime(s.modTime || s.timestamp)}
                            </span>
                          </span>
                        </button>
                        <button
                          type="button"
                          onClick={() => startRename(s.path)}
                          className="session-rename-control absolute top-1/2 right-1.5 -translate-y-1/2 rounded-md p-1.5 text-[var(--color-muted)] opacity-0 hover:bg-white/5 hover:text-[var(--color-text)] focus:opacity-100 group-hover:opacity-100"
                          aria-label={`Rename ${s.name || s.preview || "session"}`}
                          title="Rename session"
                        >
                          <Pencil size={12} />
                        </button>
                      </>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </aside>

        {/* Main */}
        <main className="flex min-w-0 flex-1 flex-col">
          {error && (
            <div role="alert" className="flex items-center justify-between bg-[color-mix(in_srgb,var(--color-danger)_18%,transparent)] px-4 py-2 text-sm text-[var(--color-danger)]">
              <span>{error}</span>
              <button type="button" className="underline" onClick={props.onDismissError}>
                dismiss
              </button>
            </div>
          )}
          {!state.hasApiKey && (
            <button
              type="button"
              onClick={props.onToggleSettings}
              className="border-b border-[var(--color-line)] bg-[color-mix(in_srgb,var(--color-accent)_12%,transparent)] px-4 py-2 text-left text-sm text-[var(--color-accent)] hover:bg-[color-mix(in_srgb,var(--color-accent)_17%,transparent)]"
            >
              No API key for <strong>{state.provider || "provider"}</strong>. <span className="underline underline-offset-2">Open Settings</span>
            </button>
          )}

          <Transcript
            key={state.sessionId || state.cwd || "new"}
            messages={messages}
            scrollRef={scrollRef}
            onScroll={props.onTranscriptScroll}
            streamText={streamText}
            streamThinking={streamThinking}
            thinkingStartedAt={thinkingStartedAt}
            streaming={streaming}
            greeting={greeting}
            hasWorkspace={!!state.cwd}
            onOpenFolder={props.onOpenFolder}
            openFolderShortcut={`${shortcutPrefix}O`}
          />

          <Composer
            draftKey={state.sessionId || state.cwd || "new"}
            streaming={streaming}
            onSend={props.onSend}
            onCommand={props.onCommand}
            onAbort={props.onAbort}
            disabled={!state.cwd}
          />
        </main>
      </div>

      {/* Status bar */}
      <footer className="status-bar relative z-20 flex h-8 shrink-0 items-center gap-4 border-t border-[var(--color-line)] px-3 font-mono text-[11px] text-[var(--color-muted)]">
        <Stat label="in" value={formatTokens(usage.input)} />
        <Stat label="out" value={formatTokens(usage.output)} />
        <Stat label="cache" value={formatCacheRate(usage.cacheRate)} accent />
        <Stat label="total" value={formatTokens(usage.totalTokens)} />
        <Stat label="cost" value={formatCost(usage.cost)} accent />
        <Stat label="tok/s" value={formatRate(tokensPerSec)} />
        <span className="ml-auto truncate">{state.cwd || "open a folder to begin"}</span>
        {streaming && (
          <span className="flex items-center gap-1 text-[var(--color-accent)]">
            <Square size={10} className="animate-pulse" fill="currentColor" />
            working
          </span>
        )}
      </footer>

      {ctxMenu && (
        <>
          <ClickAway
            onClose={() => setCtxMenu(null)}
            onContextMenu={(e) => {
              e.preventDefault();
              setCtxMenu(null);
            }}
          />
          <div
            data-wails-no-drag
            className="titlebar-no-drag fixed z-50 w-44 overflow-hidden rounded-lg border border-[var(--color-line)] bg-[var(--color-panel)] py-1 shadow-xl"
            style={{ left: ctxMenu.x, top: ctxMenu.y }}
          >
            <p className="px-3 pt-1 pb-1 font-mono text-[10px] text-[var(--color-muted)]">
              {basename(ctxMenu.path) || "session"}
            </p>
            <button
              type="button"
              onClick={() => startRename(ctxMenu.path)}
              className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors hover:bg-[var(--color-panel-2)]"
            >
              <Pencil size={12} />
              Rename…
            </button>
          </div>
        </>
      )}

      {settingsOpen && (
        <SettingsDialog
          keys={keys}
          onSave={props.onSaveKey}
          onClose={props.onToggleSettings}
          codexLogin={props.codexLogin}
        />
      )}
    </div>
  );
}

function IconBtn({
  children,
  onClick,
  title,
}: {
  children: React.ReactNode;
  onClick: () => void;
  title: string;
}) {
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      onClick={onClick}
      className="rounded-md p-1.5 text-[var(--color-muted)] hover:bg-[var(--color-panel-2)] hover:text-[var(--color-text)]"
    >
      {children}
    </button>
  );
}

function Stat({
  label,
  value,
  accent,
}: {
  label: string;
  value: string;
  accent?: boolean;
}) {
  return (
    <span>
      <span className="mr-1 opacity-60">{label}</span>
      <span className={accent ? "text-[var(--color-accent)]" : "text-[var(--color-text)]"}>
        {value}
      </span>
    </span>
  );
}

function formatRate(r: number) {
  if (!Number.isFinite(r) || r <= 0) return "0";
  return r >= 100 ? Math.round(r).toString() : r.toFixed(1);
}

function formatTime(iso: string) {
  if (!iso) return "";
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return iso;
  }
}

function encodeCwd(cwd: string) {
  return cwd.replace(/^[/\\]/, "").replace(/[/\\:]/g, "-");
}

function basename(p: string) {
  const parts = p.split(/[/\\]/).filter(Boolean);
  return parts[parts.length - 1] || p;
}
