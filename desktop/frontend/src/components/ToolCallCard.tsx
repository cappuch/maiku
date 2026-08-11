import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { ChevronDown, FilePenLine, FilePlus2, FileText, Terminal } from "lucide-react";
import type { UIMessage } from "../types";
import { cn } from "../lib/utils";

export function ToolCallCard({ message }: { message: UIMessage }) {
  const name = (message.toolName || "tool").toLowerCase();
  const path = toolPath(message.args);
  const fileLabel = path || "…";

  if (name === "read") {
    return <ReadCard message={message} path={fileLabel} />;
  }
  if (name === "write") {
    return <WriteCard message={message} path={fileLabel} />;
  }
  if (name === "edit") {
    return <EditCard message={message} path={fileLabel} />;
  }
  return <GenericToolCard message={message} />;
}

function ReadCard({ message, path }: { message: UIMessage; path: string }) {
  const [open, setOpen] = useState(false);
  const hasBody = !!(message.text && message.text !== "running…");

  return (
    <ToolShell
      icon={<FileText size={13} className="text-[var(--color-accent)]" />}
      label="Read"
      chip={path}
      streaming={message.streaming}
      isError={message.isError}
      open={open}
      onToggle={hasBody ? () => setOpen((v) => !v) : undefined}
    >
      {hasBody && open ? (
        <pre className="max-h-48 overflow-auto whitespace-pre-wrap font-mono text-[11px] text-[var(--color-muted)]">
          {message.text}
        </pre>
      ) : null}
    </ToolShell>
  );
}

function WriteCard({ message, path }: { message: UIMessage; path: string }) {
  const content = toolStringArg(message.args, "content");
  const lines = useMemo(() => splitLines(content), [content]);
  const [open, setOpen] = useState(!!message.streaming);
  const preRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (message.streaming) setOpen(true);
  }, [message.streaming]);

  useEffect(() => {
    if (open && message.streaming && preRef.current) {
      preRef.current.scrollTop = preRef.current.scrollHeight;
    }
  }, [content, open, message.streaming]);

  return (
    <ToolShell
      icon={<FilePlus2 size={13} className="text-[var(--color-accent)]" />}
      label={`Write${lines.length ? ` ${lines.length} lines` : ""}`}
      chip={path}
      streaming={message.streaming}
      isError={message.isError}
      open={open}
      onToggle={() => setOpen((v) => !v)}
    >
      {open && (
        <div className="overflow-hidden rounded-md border border-[var(--color-line)] bg-[#0d0d0f]">
          <pre
            ref={preRef}
            className="max-h-56 overflow-auto p-0 font-mono text-[11px] leading-[1.45]"
          >
            {lines.length === 0 ? (
              <div className="px-3 py-2 text-[var(--color-muted)]">
                {message.streaming ? "generating…" : "(empty)"}
              </div>
            ) : (
              lines.map((line, i) => (
                <div key={i} className="flex">
                  <span className="sticky left-0 w-10 shrink-0 select-none bg-[#0d0d0f] px-2 text-right text-[var(--color-muted)]/60">
                    {i + 1}
                  </span>
                  <span className="flex-1 whitespace-pre-wrap break-all pr-3 text-[var(--color-text)]">
                    {line || " "}
                  </span>
                </div>
              ))
            )}
          </pre>
        </div>
      )}
    </ToolShell>
  );
}

function EditCard({ message, path }: { message: UIMessage; path: string }) {
  const sides = useMemo(() => {
    const fromDetails = detailsDiff(message.details);
    if (fromDetails) return unifiedToSideBySide(parseDiffLines(fromDetails));
    return editsToSideBySide(message.args);
  }, [message.details, message.args]);

  const [open, setOpen] = useState(!!message.streaming || sides.length > 0);
  // A removal can still contain unchanged context on the right side after the
  // diff is paired. If there are no additions at all, a two-column comparison
  // adds no information: show the removed content in red as one change block.
  const removalOnly =
    sides.length > 0 && !sides.some((row) => row.right?.kind === "add");

  useEffect(() => {
    if (message.streaming || sides.length > 0) setOpen(true);
  }, [message.streaming, sides.length]);

  return (
    <ToolShell
      icon={<FilePenLine size={13} className="text-[var(--color-accent)]" />}
      label="Edit"
      chip={path}
      streaming={message.streaming}
      isError={message.isError}
      open={open}
      onToggle={() => setOpen((v) => !v)}
    >
      {open && (
        <div className="overflow-hidden rounded-md border border-[var(--color-line)] bg-[#0d0d0f]">
          {sides.length === 0 ? (
            <div className="px-3 py-2 font-mono text-[11px] text-[var(--color-muted)]">
              {message.streaming ? "preparing diff…" : message.text || "no changes"}
            </div>
          ) : removalOnly ? (
            <div className="max-h-72 overflow-auto font-mono text-[11px] leading-[1.45]">
              {sides.map((row, i) => (
                <div
                  key={i}
                  className={cn(
                    "flex min-h-[1.45em]",
                    row.left?.kind === "del"
                      ? "bg-[color-mix(in_srgb,var(--color-danger)_14%,transparent)] text-[var(--color-danger)]"
                      : "text-[var(--color-muted)]",
                  )}
                >
                  <span className="w-8 shrink-0 select-none px-1 text-right opacity-50">
                    {row.left?.lineNum ?? ""}
                  </span>
                  <span className="flex-1 whitespace-pre-wrap break-all pr-2">{row.left?.text ?? " "}</span>
                </div>
              ))}
            </div>
          ) : (
            <div className="max-h-72 overflow-auto">
              <div className="sticky top-0 z-[1] grid grid-cols-2 border-b border-[var(--color-line)] bg-[#121214] font-mono text-[10px] tracking-wide text-[var(--color-muted)]">
                <div className="border-r border-[var(--color-line)] px-2 py-1">before</div>
                <div className="px-2 py-1">after</div>
              </div>
              {sides.map((row, i) => (
                <div key={i} className="grid grid-cols-2 font-mono text-[11px] leading-[1.45]">
                  <SideCell side="left" cell={row.left} />
                  <SideCell side="right" cell={row.right} />
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </ToolShell>
  );
}

function SideCell({
  side,
  cell,
}: {
  side: "left" | "right";
  cell?: SideCellData;
}) {
  const kind = cell?.kind ?? "empty";
  return (
    <div
      className={cn(
        "flex min-h-[1.45em] border-[var(--color-line)]",
        side === "left" ? "border-r" : "",
        kind === "del" && "bg-[color-mix(in_srgb,var(--color-danger)_14%,transparent)] text-[var(--color-danger)]",
        kind === "add" && "bg-[color-mix(in_srgb,var(--color-ok)_14%,transparent)] text-[var(--color-ok)]",
        kind === "ctx" && "text-[var(--color-muted)]",
        kind === "empty" && "bg-transparent",
      )}
    >
      <span className="w-8 shrink-0 select-none px-1 text-right opacity-50">
        {cell?.lineNum ?? ""}
      </span>
      <span className="flex-1 whitespace-pre-wrap break-all pr-2">{cell?.text ?? " "}</span>
    </div>
  );
}

function GenericToolCard({ message }: { message: UIMessage }) {
  const [open, setOpen] = useState(false);
  const args =
    typeof message.args === "string"
      ? message.args
      : JSON.stringify(message.args ?? {}, null, 2);

  return (
    <ToolShell
      icon={<Terminal size={13} className="text-[var(--color-accent)]" />}
      label={message.toolName || "Tool"}
      chip={toolSummary(message.args)}
      streaming={message.streaming}
      isError={message.isError}
      open={open}
      onToggle={() => setOpen((v) => !v)}
    >
      {open && (
        <div className="space-y-2 font-mono text-[11px] text-[var(--color-muted)]">
          <div>
            <div className="mb-1 text-[10px] tracking-wide">args</div>
            <pre className="overflow-x-auto whitespace-pre-wrap text-[var(--color-text)]">{args}</pre>
          </div>
          {message.text && (
            <div>
              <div className="mb-1 text-[10px] tracking-wide">result</div>
              <pre className="max-h-64 overflow-auto whitespace-pre-wrap text-[var(--color-text)]">
                {message.text}
              </pre>
            </div>
          )}
        </div>
      )}
    </ToolShell>
  );
}

function ToolShell({
  icon,
  label,
  chip,
  streaming,
  isError,
  open,
  onToggle,
  children,
}: {
  icon: ReactNode;
  label: ReactNode;
  chip: ReactNode;
  streaming?: boolean;
  isError?: boolean;
  open: boolean;
  onToggle?: () => void;
  children?: React.ReactNode;
}) {
  const clickable = !!onToggle;
  return (
    <div className={cn("tool-shell", isError && "tool-shell-error")}>
      <button
        type="button"
        onClick={onToggle}
        disabled={!clickable}
        className={cn(
          "tool-row group flex w-full min-w-0 items-center gap-2 rounded-lg px-1.5 py-1 text-left text-xs",
          !clickable && "cursor-default",
        )}
      >
        {icon}
        <span className="shrink-0 font-medium text-[var(--color-text)]">{label}</span>
        <span className="tool-chip min-w-0 flex-1 truncate">{chip}</span>
        {streaming && <span className="tool-working" aria-label="Running">…</span>}
        {isError && <span className="text-[var(--color-danger)]">error</span>}
        {clickable && (
          <ChevronDown
            size={14}
            className={cn("tool-chevron text-[var(--color-muted)] transition-transform", open && "rotate-180")}
          />
        )}
      </button>
      {open && children ? (
        <div className="tool-detail">{children}</div>
      ) : null}
    </div>
  );
}

function asArgsObject(args: unknown): Record<string, unknown> | null {
  if (!args) return null;
  if (typeof args === "string") {
    try {
      const parsed = JSON.parse(args);
      return parsed && typeof parsed === "object" ? (parsed as Record<string, unknown>) : null;
    } catch {
      return null;
    }
  }
  if (typeof args === "object") return args as Record<string, unknown>;
  return null;
}

function toolPath(args: unknown): string {
  const a = asArgsObject(args);
  if (!a) return "";
  const p = a.path ?? a.file_path;
  return typeof p === "string" ? p : "";
}

function toolStringArg(args: unknown, key: string): string {
  const a = asArgsObject(args);
  if (!a) return "";
  const v = a[key];
  return typeof v === "string" ? v : "";
}

function toolSummary(args: unknown): string {
  const object = asArgsObject(args);
  if (!object) return "Details";
  const command = object.command ?? object.cmd;
  if (typeof command === "string") return command;
  const path = toolPath(args);
  if (path) return path;
  const text = object.query ?? object.pattern;
  return typeof text === "string" ? text : "Details";
}

function detailsDiff(details: unknown): string {
  if (!details || typeof details !== "object") return "";
  const d = details as Record<string, unknown>;
  return typeof d.diff === "string" ? d.diff : typeof d.Diff === "string" ? d.Diff : "";
}

function splitLines(content: string): string[] {
  if (!content) return [];
  const lines = content.split("\n");
  // Drop a single trailing empty line from a terminating newline.
  if (lines.length > 1 && lines[lines.length - 1] === "") lines.pop();
  return lines;
}

type DiffRow = {
  kind: "add" | "del" | "ctx" | "meta";
  lineNum?: string;
  text: string;
};

type SideCellData = {
  kind: "add" | "del" | "ctx";
  lineNum?: string;
  text: string;
};

type SideBySideRow = {
  left?: SideCellData;
  right?: SideCellData;
};

/** Parse GenerateDiffString output (`+12 line`, `-10 line`, ` 11 line`) or a simple +/- preview. */
function parseDiffLines(diff: string): DiffRow[] {
  if (!diff) return [];
  return diff.split("\n").map((line) => {
    const numbered = line.match(/^([+\-\s])(\s*\d+)\s(.*)$/);
    if (numbered) {
      const kind = numbered[1] === "+" ? "add" : numbered[1] === "-" ? "del" : "ctx";
      return { kind, lineNum: numbered[2].trim(), text: numbered[3] } as DiffRow;
    }
    if (line.startsWith("+")) return { kind: "add", text: line.slice(1) };
    if (line.startsWith("-")) return { kind: "del", text: line.slice(1) };
    if (line.startsWith("@@") || line.startsWith("---") || line.startsWith("+++")) {
      return { kind: "meta", text: line };
    }
    if (line.trim() === "...") return { kind: "meta", text: "…" };
    return { kind: "ctx", text: line.startsWith(" ") ? line.slice(1) : line };
  });
}

function unifiedToSideBySide(rows: DiffRow[]): SideBySideRow[] {
  const out: SideBySideRow[] = [];
  let i = 0;
  while (i < rows.length) {
    const row = rows[i];
    if (row.kind === "meta") {
      i++;
      continue;
    }
    if (row.kind === "ctx") {
      const cell: SideCellData = { kind: "ctx", lineNum: row.lineNum, text: row.text };
      out.push({ left: cell, right: { ...cell } });
      i++;
      continue;
    }
    if (row.kind === "del" || row.kind === "add") {
      const dels: DiffRow[] = [];
      const adds: DiffRow[] = [];
      while (i < rows.length && rows[i].kind === "del") dels.push(rows[i++]);
      while (i < rows.length && rows[i].kind === "add") adds.push(rows[i++]);
      const n = Math.max(dels.length, adds.length);
      for (let j = 0; j < n; j++) {
        const left = dels[j]
          ? ({ kind: "del", lineNum: dels[j].lineNum, text: dels[j].text } as SideCellData)
          : undefined;
        const right = adds[j]
          ? ({ kind: "add", lineNum: adds[j].lineNum, text: adds[j].text } as SideCellData)
          : undefined;
        out.push({ left, right });
      }
      continue;
    }
    i++;
  }
  return out;
}

/** Live preview from edit tool args before execution finishes. */
function editsToSideBySide(args: unknown): SideBySideRow[] {
  const a = asArgsObject(args);
  if (!a) return [];
  const edits: Array<{ oldText?: string; newText?: string }> = [];

  if (Array.isArray(a.edits)) {
    for (const e of a.edits) {
      if (e && typeof e === "object") {
        const m = e as Record<string, unknown>;
        edits.push({
          oldText: typeof m.oldText === "string" ? m.oldText : undefined,
          newText: typeof m.newText === "string" ? m.newText : undefined,
        });
      }
    }
  } else if (typeof a.oldText === "string" || typeof a.newText === "string") {
    edits.push({
      oldText: typeof a.oldText === "string" ? a.oldText : undefined,
      newText: typeof a.newText === "string" ? a.newText : undefined,
    });
  }

  const out: SideBySideRow[] = [];
  for (const e of edits) {
    const left = splitLines(e.oldText || "");
    const right = splitLines(e.newText || "");
    const n = Math.max(left.length, right.length);
    for (let i = 0; i < n; i++) {
      out.push({
        left:
          i < left.length
            ? { kind: "del", text: left[i], lineNum: String(i + 1) }
            : undefined,
        right:
          i < right.length
            ? { kind: "add", text: right[i], lineNum: String(i + 1) }
            : undefined,
      });
    }
  }
  return out;
}
