import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { EventsOn } from "../wailsjs/runtime/runtime";
import {
  Abort,
  BeginOpenAICodexLogin,
  CancelOpenAICodexLogin,
  Compact,
  FinishOpenAICodexLogin,
  GetState,
  Goal,
  ListAPIKeys,
  ListModels,
  ListSessions,
  NewSession,
  OpenFolder,
  OpenRecentFolder,
  OpenSession,
  Prompt,
  RenameSession,
  ResendUserMessage,
  SetAPIKey,
  SetModel,
  SetSubagentEnabled,
  SetThinking,
} from "../wailsjs/go/main/App";
import type {
  APIKeyStatus,
  AppState,
  ImageAttachment,
  ModelInfo,
  SessionSummary,
  SubagentActivity,
  UIMessage,
  UsageTotals,
} from "./types";
import { emptyUsage } from "./types";
import { AppShell } from "./components/AppShell";

const SCROLL_BOTTOM_THRESHOLD = 48;

function normalizeState(raw: Partial<AppState> | null | undefined): AppState {
  return {
    cwd: raw?.cwd ?? "",
    folderName: raw?.folderName ?? "",
    userName: raw?.userName ?? "",
    provider: raw?.provider ?? "",
    modelId: raw?.modelId ?? "",
    modelName: raw?.modelName ?? "",
    thinking: raw?.thinking ?? "off",
    streaming: !!raw?.streaming,
    sessionId: raw?.sessionId ?? "",
    sessionPath: raw?.sessionPath ?? "",
    usage: normalizeUsage(raw?.usage),
    hasApiKey: !!raw?.hasApiKey,
    messages: Array.isArray(raw?.messages) ? raw.messages : [],
    recentDirs: Array.isArray(raw?.recentDirs) ? raw.recentDirs : [],
    streamingSessionIds: Array.isArray(raw?.streamingSessionIds)
      ? raw.streamingSessionIds
      : [],
    streamText: typeof raw?.streamText === "string" ? raw.streamText : "",
    streamThinking: typeof raw?.streamThinking === "string" ? raw.streamThinking : "",
    mcp: normalizeMCP(raw?.mcp),
  };
}

function normalizeMCP(raw: AppState["mcp"] | null | undefined): AppState["mcp"] {
  if (!raw || typeof raw !== "object") {
    return { configured: 0, connected: 0, failed: 0, servers: [] };
  }
  return {
    configured: typeof raw.configured === "number" ? raw.configured : 0,
    connected: typeof raw.connected === "number" ? raw.connected : 0,
    failed: typeof raw.failed === "number" ? raw.failed : 0,
    servers: Array.isArray(raw.servers) ? raw.servers : [],
  };
}

function normalizeUsage(raw: Partial<UsageTotals> | null | undefined): UsageTotals {
  const base = emptyUsage();
  if (!raw) return base;
  const cost = typeof raw.cost === "number" ? raw.cost : 0;
  const totalCost =
    typeof raw.totalCost === "number" ? raw.totalCost : cost;
  return {
    input: typeof raw.input === "number" ? raw.input : 0,
    output: typeof raw.output === "number" ? raw.output : 0,
    cacheRead: typeof raw.cacheRead === "number" ? raw.cacheRead : 0,
    cacheWrite: typeof raw.cacheWrite === "number" ? raw.cacheWrite : 0,
    totalTokens: typeof raw.totalTokens === "number" ? raw.totalTokens : 0,
    cost,
    totalCost,
    cacheRate: typeof raw.cacheRate === "number" ? raw.cacheRate : 0,
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
  const [retryingStartup, setRetryingStartup] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  // Stream updates should follow the response only while the user is already
  // at the bottom. Keep this in a ref so every generated chunk does not render.
  const followTranscriptRef = useRef(true);
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
    const sameSession = focusedSessionRef.current === next.sessionId;
    focusedSessionRef.current = next.sessionId;
    setState(next);
    setMessages((current) => {
      if (!sameSession) return next.messages;
      const liveByToolCall = new Map(
        current
          .filter((message) => message.toolCallId && message.subagent)
          .map((message) => [message.toolCallId, message] as const),
      );
      return next.messages.map((message) => {
        if (!message.toolCallId) return message;
        const live = liveByToolCall.get(message.toolCallId);
        return live?.subagent ? { ...message, subagent: live.subagent } : message;
      });
    });
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
      EventsOn("maiku:compaction_start", (data) => {
        const sid = data?.sessionId as string | undefined;
        if (sid) setStreamingSessionIds((prev) => markStreaming(prev, sid));
        if (!isFocused(sid)) return;
        setStreaming(true);
        setStreamText("");
        setStreamThinking("");
        setThinkingStartedAt(null);
      }),
      EventsOn("maiku:compacted", (data) => {
        if (!isFocused(data?.sessionId)) return;
        if (data?.usage) setUsage(data.usage);
        const removed = typeof data?.messagesRemoved === "number" ? data.messagesRemoved : 0;
        const before = typeof data?.tokensBefore === "number" ? data.tokensBefore : 0;
        const after = typeof data?.tokensAfter === "number" ? data.tokensAfter : 0;
        const sessionId = data?.sessionId as string;
        void (async () => {
          try {
            const s = await GetState();
            if (!isFocused(sessionId)) return;
            const next = normalizeState(s);
            setMessages([
              ...next.messages,
              {
                id: `notice-compact-${Date.now()}`,
                role: "notice",
                text: formatCompactNotice(removed, before, after),
              },
            ]);
          } catch {
            setMessages((prev) => [
              ...prev,
              {
                id: `notice-compact-${Date.now()}`,
                role: "notice",
                text: formatCompactNotice(removed, before, after),
              },
            ]);
          }
        })();
      }),
      EventsOn("maiku:message_update", (data) => {
        const sid = data?.sessionId as string | undefined;
        if (sid) {
          setStreamingSessionIds((prev) => markStreaming(prev, sid));
        }
        if (!isFocused(sid)) return;

        if (data?.role === "assistant") {
          setStreaming(true);
          const replace = data?.replace === true;
          const hasTextDelta = typeof data?.textDelta === "string";
          const textDelta = hasTextDelta ? data.textDelta : "";
          const thinkingDelta =
            typeof data?.thinkingDelta === "string" ? data.thinkingDelta : "";
          setStreamText((current) =>
            hasTextDelta
              ? replace
                ? textDelta
                : current + textDelta
              : typeof data?.text === "string"
                ? data.text
                : current,
          );
          setStreamThinking((current) =>
            typeof data?.thinkingDelta === "string"
              ? replace
                ? thinkingDelta
                : current + thinkingDelta
              : typeof data?.thinking === "string"
                ? data.thinking
                : current,
          );
          if (thinkingDelta || (typeof data?.thinking === "string" && data.thinking)) {
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
      EventsOn("maiku:message_end", (data) => {
        const sid = data?.sessionId as string | undefined;
        if (!isFocused(sid)) return;

        const msg = data?.message as UIMessage | undefined;
        const toolCallIds = Array.isArray(data?.toolCallIds)
          ? data.toolCallIds.filter((id: unknown): id is string => typeof id === "string" && id !== "")
          : [];
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

            const completed = { ...msg, streaming: false };
            if (msg.role === "assistant" && toolCallIds.length > 0) {
              // message_update creates tool cards while this assistant message
              // is still streaming. Keep the completed thinking/text in its
              // real chronological position: immediately before its tools.
              const ids = new Set(toolCallIds);
              const firstTool = prev.findIndex(
                (message) =>
                  !!message.toolCallId &&
                  ids.has(message.toolCallId) &&
                  (message.role === "tool" || message.role === "toolResult"),
              );
              if (firstTool >= 0) {
                return [
                  ...prev.slice(0, firstTool),
                  completed,
                  ...prev.slice(firstTool),
                ];
              }
            }
            return [...prev, completed];
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
      EventsOn("maiku:tool_start", (data) => {
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
      EventsOn("maiku:tool_update", (data) => {
        if (!isFocused(data?.sessionId)) return;
        const id = typeof data?.toolCallId === "string" ? data.toolCallId : "";
        if (!id) return;
        const partial = parsePartialResult(data?.partialResult);
        if (!partial.text && partial.details === undefined) return;
        setMessages((prev) => {
          const index = prev.findIndex(
            (message) =>
              message.toolCallId === id &&
              (message.role === "tool" || message.role === "toolResult"),
          );
          if (index < 0) return prev;
          const current = prev[index];
          const next = [...prev];
          next[index] = {
            ...current,
            text: partial.text ? truncate(partial.text, 4000) : current.text,
            details: partial.details ?? current.details,
          };
          return next;
        });
      }),
      EventsOn("maiku:subagent_event", (data) => {
        if (!isFocused(data?.sessionId)) return;
        const subagentId = typeof data?.subagentId === "string" ? data.subagentId : "";
        if (!subagentId) return;

        setMessages((prev) => {
          const index = prev.findIndex(
            (message) =>
              message.toolCallId === subagentId &&
              (message.toolName || "").toLowerCase() === "subagent",
          );
          if (index < 0) return prev;

          const current = prev[index];
          const view = current.subagent ?? {
            status: "starting" as const,
            activities: persistedSubagentActivities(current.details),
          };
          let status = view.status;
          const activities = [...view.activities];
          let text = view.text;
          let thinking = view.thinking;
          let childError = view.error;
          const eventType = typeof data?.type === "string" ? data.type : "";

          if (eventType === "start") {
            status = "running";
          } else if (eventType === "message") {
            if (typeof data?.text === "string") text = truncate(data.text, 8000);
            if (typeof data?.thinking === "string") thinking = truncate(data.thinking, 8000);
            if (data?.status === "error") {
              status = "error";
              childError = typeof data?.error === "string" ? data.error : "Subagent failed";
            }
          } else if (eventType === "tool_start") {
            status = "running";
            const toolCallId = typeof data?.toolCallId === "string" ? data.toolCallId : "";
            const activity: SubagentActivity = {
              toolCallId,
              toolName: typeof data?.toolName === "string" ? data.toolName : "tool",
              input: subagentActionInput(data?.toolName, data?.args),
              status: "running",
            };
            const activityIndex = activities.findIndex((item) => item.toolCallId === toolCallId);
            if (activityIndex >= 0) activities[activityIndex] = activity;
            else activities.push(activity);
          } else if (eventType === "tool_end") {
            const toolCallId = typeof data?.toolCallId === "string" ? data.toolCallId : "";
            const activityIndex = activities.findIndex((item) => item.toolCallId === toolCallId);
            const completed: SubagentActivity = {
              ...(activityIndex >= 0
                ? activities[activityIndex]
                : {
                    toolCallId,
                    toolName: typeof data?.toolName === "string" ? data.toolName : "tool",
                  }),
              output: typeof data?.result === "string" ? truncate(data.result, 1600) : undefined,
              status: data?.isError ? "error" : "completed",
              isError: !!data?.isError,
            };
            if (activityIndex >= 0) activities[activityIndex] = completed;
            else activities.push(completed);
          } else if (eventType === "end" && status !== "error") {
            status = "completed";
          }

          const next = [...prev];
          next[index] = {
            ...current,
            subagent: { status, activities, text, thinking, error: childError },
          };
          return next;
        });
      }),
      EventsOn("maiku:tool_end", (data) => {
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
      EventsOn("maiku:idle", (data) => {
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
        setError(null);
        setState((current) =>
          current
            ? { ...current, streaming: false, streamText: "", streamThinking: "" }
            : current,
        );
        // The transcript is already projected incrementally from agent events.
        // Re-fetching it here made every completed turn serialize and rebuild
        // the entire chat, which is especially painful for long sessions.
        ListSessions()
          .then((sess) => setSessions((sess as SessionSummary[]) || []))
          .catch(() => {});
      }),
      EventsOn("maiku:error", (data) => {
        const sid = data?.sessionId as string | undefined;
        if (sid) setStreamingSessionIds((prev) => clearStreaming(prev, sid));
        if (!isFocused(sid)) return;
        setError(data?.error || "Unknown error");
        setStreaming(false);
      }),
      EventsOn("maiku:mcp", (data) => {
        setState((current) =>
          current
            ? { ...current, mcp: normalizeMCP(data) }
            : current,
        );
      }),
      EventsOn("maiku:retry", (data) => {
        if (!isFocused(data?.sessionId)) return;
        const attempt = data?.attempt ?? 0;
        const maxAttempts = data?.maxAttempts ?? 0;
        const msg = data?.errorMessage || "Temporary error";
        setError(`Retrying (${attempt}/${maxAttempts}): ${msg}`);
      }),
    ];
    return () => {
      for (const off of offs) off?.();
    };
  }, [isFocused]);

  const onTranscriptScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.clientHeight - el.scrollTop;
    followTranscriptRef.current = distanceFromBottom <= SCROLL_BOTTOM_THRESHOLD;
  }, []);

  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (el && followTranscriptRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  });

  // Live Markdown is throttled inside the transcript, so some height changes
  // happen without a parent render. Follow those DOM updates as well.
  useEffect(() => {
    if (!state) return;
    const element = scrollRef.current;
    if (!element) return;
    const observer = new MutationObserver(() => {
      if (followTranscriptRef.current) element.scrollTop = element.scrollHeight;
    });
    observer.observe(element, { childList: true, characterData: true, subtree: true });
    return () => observer.disconnect();
  }, [state]);

  const onSend = async (text: string, images: ImageAttachment[] = []): Promise<boolean> => {
    setError(null);
    // Sending is an explicit request to start a new turn, so reveal it and
    // resume following even if the user had been reading farther up.
    followTranscriptRef.current = true;
    const optimisticId = `pending-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    setMessages((prev) => [...prev, { id: optimisticId, role: "user", text, images }]);
    setStreaming(true);
    if (focusedSessionRef.current) {
      setStreamingSessionIds((prev) => markStreaming(prev, focusedSessionRef.current));
    }
    try {
      await Prompt(text, images);
      return true;
    } catch (error: unknown) {
      // Keep the draft in the composer and remove the optimistic bubble so a
      // rejected prompt never looks as if it was accepted.
      setMessages((prev) => prev.filter((message) => message.id !== optimisticId));
      setError(errorMessage(error));
      setStreaming(false);
      if (focusedSessionRef.current) {
        setStreamingSessionIds((prev) => clearStreaming(prev, focusedSessionRef.current));
      }
      return false;
    }
  };

  const onCommand = async (command: string) => {
    if (command === "settings" || command.startsWith("settings ")) {
      const match = command.match(/^settings subagent (true|false)$/);
      if (!match) {
        setError("Usage: /settings subagent true|false");
        return;
      }
      const enabled = match[1] === "true";
      setError(null);
      try {
        await SetSubagentEnabled(enabled);
        setMessages((prev) => [
          ...prev,
          {
            id: `notice-subagent-${Date.now()}`,
            role: "notice",
            text: enabled ? "Subagents enabled" : "Subagents disabled",
          },
        ]);
      } catch (error: unknown) {
        setError(errorMessage(error));
      }
      return;
    }

    if (command === "goal" || command.startsWith("goal ")) {
      const raw = command === "goal" ? "" : command.slice("goal ".length);
      if (!raw.trim()) {
        setError("Usage: /goal <goal>[, <goal>...]");
        return;
      }
      setError(null);
      setStreaming(true);
      if (focusedSessionRef.current) {
        setStreamingSessionIds((prev) => markStreaming(prev, focusedSessionRef.current));
      }
      try {
        await Goal(raw);
      } catch (error: unknown) {
        setError(errorMessage(error));
        setStreaming(false);
        if (focusedSessionRef.current) {
          setStreamingSessionIds((prev) => clearStreaming(prev, focusedSessionRef.current));
        }
      }
      return;
    }

    if (command !== "compact") return;
    setError(null);
    setStreaming(true);
    if (focusedSessionRef.current) {
      setStreamingSessionIds((prev) => markStreaming(prev, focusedSessionRef.current));
    }
    try {
      await Compact();
    } catch (error: unknown) {
      setError(errorMessage(error));
      setStreaming(false);
      if (focusedSessionRef.current) {
        setStreamingSessionIds((prev) => clearStreaming(prev, focusedSessionRef.current));
      }
    }
  };

  const switchSession = async (fn: () => Promise<unknown>) => {
    refreshGenRef.current += 1;
    followTranscriptRef.current = true;
    // Ignore transcript events until refresh pins the new focus.
    focusedSessionRef.current = "";
    setStreamText("");
    setStreamThinking("");
    setThinkingStartedAt(null);
    streamRef.current = null;
    rateSamplesRef.current = [];
    setTokensPerSec(0);
    try {
      await fn();
      await refresh();
    } catch (switchError) {
      // Restore the previous focus if a picker was cancelled or an open failed.
      await refresh().catch(() => {});
      throw switchError;
    }
  };

  if (!state) {
    const retry = async () => {
      setRetryingStartup(true);
      setError(null);
      try {
        await refresh();
      } catch (startupError: unknown) {
        setError(errorMessage(startupError));
      } finally {
        setRetryingStartup(false);
      }
    };

    return (
      <div className="startup-screen">
        <div className="startup-mark" aria-hidden>m</div>
        {error ? (
          <div className="startup-card" role="alert">
            <h1>Couldn’t start maiku</h1>
            <p>{error}</p>
            <button type="button" onClick={() => void retry()} disabled={retryingStartup}>
              {retryingStartup ? "Retrying…" : "Try again"}
            </button>
          </div>
        ) : (
          <div className="startup-loading" role="status">
            <span className="startup-dot" />
            Loading your workspace…
          </div>
        )}
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
      onTranscriptScroll={onTranscriptScroll}
      recentDirs={state.recentDirs}
      onToggleSidebar={() => setSidebarOpen((v) => !v)}
      onToggleSettings={() => setSettingsOpen((v) => !v)}
      onSend={onSend}
      onResend={async (rawIndex) => {
        setError(null);
        setMessages((prev) => {
          const cut = prev.findIndex((m) => m.role === "user" && m.rawIndex === rawIndex);
          return cut >= 0 ? prev.slice(0, cut) : prev;
        });
        try {
          await ResendUserMessage(rawIndex);
          setStreaming(true);
        } catch (resendError: unknown) {
          setError(errorMessage(resendError));
          void refresh();
        }
      }}
      onCommand={onCommand}
      onAbort={async () => {
        try {
          await Abort();
          return true;
        } catch (abortError: unknown) {
          setError(errorMessage(abortError));
          return false;
        }
      }}
      onNewSession={async () => {
        try {
          await switchSession(() => NewSession());
        } catch (sessionError: unknown) {
          setError(errorMessage(sessionError));
        }
      }}
      onOpenFolder={async () => {
        try {
          await switchSession(() => OpenFolder());
        } catch (folderError: unknown) {
          setError(errorMessage(folderError));
        }
      }}
      onOpenRecentFolder={async (path) => {
        try {
          await switchSession(() => OpenRecentFolder(path));
        } catch (error: unknown) {
          setError(errorMessage(error));
        }
      }}
      onOpenSession={async (path) => {
        try {
          await switchSession(() => OpenSession(path));
        } catch (sessionError: unknown) {
          setError(errorMessage(sessionError));
        }
      }}
      onRenameSession={async (path, name) => {
        try {
          await RenameSession(path, name);
          await refresh();
        } catch (error: unknown) {
          setError(errorMessage(error));
        }
      }}
      onSetModel={async (provider, id) => {
        try {
          await SetModel(provider, id);
          await refresh();
        } catch (modelError: unknown) {
          setError(errorMessage(modelError));
        }
      }}
      onSetThinking={async (level) => {
        try {
          await SetThinking(level);
          await refresh();
        } catch (thinkingError: unknown) {
          setError(errorMessage(thinkingError));
        }
      }}
      onSaveKey={async (provider, key) => {
        await SetAPIKey(provider, key);
        await refresh();
      }}
      onProvidersChanged={async () => {
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

function persistedSubagentActivities(details: unknown): SubagentActivity[] {
  if (!details || typeof details !== "object") return [];
  const value = details as Record<string, unknown>;
  const raw = value.activities ?? value.Activities;
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((item): SubagentActivity[] => {
    if (!item || typeof item !== "object") return [];
    const activity = item as Record<string, unknown>;
    return [{
      toolCallId: typeof activity.toolCallId === "string" ? activity.toolCallId : "",
      toolName: typeof activity.toolName === "string" ? activity.toolName : "tool",
      input: typeof activity.input === "string" ? activity.input : undefined,
      output: typeof activity.output === "string" ? activity.output : undefined,
      status: activity.status === "running" || activity.status === "error" ? activity.status : "completed",
      isError: !!activity.isError,
    }];
  });
}

function subagentActionInput(toolName: unknown, rawArgs: unknown): string {
  const parsed = parseMaybeJSON(rawArgs);
  if (!parsed || typeof parsed !== "object") return "";
  const args = parsed as Record<string, unknown>;
  const name = typeof toolName === "string" ? toolName.toLowerCase() : "";
  let value: unknown;
  if (name === "bash") value = args.command;
  else if (name === "read" || name === "write" || name === "edit") value = args.path;
  else value = args.path ?? args.query ?? args.pattern;
  if (typeof value === "string") return truncate(value.trim(), 280);
  try {
    return truncate(JSON.stringify(args), 280);
  } catch {
    return "";
  }
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  return String(error);
}

function formatTokenCount(n: number): string {
  if (n >= 10000) return `${Math.round(n / 1000)}k`;
  if (n >= 1000) return `${(n / 1000).toFixed(1).replace(/\.0$/, "")}k`;
  return String(n);
}

function formatCompactNotice(removed: number, before: number, after: number): string {
  const parts = ["Compacted"];
  if (removed > 0) {
    parts.push(`${removed} message${removed === 1 ? "" : "s"}`);
  }
  if (before > 0 && after >= 0 && after < before) {
    parts.push(`${formatTokenCount(before)} → ${formatTokenCount(after)} tokens`);
  }
  return parts.join(" · ");
}

function truncate(s: string, n: number) {
  if (s.length <= n) return s;
  return `${s.slice(0, n)}…`;
}

function parsePartialResult(value: unknown): { text: string; details?: unknown } {
  if (typeof value === "string") return { text: value };
  if (!value || typeof value !== "object") return { text: "" };
  const result = value as Record<string, unknown>;
  const content = result.content ?? result.Content;
  const text = Array.isArray(content)
    ? content.flatMap((item): string[] => {
        if (!item || typeof item !== "object") return [];
        const block = item as Record<string, unknown>;
        const blockText = block.text ?? block.Text;
        return typeof blockText === "string" && blockText ? [blockText] : [];
      }).join("\n")
    : "";
  return { text, details: result.details ?? result.Details };
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
