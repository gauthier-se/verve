import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { LedgerRow } from "@/lib/types";

/** useLedger loads the Ledger overview (GET /v1/ledger, ADR 0021): one folded row per
 *  Metric the Account has data for, over fixed now-relative windows. The endpoint takes
 *  no params (its windows are 7/30 days), so it keys on nothing but its name. */
export function useLedger() {
  return useQuery({
    queryKey: ["ledger"],
    queryFn: async () => {
      const { rows } = await api<{ rows: LedgerRow[] }>("/v1/ledger");
      return rows;
    },
  });
}
