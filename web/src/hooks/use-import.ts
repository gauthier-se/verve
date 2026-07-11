import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, upload } from "@/lib/api";
import type { ImportStatus } from "@/lib/types";

const KEY = ["import-status"];

/** jobActive is true while an import is still uploading or running, the signal to
 *  keep polling. */
function jobActive(status: ImportStatus | undefined): boolean {
  const s = status?.job?.status;
  return s === "pending" || s === "running";
}

/** useImportStatus polls GET /v1/imports while an import is in flight, then settles
 *  (ADR 0016). Pass `forcePoll` (the upload's in-flight flag) so polling starts the
 *  moment an upload begins, before the first job snapshot arrives. */
export function useImportStatus(forcePoll = false) {
  return useQuery({
    queryKey: KEY,
    queryFn: () => api<ImportStatus>("/v1/imports"),
    refetchInterval: (query) =>
      jobActive(query.state.data) || forcePoll ? 600 : false,
  });
}

/** useUploadImport streams a .zip to the import endpoint. On acceptance it primes
 *  the status cache so polling picks up immediately; a rejected upload (oversize,
 *  wrong type, one already running) surfaces as the mutation error. */
export function useUploadImport() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (file: File) =>
      upload<ImportStatus>(`/v1/imports?filename=${encodeURIComponent(file.name)}`, file),
    onSuccess: (status) => {
      qc.setQueryData(KEY, status);
    },
  });
}

/** useOnImportDone returns a callback that invalidates cached series and dashboards
 *  when an import finishes, so the seeded Panels refill without a reload (ADR 0018). */
export function useOnImportDone() {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: ["series"] });
    qc.invalidateQueries({ queryKey: ["dashboards"] });
  };
}
