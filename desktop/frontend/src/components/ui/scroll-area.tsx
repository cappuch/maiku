import { ScrollArea as ScrollAreaPrimitive, Viewport, Scrollbar, Thumb, Corner } from "@radix-ui/react-scroll-area";
import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

export function ScrollArea({ className, children, ...props }: ComponentProps<typeof ScrollAreaPrimitive>) {
  return (
    <ScrollAreaPrimitive className={cn("relative overflow-hidden", className)} {...props}>
      <Viewport className="h-full w-full rounded-[inherit]">{children}</Viewport>
      <Scrollbar
        orientation="vertical"
        className="flex w-2 touch-none p-0.5 select-none"
      >
        <Thumb className="relative flex-1 rounded-full bg-white/15" />
      </Scrollbar>
      <Corner />
    </ScrollAreaPrimitive>
  );
}
