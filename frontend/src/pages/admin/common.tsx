import { type ReactNode } from "react";
import { AnimatePresence, m, useReducedMotion } from "motion/react";
import { Icon } from "../../components/Icon";
import { Button } from "../../components/ui";
import { cn } from "../../lib/utils";

export const surfaceClass = "rounded-[14px] border border-line bg-white";
export const buttonClass =
  "inline-flex min-h-11 items-center justify-center gap-2 rounded-[10px] border border-line-strong bg-white px-[18px] text-[13px] font-bold text-ink hover:bg-canvas disabled:cursor-not-allowed disabled:border-line disabled:bg-[#efedf1] disabled:text-[#aaa4ad]";

export const avatarClass =
  "grid size-[38px] shrink-0 place-items-center rounded-full bg-violet-soft text-[11px] font-extrabold text-violet";
export const glyphClass =
  "grid size-[38px] shrink-0 place-items-center rounded-xl bg-violet-soft text-violet";
export const detailSectionClass = "border-b border-line p-[18px]";
export const assignedRowClass =
  "grid min-h-[52px] grid-cols-[38px_minmax(0,1fr)_32px] items-center gap-[9px] border-b border-line px-[9px] py-[7px] last:border-b-0 [&_b]:block [&_b]:text-[9px] [&_small]:block [&_small]:text-[8px] [&_small]:text-muted";
export const pickerOptionClass =
  "grid min-h-[50px] w-full grid-cols-[34px_minmax(0,1fr)_40px] items-center gap-[9px] rounded-lg border-0 bg-transparent p-[7px] text-left text-ink hover:bg-white [&_b]:block [&_b]:text-[9px] [&_small]:mt-[3px] [&_small]:block [&_small]:text-[8px] [&_small]:text-muted";
export const removeButtonClass =
  "grid size-8 place-items-center rounded-lg border-0 bg-transparent p-0 text-lg text-muted hover:bg-danger-soft hover:text-danger";

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
  const reduceMotion = useReducedMotion();
  return (
    <Button
      className={cn("min-h-[38px] px-[11px] text-[9px]", className)}
      onClick={onClick}
    >
      <span className="relative inline-grid size-[18px] place-items-center">
        <AnimatePresence initial={false} mode="sync">
          <m.span
            className="absolute inset-0 grid place-items-center"
            key={open ? "close" : "plus"}
            initial={reduceMotion ? false : { opacity: 0, scale: 0.95 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={
              reduceMotion
                ? undefined
                : {
                    opacity: 0,
                    scale: 0.95,
                    transition: { duration: 0.1, ease: "easeOut" },
                  }
            }
            transition={{ duration: 0.13, ease: "easeOut" }}
          >
            <Icon name={open ? "close" : "plus"} />
          </m.span>
        </AnimatePresence>
      </span>
      {label}
    </Button>
  );
}

export function PickerPopover({ children }: { children: ReactNode }) {
  const reduceMotion = useReducedMotion();
  return (
    <m.div
      className="mt-[9px] rounded-[10px] border border-line-strong bg-[#fbfafc] p-[9px]"
      initial={reduceMotion ? false : { opacity: 0, scale: 0.95, y: -4 }}
      animate={{ opacity: 1, scale: 1, y: 0 }}
      exit={
        reduceMotion
          ? undefined
          : {
              opacity: 0,
              scale: 0.95,
              y: -4,
              transition: { duration: 0.14, ease: "easeOut" },
            }
      }
      transition={{ duration: 0.18, ease: "easeOut" }}
      style={{ transformOrigin: "top right" }}
    >
      {children}
    </m.div>
  );
}

export function StaggerRow({
  index,
  className,
  children,
}: {
  index: number;
  className: string;
  children: ReactNode;
}) {
  const reduceMotion = useReducedMotion();
  return (
    <m.article
      className={className}
      initial={reduceMotion ? false : { opacity: 0, y: 3 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{
        duration: 0.15,
        ease: "easeOut",
        delay: Math.min(index, 4) * 0.03,
      }}
    >
      {children}
    </m.article>
  );
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
    <div className="mt-[10px] overflow-hidden rounded-[10px] border border-line empty:hidden">
      {children}
    </div>
  );
}

export function AssignmentEmpty({ children }: { children: ReactNode }) {
  return (
    <p className="mt-[10px] rounded-[10px] border border-dashed border-line p-5 text-center text-[9px] text-muted">
      {children}
    </p>
  );
}
