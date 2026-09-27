import { useEffect, useMemo, useRef, useState, type RefObject } from "react";
import {
  Check,
  ChevronDown,
  FolderOpen,
  Pencil,
  Plus,
  Settings,
} from "lucide-react";
import type {
  APIKeyStatus,
  AppState,
  ImageAttachment,
  ModelInfo,
  QueuedMessage,
  SessionSummary,
  UIMessage,
  UsageTotals,
} from "../types";
import { cn } from "../lib/utils";
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
  messageQueue: QueuedMessage[];
  onRemoveQueued: (id: string) => void;
  onClearQueue: () => void;
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
  onProvidersChanged?: () => Promise<void> | void;
  onResend?: (rawIndex: number) => void;
  codexLogin?: CodexLoginHandlers;
  onDismissError: () => void;
};

export function AppShell(props: Props) {
  const {
    state,
    messages,
    sessions,
    models,
    keys,
    streaming,
    streamText,
    streamThinking,
    thinkingStartedAt,
    settingsOpen,
    error,
    scrollRef,
  } = props;

  const isMac = useMemo(() => /mac|iphone|ipad/i.test(navigator.platform), []);
  const shortcutPrefix = isMac ? "⌘" : "Ctrl+";

  const [dirMenuOpen, setDirMenuOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; path: string } | null>(null);
  const [editingPath, setEditingPath] = useState<string | null>(null);
  const [settingsTab, setSettingsTab] = useState<"providers" | "miru" | "mcp">("providers");
  const dirMenuRef = useRef<HTMLDivElement>(null);
  const historyRef = useRef<HTMLDivElement>(null);

  useClickAway(dirMenuOpen, dirMenuRef, () => setDirMenuOpen(false));
  useClickAway(historyOpen, historyRef, () => setHistoryOpen(false));

  // Close popovers on Escape.
  useEffect(() => {
    if (!dirMenuOpen && !historyOpen && !ctxMenu && !editingPath) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setDirMenuOpen(false);
      setHistoryOpen(false);
      setCtxMenu(null);
      setEditingPath(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [dirMenuOpen, historyOpen, ctxMenu, editingPath]);

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
        setHistoryOpen((open) => !open);
      } else if (key === ",") {
        event.preventDefault();
        toggleSettings();
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

  const openSettings = (tab: "providers" | "miru" | "mcp" = "providers") => {
    setSettingsTab(tab);
    if (!settingsOpen) props.onToggleSettings();
  };

  const toggleSettings = () => {
    if (settingsOpen) {
      setSettingsTab("providers");
      props.onToggleSettings();
      return;
    }
    openSettings("providers");
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
        className={cn("titlebar-drag relative z-40 flex h-12 shrink-0 items-center justify-between border-b border-[var(--color-line)] pr-3", isMac ? "pl-[96px]" : "pl-3")}
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
          <div className="relative" ref={historyRef}>
            <button
              type="button"
              onClick={() => setHistoryOpen((open) => !open)}
              className={cn(
                "rounded-md px-2 py-1 text-xs text-[var(--color-muted)] hover:bg-[var(--color-panel-2)] hover:text-[var(--color-text)]",
                historyOpen && "bg-[var(--color-panel-2)] text-[var(--color-text)]",
              )}
              aria-expanded={historyOpen}
            >
              History
            </button>
            {historyOpen && (
              <div className="titlebar-no-drag absolute left-0 top-full z-50 mt-1 w-72 overflow-hidden rounded-lg border border-[var(--color-line)] bg-[var(--color-panel)] py-1 shadow-xl">
                <button
                  type="button"
                  onClick={() => {
                    setHistoryOpen(false);
                    props.onNewSession();
                  }}
                  className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs hover:bg-[var(--color-panel-2)]"
                >
                  <Plus size={14} />
                  New
                  <kbd className="ml-auto text-[10px] text-[var(--color-muted)]">{shortcutPrefix}N</kbd>
                </button>
                <div className="mx-2 border-t border-[var(--color-line)]" />
                <div className="max-h-80 overflow-y-auto py-1">
                  {folderSessions.length === 0 && (
                    <p className="px-3 py-2 text-xs text-[var(--color-muted)]">No sessions yet</p>
                  )}
                  {folderSessions.map((s) => {
                    const active = s.id === state.sessionId;
                    const editing = editingPath === s.path;
                    return (
                      <div key={s.path} className={cn("group relative", active && "bg-[var(--color-panel-2)]")}>
                        {editing ? (
                          <input
                            ref={(input) => input?.focus()}
                            defaultValue={s.name || s.preview || s.id.slice(0, 8)}
                            aria-label="Session name"
                            className="w-full border border-[var(--color-accent-dim)] bg-[var(--color-panel-2)] px-3 py-1.5 text-xs outline-none"
                            onKeyDown={(e) => {
                              if (e.key === "Enter") e.currentTarget.blur();
                              else if (e.key === "Escape") {
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
                          <button
                            type="button"
                            onClick={() => {
                              setHistoryOpen(false);
                              props.onOpenSession(s.path);
                            }}
                            onContextMenu={(e) => openCtxMenu(e, s.path)}
                            className="flex w-full items-center px-3 py-2 pr-8 text-left hover:bg-[var(--color-panel-2)]"
                          >
                            <span className="min-w-0 flex-1">
                              <span className="block truncate text-xs">
                                {s.name || s.preview || s.id.slice(0, 8)}
                              </span>
                              <span className="block truncate text-[10px] text-[var(--color-muted)]">
                                {formatTime(s.modTime || s.timestamp)}
                              </span>
                            </span>
                          </button>
                        )}
                      </div>
                    );
                  })}
                </div>
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
            onClick={toggleSettings}
            className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--color-muted)] hover:bg-[var(--color-panel-2)] hover:text-[var(--color-text)]"
            title={`Settings (${shortcutPrefix},)`}
            aria-label="Open settings"
          >
            <Settings size={15} />
          </button>
        </div>
      </header>

      <main className="flex min-h-0 flex-1 flex-col">
        {!state.hasApiKey && (
          <button
            type="button"
            onClick={() => openSettings("providers")}
            className="px-4 py-2 text-left text-sm text-[var(--color-accent)]"
          >
            Add an API key in Settings to start.
          </button>
        )}

        {messages.length === 0 && !streaming ? (
          <div className="flex min-h-0 flex-1 items-center justify-center px-6 pb-24">
            <div className="w-full max-w-xl">
              {!state.cwd && (
                <button
                  type="button"
                  onClick={props.onOpenFolder}
                  className="mb-3 text-sm text-[var(--color-muted)] hover:text-[var(--color-text)]"
                >
                  Open a folder
                </button>
              )}
              <Composer
                draftKey={state.sessionId || state.cwd || "new"}
                streaming={streaming}
                queue={props.messageQueue}
                onSend={props.onSend}
                onRemoveQueued={props.onRemoveQueued}
                onClearQueue={props.onClearQueue}
                onCommand={props.onCommand}
                onAbort={props.onAbort}
                disabled={!state.cwd}
              />
            </div>
          </div>
        ) : (
          <>
            <Transcript
              key={state.sessionId || state.cwd || "new"}
              messages={messages}
              scrollRef={scrollRef}
              onScroll={props.onTranscriptScroll}
              streamText={streamText}
              streamThinking={streamThinking}
              thinkingStartedAt={thinkingStartedAt}
              streaming={streaming}
              hasWorkspace={!!state.cwd}
              onOpenFolder={props.onOpenFolder}
              openFolderShortcut={`${shortcutPrefix}O`}
              onResend={props.onResend}
              lastError={error}
              onDismissError={props.onDismissError}
            />
            <Composer
              draftKey={state.sessionId || state.cwd || "new"}
              streaming={streaming}
              queue={props.messageQueue}
              onSend={props.onSend}
              onRemoveQueued={props.onRemoveQueued}
              onClearQueue={props.onClearQueue}
              onCommand={props.onCommand}
              onAbort={props.onAbort}
              disabled={!state.cwd}
            />
          </>
        )}
      </main>


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
          onProvidersChanged={props.onProvidersChanged}
          onClose={() => {
            setSettingsTab("providers");
            props.onToggleSettings();
          }}
          codexLogin={props.codexLogin}
          initialTab={settingsTab}
        />
      )}
    </div>
  );
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
