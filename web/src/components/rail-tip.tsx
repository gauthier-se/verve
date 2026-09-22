import * as React from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";

/** RailTip names an icon on the collapsed rail, where the label it stands for is
 *  gone. Expanded, the label is on screen and the tooltip would only repeat it. */
export function RailTip({ label, show, children }: { label: string; show: boolean; children: React.ReactElement }) {
  if (!show) return children;
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  );
}
