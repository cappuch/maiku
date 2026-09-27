import { useRef } from "react";
import { createRoot } from "react-dom/client";
import { AppShell } from "./components/AppShell";
import type { APIKeyStatus, AppState, ModelInfo, SessionSummary, UIMessage } from "./types";
import { emptyUsage } from "./types";
import "./style.css";

const cwd = "/Users/mikus/Desktop/maiku";

const models: ModelInfo[] = [
  { provider: "anthropic", id: "claude-opus-4.6", name: "Claude Opus", reasoning: true, vision: true, hasKey: true },
  { provider: "openai", id: "gpt-5", name: "GPT-5", reasoning: true, vision: true, hasKey: false },
  { provider: "google", id: "gemini-2.5-pro", name: "Gemini 2.5 Pro", reasoning: true, vision: true, hasKey: true },
];

const keys: APIKeyStatus[] = [
  { provider: "anthropic", name: "Anthropic", hasKey: true, source: "keychain" },
  { provider: "openai", name: "OpenAI", hasKey: false, source: "" },
  { provider: "google", name: "Google", hasKey: true, source: "env" },
  { provider: "openai-codex", name: "ChatGPT", hasKey: false, source: "" },
];

const sessions: SessionSummary[] = [
  { id: "s1", path: "/sessions/s1", timestamp: "2026-09-27T15:40:00Z", cwd, preview: "You have been given goals to complete. Write them down and do not stop until each one is done.", modTime: "2026-09-27T15:42:00Z" },
  { id: "s2", path: "/sessions/s2", timestamp: "2026-09-27T12:10:00Z", cwd, preview: "fix cross platform, sh is being done on windows and the scripts still assume bash", modTime: "2026-09-27T12:18:00Z" },
  { id: "s3", path: "/sessions/s3", timestamp: "2026-09-26T21:02:00Z", cwd, preview: "When chats and the agent run so long as fuck, get rid of the gradients and the ill M logo", modTime: "2026-09-26T21:20:00Z" },
  { id: "s4", path: "/sessions/s4", timestamp: "2026-09-25T09:12:00Z", cwd, preview: "go recursively through the codebase and find every place the sidebar clips session titles", modTime: "2026-09-25T09:40:00Z" },
];

const messages: UIMessage[] = [
  {
    id: "u1",
    role: "user",
    text: "Walk me through how sessions are stored, then make the empty state feel like a product.",
    rawIndex: 0,
  },
  {
    id: "t1",
    role: "tool",
    toolName: "read",
    toolCallId: "read-1",
    args: { path: "desktop/frontend/src/components/AppShell.tsx" },
    text: "export function AppShell(props: Props) {\n  const greeting = greetingFor(state.userName);\n  return (\n    <div className=\"app-shell\">\n      …\n    </div>\n  );\n}",
  },
  {
    id: "e1",
    role: "tool",
    toolName: "edit",
    toolCallId: "edit-1",
    args: { path: "desktop/frontend/src/style.css" },
    details: {
      diff: [
        "-18   --color-accent: #f0b75a;",
        "+18   --color-accent: #f4f4f5;",
        " 19   --color-accent-dim: #a1a1aa;",
        "-20   --color-ok: #5ecf8e;",
        "+20   --color-ok: #f4f4f5;",
      ].join("\n"),
    },
    text: "updated tokens",
  },
  {
    id: "a1",
    role: "assistant",
    thinking: "The gold accent is doing too much work. A zinc ramp, a persistent session column, and a floating composer will carry the hierarchy without a hue.",
    text: [
      "Sessions live as files under the workspace. The shell was hiding them behind a menu, so the list now stays on the left.",
      "",
      "The empty state leads with a greeting, then three ways in. The composer floats on the same grey ramp — no gold, no violet.",
      "",
      "```css",
      ":root {",
      "  --color-ink: #070708;",
      "  --color-text: #f4f4f5;",
      "  --color-accent: #f4f4f5;",
      "}",
      "```",
    ].join("\n"),
  },
];

function scene(): "empty" | "chat" | "settings" {
  const hash = window.location.hash.replace("#", "");
  if (hash === "chat" || hash === "settings") return hash;
  return "empty";
}

function Preview() {
  const scrollRef = useRef<HTMLDivElement>(null);
  const mode = scene();
  const usage = emptyUsage();
  usage.input = 18420;
  usage.output = 2310;
  usage.totalTokens = 20730;
  usage.cost = 0.084;
  usage.totalCost = 0.091;

  const state: AppState = {
    cwd,
    folderName: "maiku",
    userName: "mikus",
    provider: "anthropic",
    modelId: "claude-opus-4.6",
    modelName: "Claude Opus",
    thinking: "medium",
    streaming: false,
    sessionId: mode === "empty" ? "" : "s2",
    sessionPath: mode === "empty" ? "" : "/sessions/s2",
    usage,
    hasApiKey: true,
    messages: mode === "chat" || mode === "settings" ? messages : [],
    recentDirs: [cwd, "/Users/mikus/Desktop", "/Users/mikus/src/notes"],
    streamingSessionIds: ["s1"],
    streamText: "",
    streamThinking: "",
    mcp: { configured: 1, connected: 1, failed: 0, servers: [] },
  };

  return (
    <>
      <div className="pointer-events-none fixed top-0 left-0 z-[60] flex h-12 items-center gap-2 pl-5" aria-hidden>
        <span className="h-3 w-3 rounded-full bg-zinc-700" />
        <span className="h-3 w-3 rounded-full bg-zinc-600" />
        <span className="h-3 w-3 rounded-full bg-zinc-500" />
      </div>
      <AppShell
        state={state}
        usage={mode === "empty" ? emptyUsage() : usage}
        messages={mode === "empty" ? [] : messages}
        sessions={sessions}
        models={models}
        keys={keys}
        streaming={false}
        streamingSessionIds={["s1"]}
        streamText=""
        streamThinking=""
        thinkingStartedAt={null}
        tokensPerSec={0}
        sidebarOpen
        settingsOpen={mode === "settings"}
        error={null}
        scrollRef={scrollRef}
        onTranscriptScroll={() => {}}
        recentDirs={state.recentDirs}
        onToggleSidebar={() => {}}
        onToggleSettings={() => {
          window.location.hash = mode === "settings" ? "chat" : "settings";
          window.location.reload();
        }}
        onSend={async () => true}
        messageQueue={[]}
        onRemoveQueued={() => {}}
        onClearQueue={() => {}}
        onCommand={() => {}}
        onAbort={async () => true}
        onNewSession={() => {}}
        onOpenFolder={() => {}}
        onOpenRecentFolder={() => {}}
        onOpenSession={() => {}}
        onRenameSession={() => {}}
        onSetModel={() => {}}
        onSetThinking={() => {}}
        onSaveKey={() => {}}
        onResend={() => {}}
        onDismissError={() => {}}
        codexLogin={{
          begin: async () => ({ userCode: "ABCD-EFGH", verificationUri: "https://auth.openai.com" }),
          finish: async () => {},
          cancel: () => {},
        }}
      />
    </>
  );
}

const root = document.getElementById("root");
if (!root) throw new Error("root element not found");
createRoot(root).render(<Preview />);
