import { createFormHook, createFormHookContexts } from "@tanstack/react-form";
import { type ComponentProps, useId } from "react";

import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

const { fieldContext, formContext, useFieldContext } = createFormHookContexts();

interface TextFieldProps extends Omit<
  ComponentProps<"input">,
  "id" | "name" | "value" | "onChange" | "onBlur"
> {
  label: string;
}

/**
 * A labelled input bound to the field of its `form.AppField`. Once the field
 * is touched, its first error describes the input, which is marked invalid.
 * Validators return the translated text of their errors.
 */
function TextField({ label, className, ...props }: TextFieldProps) {
  const field = useFieldContext<string>();
  const id = useId();
  const errorId = `${id}-error`;
  const error = field.state.meta.isTouched
    ? field.state.meta.errors.find((e): e is string => typeof e === "string")
    : undefined;

  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Input
        {...props}
        id={id}
        name={field.name}
        value={field.state.value}
        onChange={(event) => {
          field.handleChange(event.target.value);
        }}
        onBlur={field.handleBlur}
        aria-invalid={error !== undefined}
        aria-describedby={error === undefined ? undefined : errorId}
        className={cn("h-11", className)}
      />
      {error !== undefined && (
        <p id={errorId} className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}

/**
 * The form hook of the app, whose fields render with `field.TextField`, and
 * `withFieldGroup`, which builds fields shared by several forms.
 */
export const { useAppForm, withFieldGroup } = createFormHook({
  fieldContext,
  formContext,
  fieldComponents: { TextField },
  formComponents: {},
});
