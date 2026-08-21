import { useEffect, useState } from "react";

const cells = Array.from({ length: 9 }, (_, index) => {
  const row = Math.floor(index / 3);
  const column = index % 3;
  return {
    id: `${row}-${column}`,
    delay: (column + Math.abs(row - 1)) * 90,
  };
});

export function LoadingGrid({ label = "Working" }: { label?: string }) {
  const [tenths, setTenths] = useState(0);
  useEffect(() => {
    const timer = window.setInterval(() => setTenths((value) => value + 1), 100);
    return () => window.clearInterval(timer);
  }, []);

  return (
    <div className="live-loader" role="status" aria-label={label}>
      <span aria-hidden className="pixel-grid">
        {cells.map((cell) => (
          <span key={cell.id} style={{ animationDelay: `${cell.delay}ms` }} />
        ))}
      </span>
      <span className="live-loader-label">{label}</span>
      <span className="live-loader-time" aria-hidden>{(tenths / 10).toFixed(1)}s</span>
    </div>
  );
}
