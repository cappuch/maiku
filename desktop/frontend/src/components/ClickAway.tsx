import type { MouseEvent } from "react";

/**
 * Invisible full-window layer that closes a popover/selector when the user
 * clicks literally anywhere outside it. Marked no-drag so it also intercepts
 * clicks over the Wails titlebar drag region (where DOM mouse events are
 * otherwise swallowed for window dragging).
 */
export function ClickAway({
  onClose,
  onContextMenu,
}: {
  onClose: () => void;
  onContextMenu?: (e: MouseEvent) => void;
}) {
  return (
    <button
      type="button"
      aria-label="Close popover"
      tabIndex={-1}
      data-wails-no-drag
      className="fixed inset-0 z-40"
      onClick={onClose}
      onContextMenu={onContextMenu}
    />
  );
}
