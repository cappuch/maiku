import {
  Root,
  Trigger,
  Group,
  Portal,
  Sub,
  RadioGroup,
  SubTrigger,
  SubContent,
  Content,
  Item,
  CheckboxItem,
  ItemIndicator,
  RadioItem,
  Label,
  Separator,
} from "@radix-ui/react-dropdown-menu";
import { Check } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

export const DropdownMenu = Root;
export const DropdownMenuTrigger = Trigger;
export const DropdownMenuGroup = Group;
export const DropdownMenuPortal = Portal;
export const DropdownMenuSub = Sub;
export const DropdownMenuRadioGroup = RadioGroup;

export function DropdownMenuContent({
  className,
  sideOffset = 6,
  ...props
}: ComponentProps<typeof Content>) {
  return (
    <Portal>
      <Content
        sideOffset={sideOffset}
        data-wails-no-drag
        className={cn(
          "titlebar-no-drag z-50 min-w-48 overflow-hidden rounded-xl border border-white/10 bg-[#141416] p-1 text-zinc-100 shadow-[0_24px_80px_rgba(0,0,0,0.45)]",
          className,
        )}
        {...props}
      />
    </Portal>
  );
}

export function DropdownMenuItem({
  className,
  inset,
  ...props
}: ComponentProps<typeof Item> & { inset?: boolean }) {
  return (
    <Item
      className={cn(
        "relative flex cursor-default items-center gap-2 rounded-lg px-2.5 py-2 text-xs outline-none select-none data-[disabled]:pointer-events-none data-[disabled]:opacity-40 data-[highlighted]:bg-white/[0.06]",
        inset && "pl-8",
        className,
      )}
      {...props}
    />
  );
}

export function DropdownMenuCheckboxItem({
  className,
  children,
  checked,
  ...props
}: ComponentProps<typeof CheckboxItem>) {
  return (
    <CheckboxItem
      className={cn(
        "relative flex cursor-default items-center rounded-lg py-2 pr-2 pl-8 text-xs outline-none select-none data-[highlighted]:bg-white/[0.06]",
        className,
      )}
      checked={checked}
      {...props}
    >
      <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
        <ItemIndicator>
          <Check size={12} />
        </ItemIndicator>
      </span>
      {children}
    </CheckboxItem>
  );
}

export function DropdownMenuRadioItem({
  className,
  children,
  ...props
}: ComponentProps<typeof RadioItem>) {
  return (
    <RadioItem
      className={cn(
        "relative flex cursor-default items-center rounded-lg py-2 pr-2 pl-8 text-xs outline-none select-none data-[highlighted]:bg-white/[0.06]",
        className,
      )}
      {...props}
    >
      <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
        <ItemIndicator>
          <Check size={12} />
        </ItemIndicator>
      </span>
      {children}
    </RadioItem>
  );
}

export function DropdownMenuLabel({
  className,
  inset,
  ...props
}: ComponentProps<typeof Label> & { inset?: boolean }) {
  return (
    <Label
      className={cn("px-2.5 py-1.5 text-xs text-zinc-500", inset && "pl-8", className)}
      {...props}
    />
  );
}

export function DropdownMenuSeparator({ className, ...props }: ComponentProps<typeof Separator>) {
  return <Separator className={cn("-mx-1 my-1 h-px bg-white/10", className)} {...props} />;
}

export function DropdownMenuSubTrigger({
  className,
  inset,
  children,
  ...props
}: ComponentProps<typeof SubTrigger> & { inset?: boolean }) {
  return (
    <SubTrigger
      className={cn(
        "flex cursor-default items-center rounded-lg px-2.5 py-2 text-xs outline-none select-none data-[highlighted]:bg-white/[0.06]",
        inset && "pl-8",
        className,
      )}
      {...props}
    >
      {children}
    </SubTrigger>
  );
}

export function DropdownMenuSubContent({ className, ...props }: ComponentProps<typeof SubContent>) {
  return (
    <SubContent
      className={cn(
        "z-50 min-w-40 overflow-hidden rounded-xl border border-white/10 bg-[#141416] p-1 shadow-xl",
        className,
      )}
      {...props}
    />
  );
}
