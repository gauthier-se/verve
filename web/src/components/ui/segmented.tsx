import { cn } from "@/lib/utils";

/** Segment is a small labelled segmented control, matching the range and bucket
 *  toggles elsewhere. Shared by the summary prefs and the appearance menu. */
export function Segment({
  label,
  hint,
  value,
  options,
  onChange,
}: {
  label: string;
  hint?: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}) {
  return (
    <div className="space-y-1">
      <p className="text-xs font-medium">{label}</p>
      <div className="flex items-center rounded-md border p-0.5">
        {options.map((o) => (
          <button
            key={o.value}
            type="button"
            onClick={() => onChange(o.value)}
            className={cn(
              "flex-1 rounded px-2 py-1 text-xs transition-colors",
              value === o.value
                ? "bg-secondary font-medium text-secondary-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {o.label}
          </button>
        ))}
      </div>
      {hint && <p className="text-[11px] leading-tight text-muted-foreground">{hint}</p>}
    </div>
  );
}
