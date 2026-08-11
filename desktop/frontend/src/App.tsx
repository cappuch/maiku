import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOn } from "../wailsjs/runtime/runtime";
import {
  Abort,
  BeginOpenAICodexLogin,
  CancelOpenAICodexLogin,
  FinishOpenAICodexLogin,
  GetState,
  ListAPIKeys,
  ListModels,
  ListSessions,
  NewSession,
  OpenFolder,
  OpenRecentFolder,
  OpenSession,
  Prompt,
  RenameSession,
  SetAPIKey,
  SetModel,
  SetThinking,
} from "../wailsjs/go/main/App";
import type {
  APIKeyStatus,
  AppState,
  ImageAttachment,
  ModelInfo,
  SessionSummary,
  UIMessage,
  UsageTotals,
} from "./types";
import { emptyUsage } from "./types";
import { AppShell } from "./components/AppShell";

function normalizeState(raw: any): AppState {
  return {
    cwd: raw?.cwd ?? "",
    folderName: raw?.folderName ?? "",
    provider: raw?.provider ?? "",
    modelId: raw?.modelId ?? "",
    modelName: raw?.modelName ?? "",
    thinking: raw?.thinking ?? "off",
    streaming: !!raw?.streaming,
    sessionId: raw?.sessionId ?? "",
    sessionPath: raw?.sessionPath ?? "",
    usage: raw?.usage ?? emptyUsage(),
    hasApiKey: !!raw?.hasApiKey,
    messages: Array.isArray(raw?.messages) ? raw.messages : [],
    recentDirs: Array.isArray(raw?.recentDirs) ? raw.recentDirs : [],
    streamingSessionIds: Array.isArray(raw?.streamingSessionIds)
      ? raw.streamingSessionIds
      : [],
    streamText: typeof raw?.streamText === "string" ? raw.streamText : "",
    streamThinking: typeof raw?.streamThinking === "string" ? raw.streamThinking : "",
  };
}

function markStreaming(ids: string[], sessionId: string): string[] {
  if (!sessionId || ids.includes(sessionId)) return ids;
  return [...ids, sessionId];
}

function clearStreaming(ids: string[], sessionId: string): string[] {
  if (!sessionId) return ids;
  return ids.filter((id) => id !== sessionId);
}

export default function App() {
  const [state, setState] = useState<AppState | null>(null);
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [keys, setKeys] = useState<APIKeyStatus[]>([]);
  const [messages, setMessages] = useState<UIMessage[]>([]);
  const [streamText, setStreamText] = useState("");
  const [streamThinking, setStreamThinking] = useState("");
  const [thinkingStartedAt, setThinkingStartedAt] = useState<number | null>(null);
  const [streaming, setStreaming] = useState(false);
  const [streamingSessionIds, setStreamingSessionIds] = useState<string[]>([]);
  const [tokensPerSec, setTokensPerSec] = useState(0);
  const [usage, setUsage] = useState<UsageTotals>(emptyUsage());
  const streamRef = useRef<{ time: number; chars: number } | null>(null);
  // Ring buffer of recent instantaneous tok/s samples; the displayed rate is
  // the plain average so per-update jitter doesn't spike the readout.
  const rateSamplesRef = useRef<number[]>([]);
  const RATE_BUFFER = 16;
  const [error, setError] = useState<string | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  // Bumped on every refresh (and session switch) so a slow in-flight refresh
  // cannot overwrite a newer session after NewSession / OpenSession.
  const refreshGenRef = useRef(0);
  const focusedSessionRef = useRef("");

  const refresh = useCallback(async () => {
    const gen = ++refreshGenRef.current;
    // Apply session state first — ListModels does network I/O and must not
    // leave a stale GetState snapshot pending across a session switch.
    const [s, sess] = await Promise.all([GetState(), ListSessions()]);
    if (gen !== refreshGenRef.current) return;
    const next = normalizeState(s);
    focusedSessionRef.current = next.sessionId;
    setState(next);
    setMessages(next.messages);
    setUsage(next.usage);
    setStreaming(next.streaming);
    setStreamingSessionIds(next.streamingSessionIds);
    setStreamText(next.streamText || "");
    setStreamThinking(next.streamThinking || "");
    if (next.streamThinking) {
      setThinkingStartedAt((prev) => prev ?? Date.now());
    } else if (!next.streaming) {
      setThinkingStartedAt(null);
    }
    if (!next.streaming) {
      streamRef.current = null;
      rateSamplesRef.current = [];
      setTokensPerSec(0);
    }
    setSessions((sess as SessionSummary[]) || []);

    const [mods, apiKeys] = await Promise.all([ListModels(), ListAPIKeys()]);
    if (gen !== refreshGenRef.current) return;
    setModels((mods as ModelInfo[]) || []);
    setKeys((apiKeys as APIKeyStatus[]) || []);
  }, []);

  const isFocused = useCallback((sessionId: unknown) => {
    return typeof sessionId === "string" && sessionId !== "" && sessionId === focusedSessionRef.current;
  }, []);

  useEffect(() => {
    refresh().catch((e) => setError(String(e)));
  }, [refresh]);

  useEffect(() => {
    const offs = [
      EventsOn("maiku:message_update", (data: any) => {
        const sid = data?.sessionId as string | undefined;
        if (sid) {
          setStreamingSessionIds((prev) => markStreaming(prev, sid));
        }
        if (!isFocused(sid)) return;

        if (data?.role === "assistant") {
          setStreaming(true);
          setStreamText(data.text || "");
          const thinking =
            typeof data.thinking === "string" ? data.thinking : "";
          setStreamThinking(thinking);
          if (thinking) {
            setThinkingStartedAt((prev) => prev ?? Date.now());
          }
          // Measure live generation speed: chars delta / elapsed, ~4 chars per token.
          // The backend sends data.chars = all streamed content (text + thinking +
          // tool-call JSON), so the rate moves during reasoning instead of sitting
          // at 0 until the final answer text streams.
          const text = typeof data.text === "string" ? data.text : "";
          const total = typeof data.chars === "number" ? data.chars : text.length;
          const now = performance.now();
          const prev = streamRef.current;
          if (prev && now - prev.time < 3000) {
            const dt = (now - prev.time) / 1000;
            const dc = total - prev.chars;
            if (dt > 0 && dc > 0) {
              const inst = dc / dt / 4;
              const samples = rateSamplesRef.current;
              samples.push(inst);
              if (samples.length > RATE_BUFFER) samples.shift();
              let sum = 0;
              for (let i = 0; i < samples.length; i++) sum += samples[i];
              setTokensPerSec(sum / samples.length);
            }
          }
          streamRef.current = { time: now, chars: total };
        }
        const toolCalls = Array.isArray(data?.toolCalls) ? data.toolCalls : [];
        if (toolCalls.length > 0) {
          setMessages((prev) => {
            let next = prev;
            for (const tc of toolCalls) {
              const id = (tc?.toolCallId || tc?.id || "") as string;
              const name = (tc?.toolName || tc?.name || "") as string;
              if (!id && !name) continue;
              let idx = id
                ? next.findIndex(
                    (m) => m.toolCallId === id && (m.role === "tool" || m.role === "toolResult"),
                  )
                : -1;
              if (idx < 0 && name) {
                for (let i = next.length - 1; i >= 0; i--) {
                  const m = next[i];
                  if (
                    m.role === "tool" &&
                    m.streaming &&
                    (m.toolName || "").toLowerCase() === name.toLowerCase() &&
                    (!id || !m.toolCallId || m.toolCallId === id)
                  ) {
                    idx = i;
                    break;
                  }
                }
              }
              const patch: UIMessage = {
                role: "tool",
                toolCallId: id || undefined,
                toolName: name,
                args: tc?.args ?? {},
                streaming: true,
                text: "",
              };
              if (idx >= 0) {
                if (next === prev) next = [...prev];
                next[idx] = {
                  ...next[idx],
                  ...patch,
                  toolCallId: id || next[idx].toolCallId,
                  details: next[idx].details,
                };
              } else if (id) {
                // Only create a new card once we have a stable id.
                if (next === prev) next = [...prev];
                next.push(patch);
              }
            }
            return next;
          });
        }
      }),
      EventsOn("maiku:message_end", (data: any) => {
        const sid = data?.sessionId as string | undefined;
        if (!isFocused(sid)) return;

        const msg = data?.message as UIMessage | undefined;
        if (msg) {
          setMessages((prev) => {
            // Avoid duplicating the optimistic user bubble we already appended.
            if (msg.role === "user" && prev.length > 0) {
              const last = prev[prev.length - 1];
              if (
                last.role === "user" &&
                last.text === msg.text &&
                (last.images?.length || 0) === (msg.images?.length || 0)
              ) {
                const next = [...prev];
                next[next.length - 1] = { ...msg, streaming: false };
                return next;
              }
            }
            // toolResult message_end would duplicate the live tool card from
            // message_update / tool_start — merge into the existing card.
            if (msg.role === "toolResult" && msg.toolCallId) {
              const idx = prev.findIndex(
                (m) =>
                  m.toolCallId === msg.toolCallId &&
                  (m.role === "tool" || m.role === "toolResult"),
              );
              if (idx >= 0) {
                const next = [...prev];
                next[idx] = {
                  ...next[idx],
                  role: "tool",
                  text: msg.text || next[idx].text,
                  details: msg.details ?? next[idx].details,
                  isError: msg.isError,
                  streaming: false,
                };
                return next;
              }
            }
            // Skip empty assistant shells (tool-only turns already have cards).
            if (msg.role === "assistant" && !msg.text && !msg.thinking) return prev;
            if (msg.role === "toolResult") return prev;
            return [...prev, { ...msg, streaming: false }];
          });
        }
        if (data?.usage) setUsage(data.usage);
        setStreamText("");
        setStreamThinking("");
        setThinkingStartedAt(null);
        streamRef.current = null;
        rateSamplesRef.current = [];
        setTokensPerSec(0);
      }),
      EventsOn("maiku:tool_start", (data: any) => {
        const sid = data?.sessionId as string | undefined;
        if (sid) setStreamingSessionIds((prev) => markStreaming(prev, sid));
        if (!isFocused(sid)) return;

        setMessages((prev) => {
          const id = data?.toolCallId as string | undefined;
          const name = (data?.toolName as string) || "";
          let idx = id
            ? prev.findIndex((m) => m.toolCallId === id && (m.role === "tool" || m.role === "toolResult"))
            : -1;
          // Streaming may have created a card before the final id landed — match
          // the latest pending card with the same tool name.
          if (idx < 0 && name) {
            for (let i = prev.length - 1; i >= 0; i--) {
              const m = prev[i];
              if (
                m.role === "tool" &&
                m.streaming &&
                (m.toolName || "").toLowerCase() === name.toLowerCase() &&
                (!m.toolCallId || m.toolCallId === id)
              ) {
                idx = i;
                break;
              }
            }
          }
          const patch: UIMessage = {
            role: "tool",
            toolCallId: id,
            toolName: name,
            args: parseMaybeJSON(data?.args),
            text: "running…",
            streaming: true,
          };
          if (idx >= 0) {
            const next = [...prev];
            next[idx] = {
              ...next[idx],
              ...patch,
              args: parseMaybeJSON(data?.args) ?? next[idx].args,
            };
            return next;
          }
          return [...prev, patch];
        });
      }),
      EventsOn("maiku:tool_end", (data: any) => {
        if (!isFocused(data?.sessionId)) return;
        setMessages((prev) => {
          const next = [...prev];
          for (let i = next.length - 1; i >= 0; i--) {
            if (next[i].toolCallId === data?.toolCallId && (next[i].role === "tool" || next[i].role === "toolResult")) {
              const resultText =
                typeof data?.resultText === "string" && data.resultText
                  ? data.resultText
                  : typeof data?.result === "object" && data?.result?.content
                    ? JSON.stringify(data.result.content)
                    : JSON.stringify(data?.result ?? "", null, 0);
              next[i] = {
                ...next[i],
                streaming: false,
                isError: !!data?.isError,
                text: truncate(resultText, 4000),
                details: data?.details ?? next[i].details,
              };
              break;
            }
          }
          return next;
        });
      }),
      EventsOn("maiku:idle", (data: any) => {
        const sid = (data?.sessionId as string) || "";
        if (sid) {
          setStreamingSessionIds((prev) => clearStreaming(prev, sid));
        }
        if (!isFocused(sid)) {
          // Background session finished — refresh sidebar list only.
          ListSessions()
            .then((sess) => setSessions((sess as SessionSummary[]) || []))
            .catch(() => {});
          return;
        }
        setStreaming(false);
        setStreamText("");
        setStreamThinking("");
        setThinkingStartedAt(null);
        streamRef.current = null;
        rateSamplesRef.current = [];
        setTokensPerSec(0);
        refresh().catch(() => {});
      }),
      EventsOn("maiku:error", (data: any) => {
        const sid = data?.sessionId as string | undefined;
        if (sid) setStreamingSessionIds((prev) => clearStreaming(prev, sid));
        if (!isFocused(sid)) return;
        setError(data?.error || "Unknown error");
        setStreaming(false);
      }),
    ];
    return () => offs.forEach((off) => off && off());
  }, [refresh, isFocused]);

  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, streamText, streamThinking]);

  const onSend = async (text: string, images: ImageAttachment[] = []) => {
    setError(null);
    setMessages((prev) => [...prev, { role: "user", text, images }]);
    setStreaming(true);
    if (focusedSessionRef.current) {
      setStreamingSessionIds((prev) => markStreaming(prev, focusedSessionRef.current));
    }
    try {
      await Prompt(text, images);
    } catch (e: any) {
      setError(e?.message || String(e));
      setStreaming(false);
      if (focusedSessionRef.current) {
        setStreamingSessionIds((prev) => clearStreaming(prev, focusedSessionRef.current));
      }
    }
  };

  const switchSession = async (fn: () => Promise<unknown>) => {
    refreshGenRef.current += 1;
    // Ignore transcript events until refresh pins the new focus.
    focusedSessionRef.current = "";
    setStreamText("");
    setStreamThinking("");
    setThinkingStartedAt(null);
    streamRef.current = null;
    rateSamplesRef.current = [];
    setTokensPerSec(0);
    await fn();
    await refresh();
  };

  if (!state) {
    return (
      <div className="flex h-full items-center justify-center text-[var(--color-muted)]">
        Loading maiku…
      </div>
    );
  }

  return (
    <AppShell
      state={state}
      usage={usage}
      messages={messages}
      sessions={sessions}
      models={models}
      keys={keys}
      streaming={streaming}
      streamingSessionIds={streamingSessionIds}
      streamText={streamText}
      streamThinking={streamThinking}
      thinkingStartedAt={thinkingStartedAt}
      tokensPerSec={tokensPerSec}
      sidebarOpen={sidebarOpen}
      settingsOpen={settingsOpen}
      error={error}
      scrollRef={scrollRef}
      recentDirs={state.recentDirs}
      onToggleSidebar={() => setSidebarOpen((v) => !v)}
      onToggleSettings={() => setSettingsOpen((v) => !v)}
      onSend={onSend}
      onAbort={() => Abort()}
      onNewSession={async () => {
        await switchSession(() => NewSession());
      }}
      onOpenFolder={async () => {
        await switchSession(() => OpenFolder());
      }}
      onOpenRecentFolder={async (path) => {
        try {
          await switchSession(() => OpenRecentFolder(path));
        } catch (e: any) {
          setError(e?.message || String(e));
        }
      }}
      onOpenSession={async (path) => {
        await switchSession(() => OpenSession(path));
      }}
      onRenameSession={async (path, name) => {
        try {
          await RenameSession(path, name);
          await refresh();
        } catch (e: any) {
          setError(e?.message || String(e));
        }
      }}
      onSetModel={async (provider, id) => {
        await SetModel(provider, id);
        await refresh();
      }}
      onSetThinking={async (level) => {
        await SetThinking(level);
        await refresh();
      }}
      onSaveKey={async (provider, key) => {
        await SetAPIKey(provider, key);
        await refresh();
      }}
      codexLogin={{
        begin: async () => {
          const info = await BeginOpenAICodexLogin();
          return {
            userCode: info?.userCode ?? "",
            verificationUri: info?.verificationUri ?? "",
          };
        },
        finish: async () => {
          await FinishOpenAICodexLogin();
          await refresh();
        },
        cancel: () => CancelOpenAICodexLogin(),
      }}
      onDismissError={() => setError(null)}
    />
  );
}

function truncate(s: string, n: number) {
  if (s.length <= n) return s;
  return s.slice(0, n) + "…";
}

function parseMaybeJSON(value: unknown): unknown {
  if (typeof value === "string") {
    try {
      return JSON.parse(value);
    } catch {
      return value;
    }
  }
  return value;
}
