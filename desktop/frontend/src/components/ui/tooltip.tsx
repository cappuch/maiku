import { Tooltip as TooltipPrimitive, Provider, Root, Trigger, Content, Portal } from "@radix-ui/react-tooltip";
import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

export function TooltipProvider({ delayDuration = 400, ...props }: ComponentProps<typeof Provider>) {
  return <Provider delayDuration={delayDuration} {...props} />;
}

export const Tooltip = Root;
export const TooltipTrigger = Trigger;

export function TooltipContent({
  className,
  sideOffset = 6,
  ...props
}: ComponentProps<typeof Content>) {
  return (
    <Portal>
      <Content
        sideOffset={sideOffset}
        className={cn(
          "z-50 overflow-hidden rounded-md border border-white/10 bg-zinc-950 px-2 py-1 text-[11px] text-zinc-200 shadow-xl",
          className,
        )}
        {...props}
      />
    </Portal>
  );
}

export { TooltipPrimitive };
