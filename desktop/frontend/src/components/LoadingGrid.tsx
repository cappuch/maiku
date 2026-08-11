import { useEffect, useState } from "react";

const delays = Array.from({ length: 9 }, (_, i) => {
  const row = Math.floor(i / 3);
  const col = i % 3;
  return (col + Math.abs(row - 1)) * 90;
});

export function LoadingGrid({ label = "Working" }: { label?: string }) {
  const [tenths, setTenths] = useState(0);
  useEffect(() => {
    const timer = window.setInterval(() => setTenths((value) => value + 1), 100);
    return () => window.clearInterval(timer);
  }, []);

  return (
    <div className="live-loader" aria-live="polite">
      <span aria-hidden className="pixel-grid">
        {delays.map((delay, index) => (
          <span key={index} style={{ animationDelay: `${delay}ms` }} />
        ))}
      </span>
      <span className="live-loader-label">{label}</span>
      <span className="live-loader-time">{(tenths / 10).toFixed(1)}s</span>
    </div>
  );
}
