import { Info } from "lucide-react";
import { formatFormula, metricLabel } from "@/lib/metrics";
import type { Formula } from "@/lib/types";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";

/** FormulaHint is the info icon beside a derived Metric that reveals, on hover or
 *  focus, how the Metric is computed — its Formula with human-readable operand names
 *  (ADR 0014). Radix portals the tooltip so a dense Panel grid never clips it. Shared
 *  by the single-Metric Panel header and the multi-Metric legend. */
export function FormulaHint({ formula }: { formula: Formula }) {
  const expr = formatFormula(formula, metricLabel);
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-label={`Calculated as ${expr}`}
          className="shrink-0 rounded-full text-muted-foreground/70 outline-none transition-colors hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
        >
          <Info className="size-3.5" />
        </button>
      </TooltipTrigger>
      <TooltipContent>
        <p className="font-medium">Derived metric</p>
        <p className="mt-0.5 text-muted-foreground">{expr}</p>
      </TooltipContent>
    </Tooltip>
  );
}
