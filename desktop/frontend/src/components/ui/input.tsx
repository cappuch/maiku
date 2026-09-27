import type { InputHTMLAttributes } from "react";
import { cn } from "../../lib/utils";

export function Input({ className, type = "text", ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      type={type}
      className={cn(
        "flex h-8 w-full rounded-lg border border-white/10 bg-black/30 px-3 text-xs text-zinc-100 outline-none transition placeholder:text-zinc-500 focus:border-white/30",
        className,
      )}
      {...props}
    />
  );
}
