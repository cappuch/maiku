import { useMemo, useState } from "react";
import { Search, X } from "lucide-react";
import type { APIKeyStatus } from "../types";

export type CodexLoginHandlers = {
  begin: () => Promise<{ userCode: string; verificationUri: string }>;
  finish: () => Promise<void>;
  cancel: () => Promise<void> | void;
};

export function SettingsDialog({
  keys,
  onSave,
  onClose,
  codexLogin,
}: {
  keys: APIKeyStatus[];
  onSave: (provider: string, key: string) => Promise<void> | void;
  onClose: () => void;
  codexLogin?: CodexLoginHandlers;
}) {
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [codexBusy, setCodexBusy] = useState(false);
  const [codexInfo, setCodexInfo] = useState<{ userCode: string; verificationUri: string } | null>(null);
  const [codexError, setCodexError] = useState<string | null>(null);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return keys;
    return keys.filter((k) => {
      const name = (k.name || "").toLowerCase();
      const id = k.provider.toLowerCase();
      return name.includes(q) || id.includes(q);
    });
  }, [keys, query]);

  const startCodexLogin = async () => {
    if (!codexLogin) return;
    setCodexError(null);
    setCodexBusy(true);
    try {
      const info = await codexLogin.begin();
      setCodexInfo(info);
      // Open verification page when possible.
      try {
        window.open(info.verificationUri, "_blank", "noopener,noreferrer");
      } catch {
        /* ignore */
      }
      await codexLogin.finish();
      setCodexInfo(null);
    } catch (err) {
      setCodexError(err instanceof Error ? err.message : String(err));
    } finally {
      setCodexBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="flex max-h-[80vh] w-full max-w-lg flex-col overflow-hidden rounded-xl border border-[var(--color-line)] bg-[var(--color-panel)] shadow-2xl">
        <div className="flex items-center justify-between border-b border-[var(--color-line)] px-4 py-3">
          <div>
            <h2 className="text-sm font-semibold">Settings</h2>
            <p className="text-xs text-[var(--color-muted)]">
              API keys are stored in ~/.maiku/agent/auth.json
            </p>
          </div>
          <button
            type="button"
            onClick={() => {
              void codexLogin?.cancel();
              onClose();
            }}
            className="rounded-md p-1 text-[var(--color-muted)] hover:bg-[var(--color-panel-2)]"
          >
            <X size={16} />
          </button>
        </div>

        <div className="border-b border-[var(--color-line)] px-4 py-2">
          <div className="flex items-center gap-2 rounded-md border border-[var(--color-line)] bg-[var(--color-ink)] px-2 py-1.5">
            <Search size={14} className="shrink-0 text-[var(--color-muted)]" />
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search providers…"
              autoFocus
              className="min-w-0 flex-1 bg-transparent text-xs outline-none placeholder:text-[var(--color-muted)]"
            />
            {query && (
              <button
                type="button"
                onClick={() => setQuery("")}
                className="rounded p-0.5 text-[var(--color-muted)] hover:text-[var(--color-fg)]"
                aria-label="Clear search"
              >
                <X size={12} />
              </button>
            )}
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-4">
          <div className="space-y-3">
            {filtered.length === 0 && (
              <p className="py-6 text-center text-xs text-[var(--color-muted)]">
                No providers match “{query.trim()}”
              </p>
            )}
            {filtered.map((k) => {
              const isCodex = k.provider === "openai-codex";
              const placeholder = isCodex
                ? k.hasKey
                  ? "•••••••• (leave blank to keep)"
                  : "ChatGPT OAuth access token (optional)"
                : k.hasKey
                  ? "•••••••• (leave blank to keep)"
                  : "sk-…";
              return (
                <div key={k.provider} className="rounded-lg border border-[var(--color-line)] p-3">
                  <div className="mb-2 flex items-center justify-between gap-2">
                    <div className="min-w-0">
                      <div className="truncate text-xs font-medium">
                        {k.name || k.provider}
                      </div>
                      {k.name && k.name !== k.provider && (
                        <div className="font-mono text-[10px] text-[var(--color-muted)]">
                          {k.provider}
                        </div>
                      )}
                    </div>
                    <span className="shrink-0 text-[10px] text-[var(--color-muted)]">
                      {k.hasKey ? `set via ${k.source || "file"}` : "not set"}
                    </span>
                  </div>

                  {isCodex && codexLogin && (
                    <div className="mb-2 space-y-2">
                      <button
                        type="button"
                        disabled={codexBusy}
                        onClick={() => void startCodexLogin()}
                        className="rounded-md border border-[var(--color-line)] bg-[var(--color-panel-2)] px-3 py-1.5 text-xs font-medium hover:border-[var(--color-accent-dim)] disabled:opacity-40"
                      >
                        {codexBusy ? "Waiting for ChatGPT login…" : k.hasKey ? "Re-sign in with ChatGPT" : "Sign in with ChatGPT"}
                      </button>
                      {codexInfo && (
                        <p className="text-[11px] text-[var(--color-muted)]">
                          Enter code{" "}
                          <span className="font-mono text-[var(--color-fg)]">{codexInfo.userCode}</span> at{" "}
                          <a
                            href={codexInfo.verificationUri}
                            target="_blank"
                            rel="noreferrer"
                            className="underline"
                          >
                            {codexInfo.verificationUri}
                          </a>
                        </p>
                      )}
                      {codexError && (
                        <p className="text-[11px] text-red-400">{codexError}</p>
                      )}
                    </div>
                  )}

                  <div className="flex gap-2">
                    <input
                      type="password"
                      placeholder={placeholder}
                      value={drafts[k.provider] ?? ""}
                      onChange={(e) =>
                        setDrafts((d) => ({ ...d, [k.provider]: e.target.value }))
                      }
                      className="flex-1 rounded-md border border-[var(--color-line)] bg-[var(--color-ink)] px-2 py-1.5 font-mono text-xs outline-none focus:border-[var(--color-accent-dim)]"
                    />
                    <button
                      type="button"
                      disabled={saving === k.provider || !(drafts[k.provider] ?? "").trim()}
                      onClick={async () => {
                        const value = (drafts[k.provider] ?? "").trim();
                        if (!value) return;
                        setSaving(k.provider);
                        try {
                          await onSave(k.provider, value);
                          setDrafts((d) => ({ ...d, [k.provider]: "" }));
                        } finally {
                          setSaving(null);
                        }
                      }}
                      className="rounded-md bg-[var(--color-accent)] px-3 py-1.5 text-xs font-medium text-[var(--color-ink)] disabled:opacity-40"
                    >
                      Save
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
