import * as React from "react";
import { format, parseISO } from "date-fns";
import type { DateRange } from "react-day-picker";
import { CalendarDays } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Calendar } from "./ui/calendar";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";

/** DayRangePicker is the shared custom day-range picker used by both the Time
 *  range and the comparison Baseline controls: a trigger button showing the
 *  chosen `from → to` (or a placeholder) that opens a two-month calendar. Bounds
 *  are day-granularity YYYY-MM-DD strings, the shape the range/baseline columns
 *  persist. Open state is uncontrolled by default; pass `open`/`onOpenChange` to
 *  drive it (e.g. to auto-open when the user first picks "Custom"). */
export function DayRangePicker({
  from,
  to,
  onSelect,
  placeholder = "Custom",
  active = false,
  open,
  onOpenChange,
  align = "end",
}: {
  from: string | null;
  to: string | null;
  onSelect: (from: string, to: string) => void;
  placeholder?: string;
  active?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  align?: "start" | "center" | "end";
}) {
  const [internalOpen, setInternalOpen] = React.useState(false);
  const isOpen = open ?? internalOpen;
  const setOpen = onOpenChange ?? setInternalOpen;

  // The picker holds its own draft range so the first click's endpoint survives
  // until the second completes it: the committed bounds only land once both ends
  // are chosen, so a prop-derived `selected` alone could never advance mid-pick.
  // Re-seed from the persisted bounds whenever they change (reload, rule switch).
  const seed = (): DateRange | undefined => (from && to ? { from: parseISO(from), to: parseISO(to) } : undefined);
  const [range, setRange] = React.useState<DateRange | undefined>(seed);
  React.useEffect(() => setRange(seed()), [from, to]);

  const label = from && to ? `${from} → ${to}` : placeholder;

  const handleSelect = (next: DateRange | undefined) => {
    setRange(next);
    if (next?.from && next?.to) {
      onSelect(format(next.from, "yyyy-MM-dd"), format(next.to, "yyyy-MM-dd"));
      setOpen(false);
    }
  };

  return (
    <Popover open={isOpen} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant={active ? "secondary" : "outline"} size="sm" className={cn("h-8 gap-1.5", active && "font-medium")}>
          <CalendarDays className="size-4" />
          {label}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align={align}>
        <Calendar mode="range" numberOfMonths={2} selected={range} defaultMonth={range?.from} onSelect={handleSelect} />
      </PopoverContent>
    </Popover>
  );
}
