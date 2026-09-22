import { cn } from "@/lib/utils";

/** Mark is the product mark: a rounded square in the Palette's own accent carrying
 *  a rising stroke that ends on a plotted point. It is drawn rather than imported
 *  because Verve ships no image assets — and because a mark tinted by the active
 *  Palette belongs to the interface it sits in, which a fixed-colour logo would not.
 *
 *  The glyph is `currentColor` throughout, which is what lets one definition serve
 *  every Palette in both modes (ADR 0024): the badge sets the ink, the mark inherits
 *  it. web/public/favicon.svg is the same two shapes with the colours resolved,
 *  because a browser tab has no interface to inherit from. */
export function Mark({ className }: { className?: string }) {
  return (
    <div
      className={cn(
        "flex size-7 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground",
        className,
      )}
      aria-hidden
    >
      <svg viewBox="0 0 100 100" className="size-[71%]" fill="none">
        <path
          d="M18 30 L42 82 L64 40 L82 20"
          stroke="currentColor"
          strokeWidth={9}
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <circle cx={82} cy={20} r={6} fill="currentColor" />
      </svg>
    </div>
  );
}
