import * as React from "react";
import * as LabelPrimitive from "@radix-ui/react-label";
import { cn } from "@/lib/utils";

/** Field is one labelled control and its help or error text, stacked. It is the
 *  unit a form is composed of: a form is a FieldGroup of Fields, so the spacing
 *  between controls lives here once rather than in every screen. */
const Field = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("flex w-full flex-col gap-2", className)} {...props} />
  ),
);
Field.displayName = "Field";

/** FieldGroup stacks the Fields of one form with the wider rhythm that separates
 *  controls from each other. */
const FieldGroup = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("flex w-full flex-col gap-6", className)} {...props} />
  ),
);
FieldGroup.displayName = "FieldGroup";

const FieldLabel = React.forwardRef<
  React.ElementRef<typeof LabelPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof LabelPrimitive.Root>
>(({ className, ...props }, ref) => (
  <LabelPrimitive.Root
    ref={ref}
    className={cn(
      "text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70",
      className,
    )}
    {...props}
  />
));
FieldLabel.displayName = "FieldLabel";

/** FieldDescription is the quiet line under a control or a heading: help text,
 *  never an error. An error is FieldError, which is styled to be noticed. */
const FieldDescription = React.forwardRef<HTMLParagraphElement, React.HTMLAttributes<HTMLParagraphElement>>(
  ({ className, ...props }, ref) => (
    <p
      ref={ref}
      className={cn("text-sm text-muted-foreground [&_a]:underline [&_a]:underline-offset-4", className)}
      {...props}
    />
  ),
);
FieldDescription.displayName = "FieldDescription";

/** FieldError states what is wrong. It renders nothing when there is nothing to
 *  say, so a caller can pass a possibly-absent message without branching. */
const FieldError = React.forwardRef<HTMLParagraphElement, React.HTMLAttributes<HTMLParagraphElement>>(
  ({ className, children, ...props }, ref) =>
    children ? (
      <p ref={ref} role="alert" className={cn("text-sm text-destructive", className)} {...props}>
        {children}
      </p>
    ) : null,
);
FieldError.displayName = "FieldError";

/** FieldSeparator is a rule across the form, optionally with a word set into it. */
const FieldSeparator = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, children, ...props }, ref) => (
    <div ref={ref} className={cn("relative flex items-center", className)} {...props}>
      <span className="h-px flex-1 bg-border" />
      {children ? <span className="px-3 text-xs text-muted-foreground">{children}</span> : null}
      <span className="h-px flex-1 bg-border" />
    </div>
  ),
);
FieldSeparator.displayName = "FieldSeparator";

export { Field, FieldGroup, FieldLabel, FieldDescription, FieldError, FieldSeparator };
