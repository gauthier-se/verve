import * as React from "react";
import { Search, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { Input } from "./ui/input";

/** SearchField is the one filter box the interface uses: a magnifier, the input,
 *  and a clear button that appears only once something is typed. Escape clears it
 *  without closing anything around it, so a filter inside a dialog or a popover is
 *  emptied by the first press and the container is dismissed by the second.
 *
 *  It is deliberately a single component rather than a pattern each page repeats.
 *  A search box is judged on whether it behaves the same everywhere: the same
 *  placeholder grammar, the same clear affordance, the same key. */
export const SearchField = React.forwardRef<
  HTMLInputElement,
  {
    value: string;
    onChange: (value: string) => void;
    placeholder?: string;
    label: string;
    /** hint is the key that focuses this field, printed in the empty box so the
     *  shortcut is found by reading rather than by guessing. */
    hint?: string;
    className?: string;
    autoFocus?: boolean;
  }
>(function SearchField({ value, onChange, placeholder, label, hint, className, autoFocus }, ref) {
  return (
    <div className={cn("relative", className)}>
      <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
      <Input
        ref={ref}
        type="search"
        autoFocus={autoFocus}
        aria-label={label}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape" && value !== "") {
            e.stopPropagation();
            onChange("");
          }
        }}
        // The native search input's own clear cross is unstyleable and absent in
        // Firefox, so it is suppressed and replaced by the button below.
        className="h-8 pl-8 pr-7 text-xs [&::-webkit-search-cancel-button]:appearance-none"
      />
      {/* The hint and the clear button share the right slot, and never collide:
          one is what an empty box offers, the other what a filled one offers. */}
      {value === "" && hint && (
        <kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 rounded border px-1 font-mono text-3xs text-muted-foreground">
          {hint}
        </kbd>
      )}
      {value !== "" && (
        <button
          type="button"
          aria-label="Clear the search"
          onClick={() => onChange("")}
          className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground transition-colors hover:text-foreground"
        >
          <X className="size-3.5" />
        </button>
      )}
    </div>
  );
});
