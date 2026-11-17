import { http, HttpResponse } from "msw";

import { problemResponse } from "@/test/problem";
import { server } from "@/test/server";

/** The path of the tus endpoint of the uploads. */
export const uploadsPath = "/api/v1/uploads/";

/** A mebibyte, in bytes. */
export const MiB = 1024 * 1024;

/** A creation that the fake received. */
export interface FakeCreation {
  length: number;
  /** The filename of `Upload-Metadata`, when it has one. */
  fileName: string | undefined;
  /** The value of the CSRF header. */
  csrf: string | null;
}

/** A write that the fake received. */
export interface FakePatch {
  id: string;
  offset: number;
  size: number;
  /** The value of the CSRF header. */
  csrf: string | null;
}

/** How the writes fail, from the time `fail` is called. */
export type FakeFailure =
  /** The connection is lost: the request gets no response. */
  | "network"
  /** The disk of the server is full. */
  | "diskFull";

interface FakeUploadsOptions {
  /** The largest upload that the server accepts, `VERVE_MAX_UPLOAD_SIZE`. */
  maxSize?: number;
  /**
   * The id of the import that a completed upload creates, or null for a
   * response without `Verve-Import-Id`, as a proxy that drops it answers.
   */
  importId?: number | null;
}

interface Hold {
  offset: number;
  released: Promise<"release" | "drop">;
  settle: (how: "release" | "drop") => void;
}

/**
 * Fakes the tus endpoint of the uploads, as the server serves it (FR-2 of XML
 * import): a creation needs `Upload-Length`, answers 413 above `maxSize`, and
 * keeps the filename of `Upload-Metadata`. A write appends at the stored
 * offset, answers 409 at another one, and the write that reaches the length
 * answers the id of the import in `Verve-Import-Id`. Every request but HEAD
 * needs the CSRF header. Returns the creations and the writes it received,
 * and controls to hold the write at an offset or make the writes fail.
 */
export function fakeUploads({
  maxSize = 1024 * MiB,
  importId = 5,
}: FakeUploadsOptions = {}) {
  const uploads = new Map<string, { length: number; offset: number }>();
  const creations: FakeCreation[] = [];
  const patches: FakePatch[] = [];
  let nextId = 1;
  let hold: Hold | undefined;
  let failure: FakeFailure | undefined;

  server.use(
    http.post(uploadsPath, ({ request }) => {
      const csrf = request.headers.get("Verve-Csrf");
      if (csrf !== "1") {
        return problemResponse({
          title: "Forbidden",
          status: 403,
          code: "csrf_failed",
        });
      }
      const length = Number(request.headers.get("Upload-Length"));
      creations.push({
        length,
        fileName: metadataFileName(request.headers.get("Upload-Metadata")),
        csrf,
      });
      if (length > maxSize) {
        return problemResponse({
          title: "Request Entity Too Large",
          status: 413,
          code: "request_entity_too_large",
        });
      }
      const id = `upload-${String(nextId++)}`;
      uploads.set(id, { length, offset: 0 });
      return new HttpResponse(null, {
        status: 201,
        headers: { Location: `${uploadsPath}${id}`, "Tus-Resumable": "1.0.0" },
      });
    }),
    http.head<{ id: string }>(`${uploadsPath}:id`, ({ params }) => {
      const upload = uploads.get(params.id);
      if (upload === undefined) {
        return new HttpResponse(null, { status: 404 });
      }
      return new HttpResponse(null, {
        status: 200,
        headers: {
          "Upload-Offset": String(upload.offset),
          "Upload-Length": String(upload.length),
          "Cache-Control": "no-store",
          "Tus-Resumable": "1.0.0",
        },
      });
    }),
    http.patch<{ id: string }>(
      `${uploadsPath}:id`,
      async ({ request, params }) => {
        const upload = uploads.get(params.id);
        if (upload === undefined) {
          return new HttpResponse(null, { status: 404 });
        }
        const csrf = request.headers.get("Verve-Csrf");
        if (csrf !== "1") {
          return problemResponse({
            title: "Forbidden",
            status: 403,
            code: "csrf_failed",
          });
        }
        const offset = Number(request.headers.get("Upload-Offset"));
        if (hold?.offset === offset) {
          const held = hold;
          // One write only: the client resumes once it is settled.
          hold = undefined;
          if ((await held.released) === "drop") {
            return HttpResponse.error();
          }
        }
        if (failure === "network") {
          return HttpResponse.error();
        }
        if (failure === "diskFull") {
          return problemResponse({
            title: "Insufficient Storage",
            status: 507,
            code: "insufficient_storage",
          });
        }
        if (offset !== upload.offset) {
          return problemResponse({
            title: "Conflict",
            status: 409,
            code: "conflict",
          });
        }
        const size = (await request.arrayBuffer()).byteLength;
        patches.push({ id: params.id, offset, size, csrf });
        upload.offset += size;
        const headers: Record<string, string> = {
          "Upload-Offset": String(upload.offset),
          "Tus-Resumable": "1.0.0",
        };
        if (upload.offset === upload.length) {
          uploads.delete(params.id);
          if (importId !== null) {
            headers["Verve-Import-Id"] = String(importId);
          }
        }
        return new HttpResponse(null, { status: 204, headers });
      },
    ),
  );

  return {
    creations,
    patches,
    /**
     * Holds the next write at `offset` until `release` lets it through, or
     * `drop` loses its connection without writing it.
     */
    holdAt(offset: number) {
      let settle: Hold["settle"] = () => undefined;
      const released = new Promise<"release" | "drop">((resolve) => {
        settle = resolve;
      });
      hold = { offset, released, settle };
      return {
        release: () => {
          settle("release");
        },
        drop: () => {
          settle("drop");
        },
      };
    },
    /** Makes every write fail from now on, until `recover`. */
    fail(how: FakeFailure) {
      failure = how;
    },
    /** Lets the writes succeed again. */
    recover() {
      failure = undefined;
    },
  };
}

/** The filename of an `Upload-Metadata` header, decoded from base64. */
function metadataFileName(metadata: string | null): string | undefined {
  for (const pair of (metadata ?? "").split(",")) {
    const [key, value] = pair.trim().split(" ");
    if (key === "filename" && value !== undefined) {
      return new TextDecoder().decode(
        Uint8Array.from(atob(value), (char) => char.charCodeAt(0)),
      );
    }
  }
  return undefined;
}

/** A file of `size` bytes named `name`, modified at a fixed time. */
export function exportFile(size: number, name = "export.zip") {
  return new File([new Uint8Array(size)], name, {
    type: "application/zip",
    lastModified: Date.UTC(2026, 10, 10),
  });
}
