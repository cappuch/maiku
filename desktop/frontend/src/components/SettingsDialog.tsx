import { useEffect, useMemo, useRef, useState } from "react";
import { Code2, KeyRound, Plus, RefreshCw, Search, Server, Trash2, X } from "lucide-react";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";
import {
  ListCustomProviders,
  ListMCPServers,
  ReloadMCP,
  RemoveCustomProvider,
  RemoveMCPServer,
  SetMCPServerEnabled,
  UpsertCustomProvider,
  UpsertMCPServer,
} from "../../wailsjs/go/main/App";
import type { APIKeyStatus, CustomProvider, MCPServerStatus } from "../types";

export type CodexLoginHandlers = {
  begin: () => Promise<{ userCode: string; verificationUri: string }>;
  finish: () => Promise<void>;
  cancel: () => Promise<void> | void;
};

type SettingsTab = "providers" | "miru" | "mcp";

export function SettingsDialog({
  keys,
  onSave,
  onClose,
  onProvidersChanged,
  codexLogin,
  initialTab = "providers",
}: {
  keys: APIKeyStatus[];
  onSave: (provider: string, key: string) => Promise<void> | void;
  onClose: () => void;
  onProvidersChanged?: () => Promise<void> | void;
  codexLogin?: CodexLoginHandlers;
  initialTab?: SettingsTab;
}) {
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [tab, setTab] = useState<SettingsTab>(initialTab);

  useEffect(() => {
    setTab(initialTab);
  }, [initialTab]);
  const [codexBusy, setCodexBusy] = useState(false);
  const [codexInfo, setCodexInfo] = useState<{ userCode: string; verificationUri: string } | null>(null);
  const [codexError, setCodexError] = useState<string | null>(null);
  const [saveErrors, setSaveErrors] = useState<Record<string, string>>({});
  const dialogRef = useRef<HTMLDivElement>(null);
  const closeRef = useRef<() => void>(() => {});

  closeRef.current = () => {
    void codexLogin?.cancel();
    onClose();
  };

  useEffect(() => {
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const dialog = dialogRef.current;
    const focusable = dialog?.querySelector<HTMLElement>(
      "button:not([disabled]), input:not([disabled]), [href], [tabindex]:not([tabindex='-1'])",
    );
    focusable?.focus();

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        closeRef.current();
        return;
      }
      if (event.key !== "Tab" || !dialog) return;
      const items = Array.from(dialog.querySelectorAll<HTMLElement>(
        "button:not([disabled]), input:not([disabled]), [href], [tabindex]:not([tabindex='-1'])",
      )).filter((item) => item.offsetParent !== null);
      if (items.length === 0) return;
      const first = items[0];
      const last = items[items.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };

    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      previousFocus?.focus();
    };
  }, []);

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
      // Open verification page in the system browser.
      try {
        BrowserOpenURL(info.verificationUri);
      } catch {
        window.open(info.verificationUri, "_blank", "noopener,noreferrer");
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
    <div
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-labelledby="settings-title"
      className="fixed inset-0 z-50 bg-[var(--color-panel)]"
    >
      <div className="flex h-full min-h-0 w-full flex-col overflow-hidden">
        <header
          data-wails-drag
          className="titlebar-drag relative z-40 flex h-12 shrink-0 items-center justify-between border-b border-[var(--color-line)] pr-3 pl-[96px]"
        >
          <h2 id="settings-title" className="text-sm font-semibold leading-none tracking-tight">Settings</h2>
          <button
            type="button"
            data-wails-no-drag
            onClick={() => closeRef.current()}
            className="titlebar-no-drag flex h-7 w-7 items-center justify-center rounded-md text-[var(--color-muted)] outline-none transition hover:bg-[var(--color-panel-2)] hover:text-[var(--color-text)] focus-visible:ring-2 focus-visible:ring-[var(--color-accent-dim)]"
            title="Close settings"
            aria-label="Close settings"
          >
            <X size={15} />
          </button>
        </header>

        <div className="flex min-h-0 flex-1">
          <nav aria-label="Settings sections" className="w-56 shrink-0 border-r border-[var(--color-line)] px-3 py-4">
            <button type="button" onClick={() => setTab("providers")} className={`mb-1 flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-left text-xs font-medium outline-none transition focus-visible:ring-2 focus-visible:ring-[var(--color-accent-dim)] ${tab === "providers" ? "bg-[var(--color-panel-2)] text-[var(--color-text)]" : "text-[var(--color-muted)] hover:bg-[var(--color-panel-2)] hover:text-[var(--color-text)]"}`}>
              <KeyRound size={14} /> Providers
            </button>
            <button type="button" onClick={() => setTab("miru")} className={`mb-1 flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-left text-xs font-medium outline-none transition focus-visible:ring-2 focus-visible:ring-[var(--color-accent-dim)] ${tab === "miru" ? "bg-[var(--color-panel-2)] text-[var(--color-text)]" : "text-[var(--color-muted)] hover:bg-[var(--color-panel-2)] hover:text-[var(--color-text)]"}`}>
              <Code2 size={14} /> Miru Code
            </button>
            <button type="button" onClick={() => setTab("mcp")} className={`flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-left text-xs font-medium outline-none transition focus-visible:ring-2 focus-visible:ring-[var(--color-accent-dim)] ${tab === "mcp" ? "bg-[var(--color-panel-2)] text-[var(--color-text)]" : "text-[var(--color-muted)] hover:bg-[var(--color-panel-2)] hover:text-[var(--color-text)]"}`}>
              <Server size={14} /> MCP
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
                aria-label="Search providers"
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search providers…"
                className="min-w-0 flex-1 appearance-none border-0 bg-transparent p-0 text-xs outline-none ring-0 shadow-none placeholder:text-[var(--color-muted)] focus:border-0 focus:outline-none focus:ring-0 focus-visible:outline-none"
                style={{ outline: "none", boxShadow: "none" }}
              />
              {query && (
                <button
                  type="button"
                  onClick={() => setQuery("")}
                  className="rounded p-0.5 text-[var(--color-muted)] hover:text-[var(--color-text)]"
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
            <CustomProvidersPanel onChanged={() => { void onProvidersChanged?.(); }} />
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
                          <span className="font-mono text-[var(--color-text)]">{codexInfo.userCode}</span> at{" "}
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
                      aria-label={`${k.name || k.provider} API key`}
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
                        setSaveErrors((errors) => ({ ...errors, [k.provider]: "" }));
                        try {
                          await onSave(k.provider, value);
                          setDrafts((d) => ({ ...d, [k.provider]: "" }));
                        } catch (error) {
                          setSaveErrors((errors) => ({
                            ...errors,
                            [k.provider]: error instanceof Error ? error.message : String(error),
                          }));
                        } finally {
                          setSaving(null);
                        }
                      }}
                      className="rounded-lg bg-[var(--color-accent)] px-4 py-2 text-xs font-medium text-[var(--color-ink)] transition hover:brightness-110 disabled:opacity-40 disabled:hover:brightness-100"
                    >
                      Save
                    </button>
                  </div>
                  {saveErrors[k.provider] ? (
                    <p role="alert" className="mt-2 text-[11px] text-[var(--color-danger)]">
                      {saveErrors[k.provider]}
                    </p>
                  ) : null}
                </div>
                </div>
              );
            })}
          </div>
        </div>
        </> : tab === "miru" ? (
          <div className="min-h-0 flex-1 overflow-y-auto p-6">
            <div className="mx-auto max-w-2xl">
              <div className="mb-6 flex items-start gap-3">
                <div className="rounded-lg bg-[var(--color-panel-2)] p-2 text-[var(--color-accent)]"><Code2 size={18} /></div>
                <div><h3 className="text-sm font-semibold">Miru Code Search</h3><p className="mt-1 text-xs leading-5 text-[var(--color-muted)]">Semantic code search for your workspace. Miru uses your Takara API key to index and understand code.</p></div>
              </div>
              {miruKey ? (
                <div className="rounded-xl border border-[var(--color-line)] bg-[var(--color-panel-2)] p-4">
                  <div className="mb-3">
                    <div className="text-xs font-medium">Takara API key</div>
                    <div className="mt-1 text-[10px] text-[var(--color-muted)]">
                      {miruKey.hasKey ? `set via ${miruKey.source || "file"}` : "not set"}
                    </div>
                  </div>
                  <div className="flex gap-2">
                    <input
                      type="password"
                      aria-label="Takara API key"
                      placeholder={miruKey.hasKey ? "•••••••• (leave blank to keep)" : "Enter Takara API key"}
                      value={drafts.miru ?? ""}
                      onChange={(e) => setDrafts((draft) => ({ ...draft, miru: e.target.value }))}
                      className="flex-1 rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] px-3 py-2 font-mono text-xs outline-none transition focus:border-[var(--color-accent-dim)] focus:ring-2 focus:ring-[var(--color-accent-dim)]"
                    />
                    <button
                      type="button"
                      disabled={saving === "miru" || !(drafts.miru ?? "").trim()}
                      onClick={async () => {
                        const value = (drafts.miru ?? "").trim();
                        if (!value) return;
                        setSaving("miru");
                        setSaveErrors((errors) => ({ ...errors, miru: "" }));
                        try {
                          await onSave("miru", value);
                          setDrafts((draft) => ({ ...draft, miru: "" }));
                        } catch (error) {
                          setSaveErrors((errors) => ({
                            ...errors,
                            miru: error instanceof Error ? error.message : String(error),
                          }));
                        } finally {
                          setSaving(null);
                        }
                      }}
                      className="rounded-lg bg-[var(--color-accent)] px-4 py-2 text-xs font-medium text-[var(--color-ink)] disabled:opacity-40"
                    >
                      Save key
                    </button>
                  </div>
                  {saveErrors.miru ? (
                    <p role="alert" className="mt-2 text-[11px] text-[var(--color-danger)]">{saveErrors.miru}</p>
                  ) : null}
                </div>
              ) : (
                <p className="text-xs text-[var(--color-muted)]">Miru is not available in this build.</p>
              )}
            </div>
          </div>
        ) : (
          <MCPSettingsPane />
        )}
          </section>
        </div>
      </div>
    </div>
  );
}

function CustomProvidersPanel({ onChanged }: { onChanged?: () => void }) {
  const [custom, setCustom] = useState<CustomProvider[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [modelsText, setModelsText] = useState("");

  const refresh = async () => {
    try {
      const list = await ListCustomProviders();
      setCustom(list ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  useEffect(() => {
    void refresh();
  }, []);

  const resetForm = () => {
    setShowForm(false);
    setId("");
    setName("");
    setBaseUrl("");
    setModelsText("");
  };

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      await UpsertCustomProvider({
        id: id.trim(),
        name: name.trim() || id.trim(),
        baseUrl: baseUrl.trim(),
        api: "openai-completions",
        models: modelsText
          .split(/[,\n]/)
          .map((m) => m.trim())
          .filter(Boolean),
      });
      resetForm();
      await refresh();
      onChanged?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mb-6 rounded-xl border border-[var(--color-line)] bg-[var(--color-panel-2)] p-4">
      <div className="mb-3 flex items-start justify-between gap-3">
        <div>
          <h4 className="text-xs font-semibold">Custom OpenAI-compatible routes</h4>
          <p className="mt-1 text-[11px] leading-5 text-[var(--color-muted)]">
            Point maiku at any OpenAI-compatible <span className="font-mono">/v1</span> endpoint (Ollama, vLLM, LiteLLM, etc).
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowForm(true)}
          className="flex items-center gap-1 rounded-lg bg-[var(--color-accent)] px-3 py-1.5 text-[11px] font-medium text-[var(--color-ink)]"
        >
          <Plus size={12} />
          Add route
        </button>
      </div>

      {error ? <p role="alert" className="mb-2 text-[11px] text-[var(--color-danger)]">{error}</p> : null}

      {showForm ? (
        <div className="mb-3 grid gap-2 rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] p-3">
          <input
            value={id}
            onChange={(e) => setId(e.target.value)}
            placeholder="id (e.g. ollama)"
            className="rounded-md border border-[var(--color-line)] bg-[var(--color-panel)] px-2.5 py-1.5 font-mono text-xs outline-none focus:border-[var(--color-accent-dim)]"
          />
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Display name"
            className="rounded-md border border-[var(--color-line)] bg-[var(--color-panel)] px-2.5 py-1.5 text-xs outline-none focus:border-[var(--color-accent-dim)]"
          />
          <input
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder="https://localhost:11434/v1"
            className="rounded-md border border-[var(--color-line)] bg-[var(--color-panel)] px-2.5 py-1.5 font-mono text-xs outline-none focus:border-[var(--color-accent-dim)]"
          />
          <input
            value={modelsText}
            onChange={(e) => setModelsText(e.target.value)}
            placeholder="Optional model ids (comma-separated)"
            className="rounded-md border border-[var(--color-line)] bg-[var(--color-panel)] px-2.5 py-1.5 font-mono text-xs outline-none focus:border-[var(--color-accent-dim)]"
          />
          <div className="flex gap-2">
            <button
              type="button"
              disabled={busy}
              onClick={() => void save()}
              className="rounded-md bg-[var(--color-accent)] px-3 py-1.5 text-[11px] font-medium text-[var(--color-ink)] disabled:opacity-40"
            >
              {busy ? "Saving…" : "Save"}
            </button>
            <button
              type="button"
              onClick={resetForm}
              className="rounded-md border border-[var(--color-line)] px-3 py-1.5 text-[11px] text-[var(--color-muted)]"
            >
              Cancel
            </button>
          </div>
        </div>
      ) : null}

      {custom.length === 0 ? (
        <p className="text-[11px] text-[var(--color-muted)]">No custom routes yet.</p>
      ) : (
        <ul className="space-y-2">
          {custom.map((provider) => (
            <li key={provider.id} className="flex items-start justify-between gap-3 rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] px-3 py-2">
              <div className="min-w-0">
                <div className="text-xs font-medium">{provider.name || provider.id}</div>
                <div className="mt-0.5 truncate font-mono text-[10px] text-[var(--color-muted)]">{provider.baseUrl}</div>
                {provider.models?.length ? (
                  <div className="mt-0.5 text-[10px] text-[var(--color-muted)]">{provider.models.join(", ")}</div>
                ) : null}
              </div>
              <button
                type="button"
                onClick={() => void (async () => {
                  try {
                    await RemoveCustomProvider(provider.id);
                    await refresh();
                    onChanged?.();
                  } catch (err) {
                    setError(err instanceof Error ? err.message : String(err));
                  }
                })()}
                className="rounded-md border border-[var(--color-line)] p-1.5 text-[var(--color-muted)] hover:text-[var(--color-danger)]"
                aria-label={`Remove ${provider.id}`}
              >
                <Trash2 size={12} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function MCPSettingsPane() {
  const [servers, setServers] = useState<MCPServerStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [subtab, setSubtab] = useState<"stdio" | "http">("stdio");
  const [showForm, setShowForm] = useState(false);
  const [editName, setEditName] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [command, setCommand] = useState("");
  const [argsText, setArgsText] = useState("");
  const [envText, setEnvText] = useState("");
  const [url, setUrl] = useState("");
  const [headersText, setHeadersText] = useState("");
  const [httpKind, setHttpKind] = useState<"http" | "sse">("http");

  const refresh = async () => {
    setLoading(true);
    setError(null);
    try {
      const list = await ListMCPServers();
      setServers(list ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void refresh();
  }, []);

  const isRemote = (server: MCPServerStatus) => {
    const kind = (server.kind || "").toLowerCase();
    return kind === "http" || kind === "sse" || !!server.url;
  };

  const visible = servers.filter((s) => (subtab === "http" ? isRemote(s) : !isRemote(s)));

  const resetForm = () => {
    setShowForm(false);
    setEditName(null);
    setName("");
    setCommand("");
    setArgsText("");
    setEnvText("");
    setUrl("");
    setHeadersText("");
    setHttpKind("http");
  };

  const openAdd = () => {
    resetForm();
    setShowForm(true);
  };

  const openEdit = (server: MCPServerStatus) => {
    setEditName(server.name);
    setName(server.name);
    if (isRemote(server)) {
      setSubtab("http");
      setUrl(server.url || "");
      setHttpKind(server.kind === "sse" ? "sse" : "http");
      setHeadersText(
        Object.entries(server.headers || {})
          .map(([k, v]) => `${k}=${v}`)
          .join("\n"),
      );
      setCommand("");
      setArgsText("");
      setEnvText("");
    } else {
      setSubtab("stdio");
      setCommand(server.command || "");
      setArgsText((server.args || []).join(" "));
      setEnvText(
        Object.entries(server.env || {})
          .map(([k, v]) => `${k}=${v}`)
          .join("\n"),
      );
      setUrl("");
      setHeadersText("");
    }
    setShowForm(true);
  };

  const parseArgs = (text: string) =>
    text
      .trim()
      .split(/\s+/)
      .filter(Boolean);

  const parseKV = (text: string) => {
    const out: Record<string, string> = {};
    for (const line of text.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith("#")) continue;
      const eq = trimmed.indexOf("=");
      if (eq <= 0) continue;
      out[trimmed.slice(0, eq).trim()] = trimmed.slice(eq + 1);
    }
    return out;
  };

  const saveServer = async () => {
    const serverName = name.trim();
    if (!serverName) {
      setError("Name is required");
      return;
    }
    if (subtab === "stdio") {
      const cmd = command.trim();
      if (!cmd) {
        setError("Command is required");
        return;
      }
    } else if (!url.trim()) {
      setError("URL is required");
      return;
    }

    setBusy("save");
    setError(null);
    try {
      if (editName && editName !== serverName) {
        await RemoveMCPServer(editName);
      }
      if (subtab === "stdio") {
        await UpsertMCPServer({
          name: serverName,
          kind: "stdio",
          command: command.trim(),
          args: parseArgs(argsText),
          env: parseKV(envText),
          disabled: false,
        });
      } else {
        await UpsertMCPServer({
          name: serverName,
          kind: httpKind,
          url: url.trim(),
          headers: parseKV(headersText),
          disabled: false,
        });
      }
      resetForm();
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  const subtabBtn = (id: "stdio" | "http", label: string) => (
    <button
      type="button"
      onClick={() => {
        setSubtab(id);
        resetForm();
      }}
      className={`rounded-md px-3 py-1.5 text-[11px] font-medium transition ${
        subtab === id
          ? "bg-[var(--color-panel-2)] text-[var(--color-text)]"
          : "text-[var(--color-muted)] hover:text-[var(--color-text)]"
      }`}
    >
      {label}
    </button>
  );

  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-6">
      <div className="mx-auto max-w-3xl">
        <div className="mb-4 flex items-start justify-between gap-4">
          <div className="flex items-start gap-3">
            <div className="rounded-lg bg-[var(--color-panel-2)] p-2 text-[var(--color-accent)]">
              <Server size={18} />
            </div>
            <div>
              <h3 className="text-sm font-semibold">MCP servers</h3>
              <p className="mt-1 text-xs leading-5 text-[var(--color-muted)]">
                Hook up Model Context Protocol servers. Tools are exposed as{" "}
                <span className="font-mono">mcp__name__tool</span>. Config lives in{" "}
                <span className="font-mono">~/.maiku/agent/mcp.json</span>.
              </p>
            </div>
          </div>
          <div className="flex shrink-0 gap-2">
            <button
              type="button"
              onClick={() => void (async () => {
                setBusy("reload");
                try {
                  await ReloadMCP();
                  await refresh();
                } catch (err) {
                  setError(err instanceof Error ? err.message : String(err));
                } finally {
                  setBusy(null);
                }
              })()}
              className="flex items-center gap-1.5 rounded-lg border border-[var(--color-line)] px-3 py-2 text-xs text-[var(--color-muted)] transition hover:bg-[var(--color-panel-2)] hover:text-[var(--color-text)]"
              title="Reload MCP servers"
            >
              <RefreshCw size={12} className={busy === "reload" ? "animate-spin" : ""} />
              Reload
            </button>
            <button
              type="button"
              onClick={openAdd}
              className="flex items-center gap-1.5 rounded-lg bg-[var(--color-accent)] px-3 py-2 text-xs font-medium text-[var(--color-ink)]"
            >
              <Plus size={12} />
              Add {subtab === "http" ? "HTTP" : "stdio"}
            </button>
          </div>
        </div>

        <div className="mb-5 inline-flex rounded-lg border border-[var(--color-line)] p-0.5">
          {subtabBtn("stdio", "stdio")}
          {subtabBtn("http", "HTTP")}
        </div>

        {error ? (
          <p role="alert" className="mb-4 text-[11px] text-[var(--color-danger)]">{error}</p>
        ) : null}

        {showForm ? (
          <div className="mb-6 rounded-xl border border-[var(--color-line)] bg-[var(--color-panel-2)] p-4">
            <h4 className="mb-3 text-xs font-semibold">
              {editName ? `Edit ${editName}` : subtab === "http" ? "New HTTP server" : "New stdio server"}
            </h4>
            <div className="grid gap-3">
              <label className="grid gap-1 text-[11px] text-[var(--color-muted)]">
                Name
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  disabled={!!editName}
                  placeholder={subtab === "http" ? "remote-api" : "filesystem"}
                  className="rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] px-3 py-2 font-mono text-xs text-[var(--color-text)] outline-none focus:border-[var(--color-accent-dim)]"
                />
              </label>
              {subtab === "stdio" ? (
                <>
                  <label className="grid gap-1 text-[11px] text-[var(--color-muted)]">
                    Command
                    <input
                      value={command}
                      onChange={(e) => setCommand(e.target.value)}
                      placeholder="npx"
                      className="rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] px-3 py-2 font-mono text-xs text-[var(--color-text)] outline-none focus:border-[var(--color-accent-dim)]"
                    />
                  </label>
                  <label className="grid gap-1 text-[11px] text-[var(--color-muted)]">
                    Args (space-separated)
                    <input
                      value={argsText}
                      onChange={(e) => setArgsText(e.target.value)}
                      placeholder="-y @modelcontextprotocol/server-filesystem /path"
                      className="rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] px-3 py-2 font-mono text-xs text-[var(--color-text)] outline-none focus:border-[var(--color-accent-dim)]"
                    />
                  </label>
                  <label className="grid gap-1 text-[11px] text-[var(--color-muted)]">
                    Env (KEY=value per line)
                    <textarea
                      value={envText}
                      onChange={(e) => setEnvText(e.target.value)}
                      rows={3}
                      placeholder={"API_KEY=${MY_KEY}"}
                      className="rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] px-3 py-2 font-mono text-xs text-[var(--color-text)] outline-none focus:border-[var(--color-accent-dim)]"
                    />
                  </label>
                </>
              ) : (
                <>
                  <label className="grid gap-1 text-[11px] text-[var(--color-muted)]">
                    Transport
                    <select
                      value={httpKind}
                      onChange={(e) => setHttpKind(e.target.value === "sse" ? "sse" : "http")}
                      className="rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] px-3 py-2 text-xs text-[var(--color-text)] outline-none focus:border-[var(--color-accent-dim)]"
                    >
                      <option value="http">Streamable HTTP</option>
                      <option value="sse">SSE (legacy)</option>
                    </select>
                  </label>
                  <label className="grid gap-1 text-[11px] text-[var(--color-muted)]">
                    URL
                    <input
                      value={url}
                      onChange={(e) => setUrl(e.target.value)}
                      placeholder="https://example.com/mcp"
                      className="rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] px-3 py-2 font-mono text-xs text-[var(--color-text)] outline-none focus:border-[var(--color-accent-dim)]"
                    />
                  </label>
                  <label className="grid gap-1 text-[11px] text-[var(--color-muted)]">
                    Headers (Header=value per line)
                    <textarea
                      value={headersText}
                      onChange={(e) => setHeadersText(e.target.value)}
                      rows={3}
                      placeholder={"Authorization=Bearer ${TOKEN}"}
                      className="rounded-lg border border-[var(--color-line)] bg-[var(--color-ink)] px-3 py-2 font-mono text-xs text-[var(--color-text)] outline-none focus:border-[var(--color-accent-dim)]"
                    />
                  </label>
                </>
              )}
            </div>
            <div className="mt-4 flex gap-2">
              <button
                type="button"
                disabled={busy === "save"}
                onClick={() => void saveServer()}
                className="rounded-lg bg-[var(--color-accent)] px-4 py-2 text-xs font-medium text-[var(--color-ink)] disabled:opacity-40"
              >
                {busy === "save" ? "Saving…" : "Save"}
              </button>
              <button
                type="button"
                onClick={resetForm}
                className="rounded-lg border border-[var(--color-line)] px-4 py-2 text-xs text-[var(--color-muted)] hover:bg-[var(--color-ink)]"
              >
                Cancel
              </button>
            </div>
          </div>
        ) : null}

        {loading ? (
          <p className="text-xs text-[var(--color-muted)]">Loading…</p>
        ) : visible.length === 0 ? (
          <p className="text-xs text-[var(--color-muted)]">
            No {subtab === "http" ? "HTTP" : "stdio"} MCP servers configured yet.
          </p>
        ) : (
          <div className="flex flex-col gap-3">
            {visible.map((server) => (
              <div key={server.name} className="rounded-xl border border-[var(--color-line)] bg-[var(--color-panel-2)] p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-xs font-medium">{server.name}</span>
                      <span
                        className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${
                          server.disabled
                            ? "bg-[var(--color-ink)] text-[var(--color-muted)]"
                            : server.connected
                              ? "bg-emerald-500/15 text-emerald-400"
                              : "bg-red-500/15 text-red-400"
                        }`}
                      >
                        {server.disabled ? "Disabled" : server.connected ? "Connected" : "Error"}
                      </span>
                      <span className="text-[10px] text-[var(--color-muted)]">
                        {server.kind || (isRemote(server) ? "http" : "stdio")} · {server.scope}
                      </span>
                    </div>
                    <p className="mt-1 truncate font-mono text-[10px] text-[var(--color-muted)]">
                      {isRemote(server)
                        ? server.url
                        : `${server.command || ""} ${(server.args || []).join(" ")}`.trim()}
                    </p>
                    {server.connected ? (
                      <p className="mt-1 text-[10px] text-[var(--color-muted)]">
                        {server.toolCount} tool{server.toolCount === 1 ? "" : "s"}
                        {server.tools?.length
                          ? `: ${server.tools.slice(0, 8).join(", ")}${server.tools.length > 8 ? "…" : ""}`
                          : ""}
                      </p>
                    ) : null}
                    {server.error ? (
                      <p className="mt-1 text-[11px] text-[var(--color-danger)]">{server.error}</p>
                    ) : null}
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <button
                      type="button"
                      disabled={busy === server.name || server.scope === "project"}
                      onClick={() => void (async () => {
                        setBusy(server.name);
                        try {
                          await SetMCPServerEnabled(server.name, server.disabled);
                          await refresh();
                        } catch (err) {
                          setError(err instanceof Error ? err.message : String(err));
                        } finally {
                          setBusy(null);
                        }
                      })()}
                      className="rounded-lg border border-[var(--color-line)] px-2.5 py-1.5 text-[10px] text-[var(--color-muted)] hover:bg-[var(--color-ink)] disabled:opacity-40"
                      title={server.scope === "project" ? "Edit project mcp.json on disk" : undefined}
                    >
                      {server.disabled ? "Enable" : "Disable"}
                    </button>
                    <button
                      type="button"
                      disabled={server.scope === "project"}
                      onClick={() => openEdit(server)}
                      className="rounded-lg border border-[var(--color-line)] px-2.5 py-1.5 text-[10px] text-[var(--color-muted)] hover:bg-[var(--color-ink)] disabled:opacity-40"
                    >
                      Edit
                    </button>
                    <button
                      type="button"
                      disabled={busy === server.name || server.scope === "project"}
                      onClick={() => void (async () => {
                        if (!window.confirm(`Remove MCP server “${server.name}”?`)) return;
                        setBusy(server.name);
                        try {
                          await RemoveMCPServer(server.name);
                          await refresh();
                        } catch (err) {
                          setError(err instanceof Error ? err.message : String(err));
                        } finally {
                          setBusy(null);
                        }
                      })()}
                      className="rounded-lg border border-[var(--color-line)] p-1.5 text-[var(--color-muted)] hover:bg-[var(--color-ink)] hover:text-[var(--color-danger)] disabled:opacity-40"
                      aria-label={`Remove ${server.name}`}
                    >
                      <Trash2 size={12} />
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
