import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Check, ChevronDown, Sparkles } from "lucide-react";
import { Markdown } from "./Markdown";

const BRAILLE = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

export function ThinkingLive({
  thinking,
  startedAt,
  live = true,
}: {
  thinking: string;
  startedAt?: number | null;
  live?: boolean;
}) {
  const [now, setNow] = useState(() => Date.now());
  const [frame, setFrame] = useState(0);
  const [manualExpanded, setManualExpanded] = useState<boolean | null>(null);
  const traceRef = useRef<HTMLDivElement>(null);
  const [traceHeight, setTraceHeight] = useState(0);

  useEffect(() => {
    if (!live) return;
    const id = window.setInterval(() => {
      setNow(Date.now());
      setFrame((current) => (current + 1) % BRAILLE.length);
    }, 100);
    return () => window.clearInterval(id);
  }, [live]);

  const elapsedSec = Math.max(1, Math.floor((now - (startedAt ?? now)) / 1000));
  const expanded = manualExpanded ?? live;

  useLayoutEffect(() => {
    const trace = traceRef.current;
    if (!trace) return;

    const updateHeight = () => setTraceHeight(trace.offsetHeight);
    updateHeight();

    const observer = new ResizeObserver(updateHeight);
    observer.observe(trace);
    return () => observer.disconnect();
  }, []);

  return (
    <section className="thinking-trace-card">
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setManualExpanded((current) => !(current ?? live))}
        className="thinking-header"
      >
        <Sparkles size={15} fill={live ? "currentColor" : "none"} className="thinking-spark" />
        {live ? (
          <span className="thinking-title thinking-active">Thinking</span>
        ) : (
          <span className="thinking-title">Thought through the task</span>
        )}
        {live && (
          <span className="thinking-time">
            <span aria-hidden>{BRAILLE[frame]}</span> {elapsedSec}s
          </span>
        )}
        {!live && <Check size={13} className="thinking-done" />}
        <ChevronDown size={14} className={expanded ? "thinking-chevron open" : "thinking-chevron"} />
      </button>

      {expanded && (
        <div className="thinking-expand expanded">
          <div className="thinking-expand-inner">
            <div className="thinking-rail" style={{ height: traceHeight ? `${traceHeight - 6}px` : 0 }} />
            <div ref={traceRef} className="thinking-lines">
              {thinking.trim() ? (
                <div className="thinking-markdown">
                  <Markdown content={thinking} />
                </div>
              ) : (
                <p className="thinking-line">Gathering context and mapping the task…</p>
              )}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
