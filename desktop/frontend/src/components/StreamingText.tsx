export function StreamingText({ content }: { content: string }) {
  // Character offsets stay stable as content is appended, so only new tokens animate.
  let offset = 0;
  const tokens = content
    .split(/(\s+)/)
    .filter(Boolean)
    .map((text) => {
      const token = { offset, text };
      offset += text.length;
      return token;
    });

  return (
    <p className="streaming-text whitespace-pre-wrap">
      {tokens.map((token) => (
        <span key={token.offset} className="stream-token">
          {token.text}
        </span>
      ))}
      <span className="stream-caret" aria-hidden />
    </p>
  );
}
