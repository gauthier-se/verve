import { Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";

/** Spinner is the shared loading indicator. */
export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={cn("size-4 animate-spin text-muted-foreground", className)} />;
}

/** CenteredSpinner fills its container with a centered spinner, for route-level
 *  and panel-level loading. */
export function CenteredSpinner() {
  return (
    <div className="flex h-full min-h-40 w-full items-center justify-center">
      <Spinner className="size-6" />
    </div>
  );
}
