import { type ReactNode } from "react";
import { Icon } from "../../components/Icon";
import { Button } from "../../components/ui";
import { cn } from "../../lib/utils";

export const surfaceClass = 'rounded-2xl border border-line/80 bg-white shadow-2xs';
export const buttonClass =
  "inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-line-strong bg-white px-[18px] text-[13px] font-bold text-ink hover:bg-canvas disabled:cursor-not-allowed disabled:border-line disabled:bg-disabled-bg disabled:text-disabled-fg";

export const avatarClass =
  "grid size-9 shrink-0 place-items-center rounded-full bg-violet-soft text-xs font-extrabold text-violet-fg";
export const glyphClass =
  "grid size-9 shrink-0 place-items-center rounded-xl bg-violet-soft text-violet-fg";
export const detailSectionClass = "border-b border-line p-5";
export const assignedRowClass =
  "grid min-h-[56px] grid-cols-[36px_minmax(0,1fr)_32px] items-center gap-3 border-b border-line px-3 py-2.5 last:border-b-0 [&_b]:block [&_b]:truncate [&_b]:text-xs [&_b]:font-bold [&_b]:text-ink [&_small]:mt-0.5 [&_small]:block [&_small]:truncate [&_small]:text-xs [&_small]:text-muted";
export const pickerOptionClass =
  "grid min-h-[52px] w-full grid-cols-[34px_minmax(0,1fr)_auto] items-center gap-3 rounded-lg border-0 bg-transparent px-2.5 py-2 text-left text-ink hover:bg-white [&_b]:block [&_b]:truncate [&_b]:text-xs [&_b]:font-bold [&_small]:mt-0.5 [&_small]:block [&_small]:truncate [&_small]:text-xs [&_small]:text-muted";
export const removeButtonClass =
  "grid size-8 place-items-center rounded-lg border-0 bg-transparent p-0 text-muted hover:bg-danger-soft hover:text-danger-fg";

export function PickerAction({
  open,
  label,
  onClick,
  className,
}: {
  open: boolean;
  label: string;
  onClick: () => void;
  className?: string;
}) {
  return (
    <Button
      size="sm"
      className={cn("min-h-9 px-3 text-xs", className)}
      onClick={onClick}
    >
      <Icon name={open ? "close" : "plus"} className="size-3.5" />
      {label}
    </Button>
  );
}

export function PickerPopover({ children }: { children: ReactNode }) {
  return (
    <div className="mt-2.5 rounded-xl border border-line-strong bg-surface-subtle p-3">
      {children}
    </div>
  );
}

export function StaggerRow({
  className,
  children,
}: {
  className: string;
  children: ReactNode;
}) {
  return <article className={className}>{children}</article>;
}

export function AssignmentHeading({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-center gap-3 max-[600px]:flex-col max-[600px]:items-stretch [&>div]:min-w-0 [&>div]:flex-1 [&>h3]:min-w-0 [&>h3]:flex-1">
      {children}
    </div>
  );
}

export function AssignedList({ children }: { children: ReactNode }) {
  return (
    <div className="mt-3 overflow-hidden rounded-xl border border-line bg-white empty:hidden">
      {children}
    </div>
  );
}

export function AssignmentEmpty({ children }: { children: ReactNode }) {
  return (
    <p className="mt-3 rounded-xl border border-dashed border-line p-5 text-center text-xs text-muted">
      {children}
    </p>
  );
}
