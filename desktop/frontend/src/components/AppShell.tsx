import { useCallback, useEffect, useMemo, useState, type MouseEvent, type RefObject } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import {
  Check,
  ChevronDown,
  FolderOpen,
  KeyRound,
  PanelLeft,
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
import { cn, formatCost, formatTokens } from "../lib/utils";
import { easeOut } from "../lib/motion";
import { ModelSelector } from "./ModelSelector";
import { ClickAway } from "./ClickAway";
import { SettingsDialog, type CodexLoginHandlers } from "./SettingsDialog";
import { Transcript } from "./Transcript";
import { Composer } from "./Composer";
import { Button } from "./ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "./ui/dropdown-menu";
import { Separator } from "./ui/separator";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./ui/tooltip";

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
  onReleaseFollow?: () => void;
  onPinFollow?: () => void;
  programmaticScrollRef?: RefObject<boolean>;
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

  const reduce = useReducedMotion();
  const isMac = useMemo(() => /mac|iphone|ipad/i.test(navigator.platform), []);
  const shortcutPrefix = isMac ? "⌘" : "Ctrl+";

  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; path: string } | null>(null);
  const [editingPath, setEditingPath] = useState<string | null>(null);
  const [settingsTab, setSettingsTab] = useState<"providers" | "miru" | "mcp">("providers");

  useEffect(() => {
    if (!ctxMenu && !editingPath) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setCtxMenu(null);
      setEditingPath(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [ctxMenu, editingPath]);

  const toggleSettings = useCallback(() => {
    setSettingsTab("providers");
    props.onToggleSettings();
  }, [props]);

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
        toggleSettings();
      }
    };
    window.addEventListener("keydown", onShortcut);
    return () => window.removeEventListener("keydown", onShortcut);
  }, [props, settingsOpen, isMac, toggleSettings]);

  const folderSessions = sessions.filter(
    (s) => !state.cwd || s.cwd === state.cwd || s.path.includes(encodeCwd(state.cwd)),
  );

  const openCtxMenu = (e: MouseEvent, path: string) => {
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

  const shellMotion = reduce
    ? { initial: false as const, animate: { opacity: 1 } }
    : { initial: { opacity: 0 }, animate: { opacity: 1 }, transition: { duration: 0.35, ease: easeOut } };

  return (
    <TooltipProvider>
      <motion.div
        {...shellMotion}
        className="app-shell flex h-full flex-col text-zinc-100"
      >
        <header
          data-wails-drag
          className={cn(
            "titlebar-drag relative z-40 flex h-12 shrink-0 items-center gap-1 border-b border-white/[0.06] pr-2",
            isMac ? "pl-[92px]" : "pl-2",
          )}
        >
          <div className="flex min-w-0 items-center gap-0.5" data-wails-no-drag>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  onClick={props.onToggleSidebar}
                  aria-pressed={sidebarOpen}
                  aria-label={sidebarOpen ? "Hide sessions" : "Show sessions"}
                >
                  <PanelLeft size={16} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Sessions ({shortcutPrefix}B)</TooltipContent>
            </Tooltip>
            {!sidebarOpen && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button type="button" variant="ghost" size="icon" onClick={props.onNewSession} aria-label="New chat">
                    <Plus size={16} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>New chat ({shortcutPrefix}N)</TooltipContent>
              </Tooltip>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button
                  type="button"
                  className="flex max-w-[280px] items-center gap-1.5 rounded-lg px-2 py-1.5 text-sm leading-none tracking-tight hover:bg-white/[0.05]"
                  title={state.cwd || "Open a folder"}
                >
                  <span className="font-medium text-zinc-100">maiku</span>
                  <span className="text-zinc-600">/</span>
                  <span className="truncate text-zinc-400">{state.folderName || "no folder"}</span>
                  <ChevronDown size={13} className="shrink-0 text-zinc-500" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-80">
                <DropdownMenuItem
                  onSelect={() => {
                    props.onOpenFolder();
                  }}
                >
                  <FolderOpen size={14} className="text-zinc-300" />
                  Open folder…
                  <span className="ml-auto font-mono text-[10px] text-zinc-500">{shortcutPrefix}O</span>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                {state.recentDirs.length === 0 && (
                  <DropdownMenuItem disabled>No recent folders yet</DropdownMenuItem>
                )}
                {state.recentDirs.map((d) => {
                  const active = d === state.cwd;
                  return (
                    <DropdownMenuItem
                      key={d}
                      onSelect={() => {
                        if (!active) props.onOpenRecentFolder(d);
                      }}
                    >
                      <span className="min-w-0 flex-1">
                        <span className="block truncate">{basename(d) || d}</span>
                        <span className="block truncate font-mono text-[10px] text-zinc-500">{d}</span>
                      </span>
                      {active && <Check size={13} className="shrink-0 text-zinc-200" />}
                    </DropdownMenuItem>
                  );
                })}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>

          <div className="min-w-0 flex-1" />

          <div className="flex shrink-0 items-center gap-1.5" data-wails-no-drag>
            <ModelSelector
              models={models}
              provider={state.provider}
              modelId={state.modelId}
              thinking={state.thinking}
              onSetModel={props.onSetModel}
              onSetThinking={props.onSetThinking}
            />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  onClick={toggleSettings}
                  aria-label="Open settings"
                >
                  <Settings size={15} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Settings ({shortcutPrefix},)</TooltipContent>
            </Tooltip>
          </div>
        </header>

        <div className="flex min-h-0 flex-1">
          <motion.aside
            initial={false}
            animate={{ width: sidebarOpen ? 272 : 0 }}
            transition={reduce ? { duration: 0 } : { type: "spring", bounce: 0, duration: 0.42 }}
            className={cn("shrink-0 overflow-hidden", sidebarOpen && "border-r border-white/[0.06]")}
            aria-hidden={!sidebarOpen}
            inert={!sidebarOpen ? true : undefined}
          >
            <div className="flex h-full w-[272px] min-w-0 flex-col overflow-hidden bg-[#0c0c0e]">
              <div className="px-3 pt-3">
                <Button type="button" className="w-full" onClick={props.onNewSession}>
                  <Plus size={14} />
                  New chat
                  <kbd className="ml-auto font-mono text-[10px] font-normal text-black/45">{shortcutPrefix}N</kbd>
                </Button>
              </div>
              <div className="mt-3 min-h-0 flex-1 overflow-x-hidden overflow-y-auto">
                <div className="flex w-full min-w-0 flex-col gap-0.5 px-2 pb-3">
                  {folderSessions.length === 0 && (
                    <p className="px-2 py-6 text-center text-xs leading-5 text-zinc-500">
                      No sessions in this folder yet.
                    </p>
                  )}
                  {folderSessions.map((s) => {
                    const active = s.id === state.sessionId;
                    const editing = editingPath === s.path;
                    const live = streamingSessionIds.includes(s.id);
                    return (
                      <div key={s.path} className="group relative min-w-0">
                        {editing ? (
                          <input
                            ref={(input) => input?.focus()}
                            defaultValue={s.name || s.preview || s.id.slice(0, 8)}
                            aria-label="Session name"
                            className="w-full rounded-lg border border-white/20 bg-white/[0.04] px-2.5 py-2 text-xs outline-none"
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
                            onClick={() => props.onOpenSession(s.path)}
                            onDoubleClick={() => startRename(s.path)}
                            onContextMenu={(e) => openCtxMenu(e, s.path)}
                            title={s.name || s.preview || s.id.slice(0, 8)}
                            className={cn(
                              "flex w-full min-w-0 items-center gap-2 overflow-hidden rounded-lg px-2.5 py-2 text-left transition-colors",
                              active
                                ? "bg-white/[0.07] text-zinc-50"
                                : "text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-100",
                            )}
                          >
                            <span className="min-w-0 flex-1 overflow-hidden">
                              <span className="block truncate text-[13px]">
                                {s.name || s.preview || s.id.slice(0, 8)}
                              </span>
                              <span className="mt-0.5 block truncate font-mono text-[10px] text-zinc-500">
                                {formatTime(s.modTime || s.timestamp)}
                              </span>
                            </span>
                            {live && (
                              <span className="h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-zinc-200" title="Streaming" />
                            )}
                          </button>
                        )}
                      </div>
                    );
                  })}
                </div>
              </div>
              <Separator className="bg-white/[0.06]" />
              <div className="flex items-center justify-between gap-3 px-4 py-3 text-[11px] text-zinc-500">
                {usage.totalTokens > 0 ? (
                  <>
                    <span className="min-w-0 truncate">
                      {formatTokens(usage.totalTokens)} tokens
                      {streaming && tokensPerSec > 0 ? (
                        <span className="font-mono text-zinc-400"> · {Math.round(tokensPerSec)} tok/s</span>
                      ) : null}
                    </span>
                    <span className="shrink-0 font-mono text-zinc-400">{formatCost(usage.totalCost || usage.cost)}</span>
                  </>
                ) : (
                  <span className="text-zinc-600">{shortcutPrefix}N new · {shortcutPrefix}B sessions</span>
                )}
              </div>
            </div>
          </motion.aside>

          <main className="flex min-h-0 min-w-0 flex-1 flex-col">
            {!state.hasApiKey && (
              <div className="flex justify-center px-4 pt-3">
                <button
                  type="button"
                  onClick={() => openSettings("providers")}
                  className="inline-flex items-center gap-2 rounded-full border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs text-zinc-300 transition hover:bg-white/[0.08] hover:text-white"
                >
                  <KeyRound size={12} />
                  Add an API key to start
                </button>
              </div>
            )}

            {messages.length === 0 && !streaming ? (
              <div className="flex min-h-0 flex-1 flex-col items-center justify-center px-6 pb-8">
                <div className="w-full max-w-[760px]">
                  <Composer
                    placement="hero"
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
                  onReleaseFollow={props.onReleaseFollow}
                  onPinFollow={props.onPinFollow}
                  programmaticScrollRef={props.programmaticScrollRef}
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
        </div>

        <AnimatePresence>
          {ctxMenu && (
            <>
              <ClickAway
                onClose={() => setCtxMenu(null)}
                onContextMenu={(e) => {
                  e.preventDefault();
                  setCtxMenu(null);
                }}
              />
              <motion.div
                data-wails-no-drag
                initial={reduce ? false : { opacity: 0, scale: 0.98, y: -4 }}
                animate={{ opacity: 1, scale: 1, y: 0 }}
                exit={reduce ? undefined : { opacity: 0, scale: 0.98 }}
                transition={{ duration: 0.14, ease: easeOut }}
                className="titlebar-no-drag fixed z-50 w-44 overflow-hidden rounded-xl border border-white/10 bg-[#141416] py-1 shadow-[0_24px_80px_rgba(0,0,0,0.45)]"
                style={{ left: ctxMenu.x, top: ctxMenu.y }}
              >
                <p className="px-3 pt-1 pb-1 font-mono text-[10px] text-zinc-500">
                  {basename(ctxMenu.path) || "session"}
                </p>
                <button
                  type="button"
                  onClick={() => startRename(ctxMenu.path)}
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs text-zinc-200 hover:bg-white/[0.06]"
                >
                  <Pencil size={12} />
                  Rename…
                </button>
              </motion.div>
            </>
          )}
        </AnimatePresence>

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
      </motion.div>
    </TooltipProvider>
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
