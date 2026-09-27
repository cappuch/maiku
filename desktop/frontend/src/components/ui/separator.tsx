import { Separator as SeparatorPrimitive } from "@radix-ui/react-separator";
import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

export function Separator({
  className,
  orientation = "horizontal",
  decorative = true,
  ...props
}: ComponentProps<typeof SeparatorPrimitive>) {
  return (
    <SeparatorPrimitive
      decorative={decorative}
      orientation={orientation}
      className={cn(
        "shrink-0 bg-white/10",
        orientation === "horizontal" ? "h-px w-full" : "h-full w-px",
        className,
      )}
      {...props}
    />
  );
}
