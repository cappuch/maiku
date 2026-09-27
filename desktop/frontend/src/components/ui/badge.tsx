import { cva, type VariantProps } from "class-variance-authority";
import type { HTMLAttributes } from "react";
import { cn } from "../../lib/utils";

const badgeVariants = cva(
  "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-medium tracking-wide",
  {
    variants: {
      variant: {
        default: "border-white/10 bg-white/[0.06] text-zinc-200",
        quiet: "border-transparent bg-white/[0.04] text-zinc-500",
        danger: "border-transparent bg-[color-mix(in_srgb,var(--color-danger)_14%,transparent)] text-[var(--color-danger)]",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

export function Badge({
  className,
  variant,
  ...props
}: HTMLAttributes<HTMLSpanElement> & VariantProps<typeof badgeVariants>) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />;
}
