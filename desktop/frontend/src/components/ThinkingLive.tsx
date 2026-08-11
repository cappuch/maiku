import { useEffect, useMemo, useState } from "react";
import { ChevronDown, Sparkles } from "lucide-react";

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
  const [expanded, setExpanded] = useState(live);

  useEffect(() => {
    if (!live) return;
    const id = window.setInterval(() => {
      setNow(Date.now());
      setFrame((f) => (f + 1) % BRAILLE.length);
    }, 80);
    return () => window.clearInterval(id);
  }, [live]);

  const elapsedSec = Math.max(0, Math.floor((now - (startedAt ?? now)) / 1000));
  // Live mode shows a rolling preview of recent lines; persisted mode keeps
  // the full reasoning so nothing is lost after the response lands.
  const lines = useMemo(() => {
    const all = thinking
      .split(/\r?\n/)
      .map((l) => l.trimEnd())
      .filter((l) => l.trim() !== "");
    return live ? all.slice(-4) : all;
  }, [thinking, live]);

  return (
    <div className="thinking-card">
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((value) => !value)}
        className="thinking-header"
      >
        <span className="thinking-spark"><Sparkles size={13} fill="currentColor" /></span>
        <span className="thinking-title">Thinking</span>
        {live && (
          <span className="thinking-time"><span aria-hidden>{BRAILLE[frame]}</span> {elapsedSec}s</span>
        )}
        <ChevronDown size={14} className={expanded ? "thinking-chevron open" : "thinking-chevron"} />
      </button>
      {expanded && (
        <div className="thinking-trace">
          {lines.length > 0 ? lines.map((line, i) => (
            <div key={`${i}-${line.slice(0, 24)}`} className="thinking-line">
              <span>{line}</span>
            </div>
          )) : (
            <div className="thinking-line"><span>Gathering context…</span></div>
          )}
        </div>
      )}
    </div>
  );
}
