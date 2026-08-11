import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatCost(n: number): string {
  if (!n || Number.isNaN(n)) return "$0.00";
  if (n < 0.01) return `$${n.toFixed(4)}`;
  return `$${n.toFixed(2)}`;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n || 0);
}

export function formatCacheRate(rate: number): string {
  if (!rate || Number.isNaN(rate)) return "0%";
  return `${Math.round(rate * 100)}%`;
}

/*
 * Personalized empty-state greeting. The variant pool is chosen by time of
 * day, then a random line is picked and [Name] is substituted with the OS
 * user's login name (e.g. "mikus" for /Users/mikus).
 */
const GREETING_POOLS: Record<string, string[]> = {
  late: [
    "Burning the midnight oil, [Name]?",
    "Quiet hours for coding, [Name]?",
    "Night shift, [Name]?",
    "Still at it?",
    "What are we building tonight?",
  ],
  casual: [
    "Hey [Name]",
    "What's on your mind, [Name]?",
    "Where should we start?",
    "What are we working on?",
    "How can I help tonight, [Name]?",
  ],
  task: [
    "All set when you are.",
    "What's top of mind?",
    "Ready for the next step.",
    "What are we tackling today?",
    "Lead the way, [Name].",
  ],
  minimal: ["[Name].", "Here.", "Standing by.", "What's next?"],
};

export function greetingFor(name: string, now: Date = new Date()): string {
  const h = now.getHours();
  // 22:00–04:59 late-night focus · 05:00–11:59 casual morning
  // 12:00–16:59 task-oriented afternoon · 17:00–21:59 quiet evening
  let pool: string[];
  if (h >= 22 || h < 5) pool = GREETING_POOLS.late;
  else if (h < 12) pool = GREETING_POOLS.casual;
  else if (h < 17) pool = GREETING_POOLS.task;
  else pool = GREETING_POOLS.minimal;
  const text = pool[Math.floor(Math.random() * pool.length)];
  return text.replace(/\[Name\]/g, name || "there");
}
