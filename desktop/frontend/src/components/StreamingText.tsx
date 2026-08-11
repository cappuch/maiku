export function StreamingText({ content }: { content: string }) {
  // Stable indexes mean only newly-arrived words animate; previous text stays put.
  const tokens = content.split(/(\s+)/).filter(Boolean);

  return (
    <p className="streaming-text whitespace-pre-wrap">
      {tokens.map((token, index) => (
        <span key={`${index}-${token}`} className="stream-token">
          {token}
        </span>
      ))}
      <span className="stream-caret" aria-hidden />
    </p>
  );
}
