import { useEffect, useRef, type RefObject } from "react";

/**
 * Closes a popover when the user clicks literally anywhere outside `ref`.
 *
 * Listens for `pointerdown` in the capture phase on `window`, so it fires
 * before the panel's own handlers and before the `click` that follows —
 * clicking another trigger therefore switches dropdowns in a single click.
 * Wails' CSS drag regions use deferred dragging (no preventDefault on
 * mousedown), so clicks on the titlebar background reach the DOM too and
 * close the popover as well.
 */
export function useClickAway(
  open: boolean,
  ref: RefObject<HTMLElement | null>,
  onClose: () => void,
) {
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  useEffect(() => {
    if (!open) return;
    const onDown = (e: PointerEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onCloseRef.current();
      }
    };
    window.addEventListener("pointerdown", onDown, true);
    return () => window.removeEventListener("pointerdown", onDown, true);
  }, [open, ref]);
}
