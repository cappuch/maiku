import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { Check, ChevronDown } from "lucide-react";
import type { ModelInfo } from "../types";
import { cn } from "../lib/utils";
import { useClickAway } from "./useClickAway";

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
  const [modelOpen, setModelOpen] = useState(false);
  const [thinkOpen, setThinkOpen] = useState(false);
  const [q, setQ] = useState("");
  const thinkRootRef = useRef<HTMLDivElement>(null);
  const modelRootRef = useRef<HTMLDivElement>(null);
  const reduceMotion = useReducedMotion();
  const menuMotion = reduceMotion
    ? { initial: false as const, animate: { opacity: 1, y: 0 }, exit: { opacity: 1 } }
    : {
        initial: { opacity: 0, y: -6, scale: 0.98 },
        animate: { opacity: 1, y: 0, scale: 1 },
        exit: { opacity: 0, y: -4, scale: 0.98 },
        transition: { duration: 0.16, ease: [0.23, 1, 0.32, 1] as const },
      };

  const closeAll = useCallback(() => {
    setModelOpen(false);
    setThinkOpen(false);
    setQ("");
  }, []);

  // Clicking anywhere outside a dropdown closes it.
  useClickAway(thinkOpen, thinkRootRef, () => setThinkOpen(false));
  useClickAway(modelOpen, modelRootRef, closeAll);

  // Close dropdowns on Escape.
  useEffect(() => {
    if (!modelOpen && !thinkOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      closeAll();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [modelOpen, thinkOpen, closeAll]);

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
      {/* Thinking level */}
      <div ref={thinkRootRef} className="relative flex items-center">
        <button
          type="button"
          data-wails-no-drag
          onClick={() => {
            setModelOpen(false);
            setThinkOpen((v) => !v);
          }}
          className={cn(
            "flex items-center gap-1 rounded-full bg-white/[0.04] px-2.5 py-1 text-xs text-zinc-400 hover:bg-white/[0.08] hover:text-zinc-100",
            thinkOpen && "bg-white/[0.08] text-zinc-100",
          )}
          title="Thinking level"
          aria-expanded={thinkOpen}
        >
          <span className="font-mono">think:{thinking}</span>
          <ChevronDown
            size={12}
            className={cn(
              "shrink-0 text-[var(--color-muted)] transition-transform",
              thinkOpen && "rotate-180",
            )}
          />
        </button>

        <AnimatePresence>
        {thinkOpen && (
          <motion.div
            {...menuMotion}
            data-wails-no-drag
            className="absolute right-0 top-full z-50 mt-1.5 w-44 overflow-hidden rounded-xl border border-white/10 bg-[#141416] py-1 shadow-[0_24px_70px_rgba(0,0,0,0.45)]"
          >
            {THINKING.map((t) => {
              const active = t === thinking;
              return (
                <button
                  key={t}
                  type="button"
                  aria-pressed={active}
                  onClick={() => {
                    onSetThinking(t);
                    setThinkOpen(false);
                  }}
                  className={cn(
                    "flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-xs transition-colors hover:bg-white/[0.06]",
                    active && "bg-white/[0.06]",
                  )}
                >
                  <span
                    className={cn(
                      "font-mono",
                      active ? "text-[var(--color-text)]" : "text-[var(--color-muted)]",
                    )}
                  >
                    think:{t}
                  </span>
                  {active && <Check size={13} className="shrink-0 text-zinc-200" />}
                </button>
              );
            })}
          </motion.div>
        )}
        </AnimatePresence>
      </div>

      {/* Model picker */}
      <div ref={modelRootRef} className="relative flex items-center">
        <button
          type="button"
          data-wails-no-drag
          onClick={() => {
            setThinkOpen(false);
            setModelOpen((v) => !v);
          }}
          className={cn(
            "flex max-w-[280px] items-center gap-1.5 rounded-full bg-white/[0.04] px-2.5 py-1 text-xs text-zinc-300 hover:bg-white/[0.08] hover:text-zinc-50",
            modelOpen && "bg-white/[0.08] text-zinc-50",
          )}
          aria-haspopup="dialog"
          aria-expanded={modelOpen}
        >
          <span className="truncate font-mono">{label}</span>
          <ChevronDown
            size={12}
            className={cn(
              "shrink-0 text-[var(--color-muted)] transition-transform",
              modelOpen && "rotate-180",
            )}
          />
        </button>

        <AnimatePresence>
        {modelOpen && (
          <motion.div
            {...menuMotion}
            data-wails-no-drag
            role="dialog"
            aria-label="Choose a model"
            className="absolute right-0 top-full z-50 mt-1.5 w-80 overflow-hidden rounded-xl border border-white/10 bg-[#141416] shadow-[0_24px_70px_rgba(0,0,0,0.45)]"
          >
            <input
              ref={(input) => input?.focus()}
              value={q}
              onChange={(e) => setQ(e.target.value)}
              aria-label="Search models"
              placeholder="Search models…"
              className="w-full border-b border-white/10 bg-transparent px-3 py-2.5 text-xs outline-none placeholder:text-zinc-500"
            />
            <div className="max-h-72 overflow-y-auto">
              {filtered.map((m) => {
                const active = m.provider === provider && m.id === modelId;
                return (
                  <button
                    key={`${m.provider}/${m.id}`}
                    type="button"
                    aria-pressed={active}
                    onClick={() => {
                      onSetModel(m.provider, m.id);
                      closeAll();
                    }}
                    className={cn(
                      "flex w-full flex-col px-3 py-2 text-left text-xs hover:bg-white/[0.06]",
                      active && "bg-white/[0.06]",
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
          </motion.div>
        )}
        </AnimatePresence>
      </div>
    </div>
  );
}
