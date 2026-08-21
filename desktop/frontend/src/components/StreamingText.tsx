import { useEffect, useRef, useState } from "react";
import { Markdown } from "./Markdown";

const STREAM_RENDER_INTERVAL = 40;

/**
 * The streaming and completed answer use the same Markdown surface, avoiding
 * the distracting raw-text-to-formatted layout jump at the end of a response.
 * Updates are capped at 25fps so fast token streams stay smooth on long turns.
 */
export function StreamingText({ content }: { content: string }) {
  const [displayContent, setDisplayContent] = useState(content);
  const pendingContent = useRef(content);
  const timer = useRef<number | null>(null);

  useEffect(() => {
    pendingContent.current = content;
    if (timer.current !== null) return;
    timer.current = window.setTimeout(() => {
      timer.current = null;
      setDisplayContent(pendingContent.current);
    }, STREAM_RENDER_INTERVAL);
  }, [content]);

  useEffect(() => () => {
    if (timer.current !== null) window.clearTimeout(timer.current);
  }, []);

  return <Markdown content={displayContent} streaming />;
}
