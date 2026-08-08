import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
  };
}

export default function App() {
  const [state, setState] = useState<AppState | null>(null);
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [keys, setKeys] = useState<APIKeyStatus[]>([]);
  const [messages, setMessages] = useState<UIMessage[]>([]);
  const [streamText, setStreamText] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [tokensPerSec, setTokensPerSec] = useState(0);
  const [usage, setUsage] = useState<UsageTotals>(emptyUsage());
  const streamRef = useRef<{ time: number; chars: number } | null>(null);
  const rateRef = useRef(0);
  const [error, setError] = useState<string | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  // Bumped on every refresh (and session switch) so a slow in-flight refresh
  // cannot overwrite a newer session after NewSession / OpenSession.
  const refreshGenRef = useRef(0);

  const refresh = useCallback(async () => {
    const gen = ++refreshGenRef.current;
    // Apply session state first — ListModels does network I/O and must not
    // leave a stale GetState snapshot pending across a session switch.
    const [s, sess] = await Promise.all([GetState(), ListSessions()]);
    if (gen !== refreshGenRef.current) return;
    const next = normalizeState(s);
    setState(next);
    setMessages(next.messages);
    setUsage(next.usage);
    setStreaming(next.streaming);
    setSessions((sess as SessionSummary[]) || []);

    const [mods, apiKeys] = await Promise.all([ListModels(), ListAPIKeys()]);
    if (gen !== refreshGenRef.current) return;
    setModels((mods as ModelInfo[]) || []);
    setKeys((apiKeys as APIKeyStatus[]) || []);
  }, []);

  useEffect(() => {
    refresh().catch((e) => setError(String(e)));
  }, [refresh]);

  useEffect(() => {
    const offs = [
      EventsOn("maiku:message_update", (data: any) => {
        if (data?.role === "assistant") {
          setStreaming(true);
          setStreamText(data.text || "");
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
              rateRef.current =
                rateRef.current > 0 ? rateRef.current * 0.7 + inst * 0.3 : inst;
              setTokensPerSec(rateRef.current);
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
            if (msg.role === "assistant" && !msg.text) return prev;
            if (msg.role === "toolResult") return prev;
            return [...prev, { ...msg, streaming: false }];
          });
        }
        if (data?.usage) setUsage(data.usage);
        setStreamText("");
        streamRef.current = null;
        rateRef.current = 0;
        setTokensPerSec(0);
      }),
      EventsOn("maiku:tool_start", (data: any) => {
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
      EventsOn("maiku:idle", () => {
        setStreaming(false);
        setStreamText("");
        streamRef.current = null;
        rateRef.current = 0;
        setTokensPerSec(0);
        refresh().catch(() => {});
      }),
      EventsOn("maiku:error", (data: any) => {
        setError(data?.error || "Unknown error");
        setStreaming(false);
      }),
    ];
    return () => offs.forEach((off) => off && off());
  }, [refresh]);

  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, streamText]);

  const displayMessages = useMemo(() => {
    if (!streamText) return messages;
    return [...messages, { role: "assistant", text: streamText, streaming: true }];
  }, [messages, streamText]);

  const onSend = async (text: string, images: ImageAttachment[] = []) => {
    setError(null);
    setMessages((prev) => [...prev, { role: "user", text, images }]);
    setStreaming(true);
    try {
      await Prompt(text, images);
    } catch (e: any) {
      setError(e?.message || String(e));
      setStreaming(false);
    }
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
      messages={displayMessages}
      sessions={sessions}
      models={models}
      keys={keys}
      streaming={streaming}
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
        refreshGenRef.current += 1;
        setMessages([]);
        setStreamText("");
        setStreaming(false);
        await NewSession();
        await refresh();
      }}
      onOpenFolder={async () => {
        refreshGenRef.current += 1;
        setMessages([]);
        setStreamText("");
        setStreaming(false);
        await OpenFolder();
        await refresh();
      }}
      onOpenRecentFolder={async (path) => {
        try {
          refreshGenRef.current += 1;
          setMessages([]);
          setStreamText("");
          setStreaming(false);
          await OpenRecentFolder(path);
          await refresh();
        } catch (e: any) {
          setError(e?.message || String(e));
        }
      }}
      onOpenSession={async (path) => {
        refreshGenRef.current += 1;
        setStreamText("");
        setStreaming(false);
        await OpenSession(path);
        await refresh();
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
