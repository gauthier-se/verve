import { HttpResponse } from "msw";

import type { Problem } from "@/api/generated";

/** An error response as the Verve API sends it: a problem with its status. */
export function problemResponse(problem: Omit<Problem, "type">) {
  return HttpResponse.json(
    { type: "about:blank", ...problem },
    {
      status: problem.status,
      headers: { "Content-Type": "application/problem+json" },
    },
  );
}
