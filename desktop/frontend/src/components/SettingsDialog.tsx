import { useMemo, useState } from "react";
import { Code2, KeyRound, Search, X } from "lucide-react";
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
  const [tab, setTab] = useState<"providers" | "miru">("providers");
  const [codexBusy, setCodexBusy] = useState(false);
  const [codexInfo, setCodexInfo] = useState<{ userCode: string; verificationUri: string } | null>(null);
  const [codexError, setCodexError] = useState<string | null>(null);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const providers = keys.filter((k) => k.provider !== "miru");
    const matching = !q ? providers : providers.filter((k) => {
      const name = (k.name || "").toLowerCase();
      const id = k.provider.toLowerCase();
      return name.includes(q) || id.includes(q);
    });
    return [...matching.filter((k) => k.hasKey), ...matching.filter((k) => !k.hasKey)];
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

  const miruKey = keys.find((k) => k.provider === "miru");

  return (
    <div className="fixed inset-0 z-50 bg-[var(--color-panel)]">
      <div className="flex h-full min-h-0 w-full flex-col overflow-hidden">
        <header
          data-wails-drag
          className="titlebar-drag relative z-40 flex h-12 shrink-0 items-center justify-between border-b border-[var(--color-line)] pr-3 pl-[96px]"
        >
          <h2 className="text-sm font-semibold leading-none tracking-tight">Settings</h2>
          <button
            type="button"
            data-wails-no-drag
            onClick={() => {
              void codexLogin?.cancel();
              onClose();
            }}
            className="titlebar-no-drag flex h-7 w-7 items-center justify-center rounded-md text-[var(--color-muted)] outline-none transition hover:bg-[var(--color-panel-2)] hover:text-[var(--color-text)] focus-visible:ring-2 focus-visible:ring-[var(--color-accent-dim)]"
            title="Close settings"
            aria-label="Close settings"
          >
            <X size={15} />
          </button>
        </header>

        <div className="flex min-h-0 flex-1">
          <nav className="w-56 shrink-0 border-r border-[var(--color-line)] px-3 py-4">
            <button type="button" onClick={() => setTab("providers")} className={`mb-1 flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-left text-xs font-medium outline-none transition focus-visible:ring-2 focus-visible:ring-[var(--color-accent-dim)] ${tab === "providers" ? "bg-[var(--color-panel-2)] text-[var(--color-fg)]" : "text-[var(--color-muted)] hover:bg-[var(--color-panel-2)] hover:text-[var(--color-fg)]"}`}>
              <KeyRound size={14} /> Providers
            </button>
            <button type="button" onClick={() => setTab("miru")} className={`flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-left text-xs font-medium outline-none transition focus-visible:ring-2 focus-visible:ring-[var(--color-accent-dim)] ${tab === "miru" ? "bg-[var(--color-panel-2)] text-[var(--color-fg)]" : "text-[var(--color-muted)] hover:bg-[var(--color-panel-2)] hover:text-[var(--color-fg)]"}`}>
              <Code2 size={14} /> Miru Code
            </button>
          </nav>

          <section className="flex min-h-0 min-w-0 flex-1 flex-col">
        {tab === "providers" ? <>
        <div className="border-b border-[var(--color-line)] px-8 py-6">
          <div className="mx-auto flex w-full max-w-4xl items-end justify-between gap-6">
            <div>
              <h3 className="text-lg font-semibold tracking-tight">Providers</h3>
              <p className="mt-1 text-xs text-[var(--color-muted)]">Connect the model providers you want to use.</p>
            </div>
            <div className="flex w-72 items-center gap-2 rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] px-3 py-2 transition focus-within:border-[var(--color-accent)]">
              <Search size={14} className="shrink-0 text-[var(--color-muted)]" />
              <input
                type="search"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search providers…"
                className="min-w-0 flex-1 appearance-none border-0 bg-transparent p-0 text-xs outline-none ring-0 shadow-none placeholder:text-[var(--color-muted)] focus:border-0 focus:outline-none focus:ring-0 focus-visible:outline-none"
                style={{ outline: "none", boxShadow: "none" }}
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
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-8 py-6">
          <div className="mx-auto w-full max-w-4xl space-y-3 pb-8">
            {filtered.length === 0 && (
              <p className="py-6 text-center text-xs text-[var(--color-muted)]">
                No providers match “{query.trim()}”
              </p>
            )}
            {filtered.map((k, index) => {
              const isCodex = k.provider === "openai-codex";
              const placeholder = isCodex
                ? k.hasKey
                  ? "•••••••• (leave blank to keep)"
                  : "ChatGPT OAuth access token (optional)"
                : k.provider === "miru"
                  ? k.hasKey
                    ? "•••••••• (leave blank to keep)"
                    : "Takara API key"
                  : k.hasKey
                  ? "•••••••• (leave blank to keep)"
                  : "sk-…";
              return (
                <div key={k.provider}>
                {(index === 0 || filtered[index - 1].hasKey !== k.hasKey) && (
                  <div className={`${index === 0 ? "" : "mt-8"} mb-3 text-[10px] font-semibold uppercase tracking-[0.16em] text-[var(--color-muted)]`}>
                    {k.hasKey ? "Configured" : "Available providers"}
                  </div>
                )}
                <div className="rounded-xl border border-[var(--color-line)] bg-[var(--color-panel-2)]/35 p-4 transition-colors hover:border-[color-mix(in_srgb,var(--color-line)_65%,var(--color-muted))]">
                  <div className="mb-3 flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="truncate text-sm font-medium">
                        {k.name || k.provider}
                      </div>
                      {k.name && k.name !== k.provider && (
                        <div className="font-mono text-[10px] text-[var(--color-muted)]">
                          {k.provider}
                        </div>
                      )}
                    </div>
                    <span className={`mt-0.5 shrink-0 rounded-full px-2 py-0.5 text-[10px] ${k.hasKey ? "bg-emerald-500/10 text-emerald-400" : "bg-[var(--color-panel-2)] text-[var(--color-muted)]"}`}>
                      {k.hasKey ? "Connected" : "Not connected"}
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
                      className="min-w-0 flex-1 rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] px-3 py-2 font-mono text-xs outline-none transition focus:border-[var(--color-accent)]"
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
                      className="rounded-lg bg-[var(--color-accent)] px-4 py-2 text-xs font-medium text-[var(--color-ink)] transition hover:brightness-110 disabled:opacity-40 disabled:hover:brightness-100"
                    >
                      Save
                    </button>
                  </div>
                </div>
                </div>
              );
            })}
          </div>
        </div>
        </> : (
          <div className="min-h-0 flex-1 overflow-y-auto p-6">
            <div className="mx-auto max-w-2xl">
              <div className="mb-6 flex items-start gap-3">
                <div className="rounded-lg bg-[var(--color-panel-2)] p-2 text-[var(--color-accent)]"><Code2 size={18} /></div>
                <div><h3 className="text-sm font-semibold">Miru Code Search</h3><p className="mt-1 text-xs leading-5 text-[var(--color-muted)]">Semantic code search for your workspace. Miru uses your Takara API key to index and understand code.</p></div>
              </div>
              {miruKey ? <div className="rounded-xl border border-[var(--color-line)] bg-[var(--color-panel-2)] p-4">
                <div className="mb-3"><div className="text-xs font-medium">Takara API key</div><div className="mt-1 text-[10px] text-[var(--color-muted)]">{miruKey.hasKey ? `set via ${miruKey.source || "file"}` : "not set"}</div></div>
                <div className="flex gap-2"><input type="password" placeholder={miruKey.hasKey ? "•••••••• (leave blank to keep)" : "Enter Takara API key"} value={drafts.miru ?? ""} onChange={(e) => setDrafts((d) => ({ ...d, miru: e.target.value }))} className="flex-1 rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] px-3 py-2 font-mono text-xs outline-none transition focus:border-[var(--color-accent-dim)] focus:ring-2 focus:ring-[var(--color-accent-dim)]" /><button type="button" disabled={saving === "miru" || !(drafts.miru ?? "").trim()} onClick={async () => { const value = (drafts.miru ?? "").trim(); if (!value) return; setSaving("miru"); try { await onSave("miru", value); setDrafts((d) => ({ ...d, miru: "" })); } finally { setSaving(null); } }} className="rounded-lg bg-[var(--color-accent)] px-4 py-2 text-xs font-medium text-[var(--color-ink)] disabled:opacity-40">Save key</button></div>
              </div> : <p className="text-xs text-[var(--color-muted)]">Miru is not available in this build.</p>}
            </div>
          </div>
        )}
          </section>
        </div>
      </div>
    </div>
  );
}
