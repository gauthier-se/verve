import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { RangeTokens } from "@/lib/time-range";
import type { Annotation, Bucket } from "@/lib/types";

/** The query key every Annotation read and write shares. A write invalidates this
 *  and nothing else: a note changes no aggregate, and refetching four Panels'
 *  worth of Series because someone typed "holiday" would make the cheapest write
 *  in the app the most expensive one. */
export const ANNOTATIONS_KEY = ["annotations"];

/** useAnnotations loads the Annotations overlapping a time axis, each already
 *  folded onto that axis' bucket grid by the server (ADR 0030). It forwards the
 *  same range tokens and bucket override `useSeries` forwards, so the markers and
 *  the curves are resolved by one module against one window, and two Panels on the
 *  same axis share one cached request.
 *
 *  No baseline token is sent: a Baseline series is laid on the current window's
 *  ordinal axis (ADR 0015), so a marker over it would sit at a bucket whose date is
 *  not the date under it. */
export function useAnnotations(params: {
  range: RangeTokens;
  bucket: Bucket | null;
  enabled?: boolean;
}) {
  const { range, bucket, enabled = true } = params;
  return useQuery({
    queryKey: [...ANNOTATIONS_KEY, range.preset, range.from, range.to, bucket],
    enabled,
    queryFn: async () => {
      const qs = new URLSearchParams({ range_preset: range.preset });
      // Only a custom range carries bounds; relative presets resolve server-side.
      if (range.preset === "custom") {
        if (range.from) qs.set("range_from", range.from);
        if (range.to) qs.set("range_to", range.to);
      }
      if (bucket) qs.set("bucket", bucket);
      const { annotations } = await api<{ annotations: Annotation[] }>(`/v1/annotations?${qs}`);
      return annotations;
    },
  });
}

/** useAllAnnotations loads the Account's whole history, unfolded: no time axis, so
 *  no buckets. It is the Data page's list, the only view that reaches an Annotation
 *  outside the current range. */
export function useAllAnnotations() {
  return useQuery({
    queryKey: [...ANNOTATIONS_KEY, "all"],
    queryFn: async () => {
      const { annotations } = await api<{ annotations: Annotation[] }>("/v1/annotations");
      return annotations;
    },
  });
}

/** AnnotationInput is the create/update body. An absent field is left unchanged by
 *  an update and an empty string clears it, which is how a span becomes a single day
 *  again and how a body is emptied. `null` is not the clearing signal: the server
 *  decodes it to the same absent-field value, here as everywhere else in this API. */
export interface AnnotationInput {
  label?: string;
  body?: string;
  starts_on?: string;
  ends_on?: string;
}

/** useInvalidateAnnotations invalidates the notes and nothing else. That scope is the
 *  point: a note changes no aggregate, so refetching four Panels' worth of Series
 *  because someone typed "holiday" would make the cheapest write in the app the most
 *  expensive one. */
function useInvalidateAnnotations() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: ANNOTATIONS_KEY });
}

/** useCreateAnnotation writes a new note. */
export function useCreateAnnotation() {
  const invalidate = useInvalidateAnnotations();
  return useMutation({
    mutationFn: (input: AnnotationInput) =>
      api<{ annotation: Annotation }>("/v1/annotations", { method: "POST", body: input }),
    onSuccess: invalidate,
  });
}

/** useUpdateAnnotation edits one. */
export function useUpdateAnnotation() {
  const invalidate = useInvalidateAnnotations();
  return useMutation({
    mutationFn: ({ id, patch }: { id: number; patch: AnnotationInput }) =>
      api<{ annotation: Annotation }>(`/v1/annotations/${id}`, { method: "PATCH", body: patch }),
    onSuccess: invalidate,
  });
}

/** useDeleteAnnotation removes one. Every Annotation is deletable, unlike a
 *  Measurement, which is only when its Source is Manual (ADR 0022): they are all
 *  typed by their owner. */
export function useDeleteAnnotation() {
  const invalidate = useInvalidateAnnotations();
  return useMutation({
    mutationFn: (id: number) => api(`/v1/annotations/${id}`, { method: "DELETE" }),
    onSuccess: invalidate,
  });
}
