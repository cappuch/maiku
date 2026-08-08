import { useMemo, useState } from "react";
import { ChevronDown } from "lucide-react";
import type { ModelInfo } from "../types";
import { cn } from "../lib/utils";

const THINKING = ["off", "minimal", "low", "medium", "high", "xhigh", "max"];

export function ModelSelector({
  models,
  provider,
  modelId,
  thinking,
  onSetModel,
  onSetThinking,
}: {
  models: ModelInfo[];
  provider: string;
  modelId: string;
  thinking: string;
  onSetModel: (provider: string, id: string) => void;
  onSetThinking: (level: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");

  const filtered = useMemo(() => {
    const lower = q.toLowerCase();
    return models.filter(
      (m) =>
        !lower ||
        m.id.toLowerCase().includes(lower) ||
        m.name.toLowerCase().includes(lower) ||
        m.provider.toLowerCase().includes(lower),
    );
  }, [models, q]);

  const label = modelId ? `${provider}/${modelId}` : "Select model";

  return (
    <div className="relative flex items-center gap-2">
      <select
        value={thinking}
        onChange={(e) => onSetThinking(e.target.value)}
        className="rounded-md border border-[var(--color-line)] bg-[var(--color-panel-2)] px-2 py-1 text-xs text-[var(--color-muted)] outline-none"
        title="Thinking level"
      >
        {THINKING.map((t) => (
          <option key={t} value={t}>
            think:{t}
          </option>
        ))}
      </select>

      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex max-w-[280px] items-center gap-1 rounded-md border border-[var(--color-line)] bg-[var(--color-panel-2)] px-2 py-1 text-xs hover:border-[var(--color-accent-dim)]"
      >
        <span className="truncate font-mono">{label}</span>
        <ChevronDown size={12} className="shrink-0 text-[var(--color-muted)]" />
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div className="absolute right-0 top-full z-50 mt-1 w-80 overflow-hidden rounded-lg border border-[var(--color-line)] bg-[var(--color-panel)] shadow-xl">
            <input
              autoFocus
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Search models…"
              className="w-full border-b border-[var(--color-line)] bg-transparent px-3 py-2 text-xs outline-none"
            />
            <div className="max-h-72 overflow-y-auto">
              {filtered.map((m) => {
                const active = m.provider === provider && m.id === modelId;
                return (
                  <button
                    key={`${m.provider}/${m.id}`}
                    type="button"
                    onClick={() => {
                      onSetModel(m.provider, m.id);
                      setOpen(false);
                      setQ("");
                    }}
                    className={cn(
                      "flex w-full flex-col px-3 py-2 text-left text-xs hover:bg-[var(--color-panel-2)]",
                      active && "bg-[var(--color-panel-2)]",
                    )}
                  >
                    <span className="font-mono text-[var(--color-text)]">
                      {m.provider}/{m.id}
                    </span>
                    <span className="text-[10px] text-[var(--color-muted)]">
                      {m.name}
                      {m.reasoning ? " · reasoning" : ""}
                      {m.vision ? " · vision" : ""}
                      {m.hasKey ? "" : " · no key"}
                    </span>
                  </button>
                );
              })}
              {filtered.length === 0 && (
                <p className="px-3 py-4 text-xs text-[var(--color-muted)]">No matches</p>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
