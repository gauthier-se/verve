import * as React from "react";
import { Trash2 } from "lucide-react";
import {
  useCreateAnnotation,
  useDeleteAnnotation,
  useUpdateAnnotation,
  type AnnotationInput,
} from "@/hooks/use-annotations";
import { ApiError } from "@/lib/api";
import type { Annotation } from "@/lib/types";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "./ui/dialog";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Textarea } from "./ui/textarea";

/** today is the fallback prefill: the day you are most likely writing about when
 *  nothing on screen says otherwise. Local wall time, because "today" is a thing
 *  about where the person is, not about UTC. */
function today(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** AnnotationDialog writes, edits and deletes one note (ADR 0030). One dialog for
 *  three verbs, like the manual entry one, because an Annotation is small enough
 *  that a separate edit screen would be more chrome than content.
 *
 *  `annotation` opens it on an existing note; otherwise it creates one, prefilled
 *  with `defaultDay`: the bucket the cursor was last on, when a chart could say.
 *  The moment you want to annotate a day is the moment you are looking at it, and
 *  retyping the date is the friction that stops the note being written. */
export function AnnotationDialog({
  open,
  onOpenChange,
  annotation,
  defaultDay,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  annotation?: Annotation | null;
  defaultDay?: string | null;
}) {
  const create = useCreateAnnotation();
  const update = useUpdateAnnotation();
  const remove = useDeleteAnnotation();

  const [label, setLabel] = React.useState("");
  const [body, setBody] = React.useState("");
  const [startsOn, setStartsOn] = React.useState(today);
  const [endsOn, setEndsOn] = React.useState("");
  const [confirmingDelete, setConfirmingDelete] = React.useState(false);

  // Seed on every open so the dialog never reappears holding the last note's text,
  // and never holds a stale "today" from whenever the app was loaded.
  React.useEffect(() => {
    if (!open) return;
    setLabel(annotation?.label ?? "");
    setBody(annotation?.body ?? "");
    setStartsOn(annotation?.starts_on ?? defaultDay ?? today());
    setEndsOn(annotation?.ends_on ?? "");
    setConfirmingDelete(false);
    create.reset();
    update.reset();
    remove.reset();
  }, [open, annotation, defaultDay]); // eslint-disable-line react-hooks/exhaustive-deps

  const pending = create.isPending || update.isPending || remove.isPending;
  const error = create.error ?? update.error ?? remove.error;
  const fields = error instanceof ApiError ? error.fields : undefined;

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = label.trim();
    if (!trimmed || !startsOn) return;
    // The empty string is what clears a body or an end day; an absent field would
    // leave the old value in place, which is not what an emptied input means.
    const input: AnnotationInput = {
      label: trimmed,
      body: body.trim(),
      starts_on: startsOn,
      ends_on: endsOn,
    };
    const done = { onSuccess: () => onOpenChange(false) };
    if (annotation) update.mutate({ id: annotation.id, patch: input }, done);
    else create.mutate(input, done);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{annotation ? "Edit note" : "Add a note"}</DialogTitle>
        </DialogHeader>

        <form className="space-y-3" onSubmit={submit}>
          <div className="space-y-1.5">
            <Label htmlFor="annotation-label">What happened</Label>
            <Input
              id="annotation-label"
              autoFocus
              maxLength={120}
              placeholder="Flu, a trip, a change of program…"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              required
            />
            {fields?.label && <p className="text-xs text-destructive">{fields.label}</p>}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="annotation-from">From</Label>
              <Input
                id="annotation-from"
                type="date"
                value={startsOn}
                onChange={(e) => setStartsOn(e.target.value)}
                required
              />
              {fields?.starts_on && <p className="text-xs text-destructive">{fields.starts_on}</p>}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="annotation-to">
                To <span className="text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="annotation-to"
                type="date"
                min={startsOn || undefined}
                value={endsOn}
                onChange={(e) => setEndsOn(e.target.value)}
              />
              {fields?.ends_on && <p className="text-xs text-destructive">{fields.ends_on}</p>}
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="annotation-body">
              Notes <span className="text-muted-foreground">(optional)</span>
            </Label>
            <Textarea
              id="annotation-body"
              maxLength={2000}
              rows={3}
              value={body}
              onChange={(e) => setBody(e.target.value)}
            />
            {fields?.body && <p className="text-xs text-destructive">{fields.body}</p>}
          </div>

          {error && !fields && (
            <p className="text-sm text-destructive">
              {error instanceof ApiError ? error.message : "Could not save the note."}
            </p>
          )}

          <div className="flex items-center justify-end gap-2">
            {annotation && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="mr-auto text-destructive hover:text-destructive"
                disabled={pending}
                onClick={() =>
                  confirmingDelete
                    ? remove.mutate(annotation.id, { onSuccess: () => onOpenChange(false) })
                    : setConfirmingDelete(true)
                }
              >
                <Trash2 className="size-4" />
                {confirmingDelete ? "Confirm delete" : "Delete"}
              </Button>
            )}
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending || !label.trim() || !startsOn}>
              {pending ? "Saving…" : "Save"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
