import { http, HttpResponse } from "msw";

import type {
  Import,
  ImportCounters,
  ImportSummary,
  Metric,
} from "@/api/generated";
import { problemResponse } from "@/test/problem";
import { server } from "@/test/server";

/** The path of the imports of the user of the session. */
export const importsPath = "/api/v1/imports";

/** The path of the metric types that the user can see. */
export const metricsPath = "/api/v1/metrics";

/** Counters at zero, which each import overrides. */
export const zeroCounters: ImportCounters = {
  received: 0,
  inserted: 0,
  updated: 0,
  duplicates: 0,
  blocked: 0,
  invalid: 0,
  unsupported: 0,
  suspicious: 0,
  warnings: 0,
  deleted: 0,
  notFound: 0,
};

/**
 * A native push of the companion app that completed in 2 minutes 5 seconds,
 * with a curated type, an uncurated type, an unsupported type and a workout.
 */
export const completedImport: Import = {
  id: 3,
  origin: "native",
  status: "completed",
  client: { name: "Verve for iOS", version: "1.0.0" },
  tokenName: "iPhone",
  createdAt: "2026-11-10T08:00:00Z",
  startedAt: "2026-11-10T08:00:05Z",
  finishedAt: "2026-11-10T08:02:10Z",
  report: {
    total: {
      ...zeroCounters,
      received: 1201,
      inserted: 1180,
      duplicates: 18,
      invalid: 2,
      unsupported: 1,
    },
    types: {
      step_count: { received: 1100, inserted: 1082, duplicates: 18 },
      uv_exposure: { received: 99, inserted: 97, invalid: 2 },
      HKDataTypeIdentifierElectrocardiogram: { received: 1, unsupported: 1 },
    },
    workouts: { received: 1, inserted: 1 },
    errors: [
      'record 17 (HKQuantityTypeIdentifierUVExposure): unit "bpm" is not a unit of count',
    ],
  },
};

/** An Apple Health export waiting for its turn, without counters yet. */
export const queuedImport: Import = {
  id: 2,
  origin: "xml",
  status: "queued",
  createdAt: "2026-11-09T19:30:00Z",
  report: { total: zeroCounters, types: {}, workouts: {}, errors: [] },
};

/** A Health Auto Export push that failed as a whole. */
export const failedImport: Import = {
  id: 1,
  origin: "hae",
  status: "failed",
  tokenName: "Health Auto Export",
  createdAt: "2026-11-08T07:00:00Z",
  startedAt: "2026-11-08T07:00:01Z",
  finishedAt: "2026-11-08T07:00:03Z",
  report: {
    total: zeroCounters,
    types: {},
    workouts: {},
    errors: ["the payload is not valid JSON"],
  },
};

/** A running import, which has received records so far. */
export const runningImport: Import = {
  id: 4,
  origin: "native",
  status: "running",
  client: { name: "Verve for iOS", version: "1.0.0" },
  tokenName: "iPhone",
  createdAt: "2026-11-11T11:58:00Z",
  startedAt: "2026-11-11T11:58:02Z",
  report: {
    total: { ...zeroCounters, received: 5000, inserted: 5000 },
    types: { step_count: { received: 5000, inserted: 5000 } },
    workouts: {},
    errors: [],
  },
};

/** The step count, curated, and the UV exposure, uncurated. */
export const metrics: Metric[] = [
  {
    key: "step_count",
    identifier: "HKQuantityTypeIdentifierStepCount",
    curated: true,
    aggregation: "cumulative",
    group: "activity",
    kind: "quantity",
    unit: "count",
    values: [],
    availability: null,
  },
  {
    key: "uv_exposure",
    identifier: "HKQuantityTypeIdentifierUVExposure",
    curated: false,
    aggregation: "discrete",
    group: "other",
    kind: "quantity",
    unit: "count",
    values: [],
    availability: null,
  },
];

/** The summary of `imp` that the list answers: the total counters only. */
function summary(imp: Import): ImportSummary {
  return { ...imp, report: { total: imp.report.total } };
}

interface FakeImportsOptions {
  /** The number of imports per page of the list. */
  pageSize?: number;
}

/**
 * Fakes the import endpoints of the user of the session, and the metric
 * types, starting with `initial`, newest first, as the server lists them.
 * The list pages through them by `pageSize`, with the index of the next one
 * as its cursor, and keeps the imports of the `status` filter. A cancellation
 * ends a queued import `cancelled` at once, keeps a running one `running`,
 * which its job ends at its next batch, and answers 409 for a finished one.
 * Returns the replace function, to change the imports between requests, and
 * the ids of the cancellations it receives.
 */
export function fakeImports(
  initial: Import[] = [],
  { pageSize = 50 }: FakeImportsOptions = {},
) {
  let imports = [...initial];
  const cancelled: number[] = [];

  server.use(
    http.get(importsPath, ({ request }) => {
      const url = new URL(request.url);
      const status = url.searchParams.get("status");
      const start = Number(url.searchParams.get("cursor") ?? 0);
      const matching = imports.filter(
        (imp) => status === null || imp.status === status,
      );
      const limit = Number(url.searchParams.get("limit") ?? pageSize);
      const size = Math.min(limit, pageSize);
      const items = matching.slice(start, start + size).map(summary);
      const next = start + size;
      return HttpResponse.json({
        items,
        ...(next < matching.length && { nextCursor: String(next) }),
      });
    }),
    http.get<{ id: string }>(`${importsPath}/:id`, ({ params }) => {
      const imp = imports.find(
        (candidate) => candidate.id === Number(params.id),
      );
      return imp
        ? HttpResponse.json(imp)
        : problemResponse({
            title: "Not Found",
            status: 404,
            code: "not_found",
          });
    }),
    http.post<{ id: string }>(`${importsPath}/:id/cancel`, ({ params }) => {
      const id = Number(params.id);
      const imp = imports.find((candidate) => candidate.id === id);
      if (imp === undefined) {
        return problemResponse({
          title: "Not Found",
          status: 404,
          code: "not_found",
        });
      }
      if (imp.status !== "queued" && imp.status !== "running") {
        return problemResponse({
          title: "Conflict",
          status: 409,
          code: "import_finished",
        });
      }
      cancelled.push(id);
      const answer: Import =
        imp.status === "queued"
          ? {
              ...imp,
              status: "cancelled",
              finishedAt: new Date().toISOString(),
            }
          : imp;
      imports = imports.map((candidate) =>
        candidate.id === id ? answer : candidate,
      );
      return HttpResponse.json(answer, { status: 202 });
    }),
    http.get(metricsPath, () => HttpResponse.json({ items: metrics })),
  );

  return {
    cancelled,
    /** Replaces the imports that the endpoints answer from now on. */
    replace: (next: Import[]) => {
      imports = [...next];
    },
  };
}
