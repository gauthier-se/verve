import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  defaultOptions,
  DetailedError,
  Upload,
  type UrlStorage,
} from "tus-js-client";

/** The size of each write of an upload (FR-2 of XML import). */
export const uploadChunkSize = 8 * 1024 * 1024;

/** The waits before each retry of a failed request of an upload, in ms. */
const retryDelays = [0, 1_000, 3_000, 5_000];

/** Why an upload stopped before its end. */
export type UploadFailure =
  /** The file is larger than `VERVE_MAX_UPLOAD_SIZE`. */
  | "tooLarge"
  /** The disk of the server is full. */
  | "diskFull"
  /** The server could not be reached, after every retry. */
  | "network"
  | "other";

/** The upload of the session, which outlives the page that started it. */
export type UploadState =
  | { status: "idle" }
  | {
      status: "uploading";
      fileName: string;
      bytesSent: number;
      bytesTotal: number;
    }
  | { status: "failed"; fileName: string; failure: UploadFailure }
  | { status: "completed"; fileName: string; importId: number };

interface UploadContextValue {
  state: UploadState;
  /**
   * Uploads `file`, resuming the upload of the same file that a previous
   * page left incomplete.
   */
  start: (file: File) => void;
  /** Uploads the file of a failed upload again, from where it stopped. */
  retry: () => void;
  /** Forgets a completed or failed upload, to choose another file. */
  reset: () => void;
}

const UploadContext = createContext<UploadContextValue | undefined>(undefined);

/**
 * Holds the upload of an Apple Health export with tus, so that it continues
 * while the user navigates between the pages of the session. The browser
 * keeps the address of each incomplete upload in local storage, so that
 * selecting the same file after a reload resumes it.
 */
export function UploadProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<UploadState>({ status: "idle" });
  const upload = useRef<Upload | undefined>(undefined);
  const file = useRef<File | undefined>(undefined);

  const start = useCallback((next: File) => {
    void upload.current?.abort();
    file.current = next;
    setState({
      status: "uploading",
      fileName: next.name,
      bytesSent: 0,
      bytesTotal: next.size,
    });

    const current: Upload = new Upload(next, {
      endpoint: new URL("/api/v1/uploads/", window.location.origin).href,
      chunkSize: uploadChunkSize,
      retryDelays,
      headers: { "Verve-Csrf": "1" },
      metadata: { filename: next.name },
      storeFingerprintForResuming: true,
      removeFingerprintOnSuccess: true,
      urlStorage: safeUrlStorage(defaultOptions.urlStorage),
      onProgress: (bytesSent, bytesTotal) => {
        if (upload.current === current) {
          setState({
            status: "uploading",
            fileName: next.name,
            bytesSent,
            bytesTotal,
          });
        }
      },
      onSuccess: ({ lastResponse }) => {
        if (upload.current === current) {
          setState({
            status: "completed",
            fileName: next.name,
            importId: Number(lastResponse.getHeader("Verve-Import-Id")),
          });
        }
      },
      onError: (error) => {
        if (upload.current === current) {
          setState({
            status: "failed",
            fileName: next.name,
            failure: uploadFailure(error),
          });
        }
      },
    });
    upload.current = current;

    void current.findPreviousUploads().then((previous) => {
      const [latest] = previous;
      if (latest !== undefined) {
        current.resumeFromPreviousUpload(latest);
      }
      if (upload.current === current) {
        current.start();
      }
    });
  }, []);

  const retry = useCallback(() => {
    if (file.current !== undefined) {
      start(file.current);
    }
  }, [start]);

  const reset = useCallback(() => {
    void upload.current?.abort();
    upload.current = undefined;
    file.current = undefined;
    setState({ status: "idle" });
  }, []);

  // The upload stops with the session, when the user logs out.
  useEffect(
    () => () => {
      void upload.current?.abort();
      upload.current = undefined;
    },
    [],
  );

  const value = useMemo(
    () => ({ state, start, retry, reset }),
    [state, start, retry, reset],
  );

  return (
    <UploadContext.Provider value={value}>{children}</UploadContext.Provider>
  );
}

/** The upload of the session, inside `UploadProvider`. */
export function useUpload(): UploadContextValue {
  const value = useContext(UploadContext);
  if (value === undefined) {
    throw new Error("useUpload must be used inside UploadProvider");
  }
  return value;
}

/** Why a request of an upload failed, from the status of its response. */
function uploadFailure(error: Error): UploadFailure {
  if (!(error instanceof DetailedError)) {
    return "other";
  }
  switch (error.originalResponse?.getStatus()) {
    case undefined:
      return "network";
    case 413:
      return "tooLarge";
    case 507:
      return "diskFull";
    default:
      return "other";
  }
}

/**
 * `storage` with every access caught: local storage can throw, as in a
 * private window, and the upload then works without resuming.
 */
function safeUrlStorage(storage: UrlStorage): UrlStorage {
  async function attempt<T>(access: () => Promise<T>, fallback: T) {
    try {
      return await access();
    } catch {
      return fallback;
    }
  }

  return {
    findAllUploads: () => attempt(() => storage.findAllUploads(), []),
    findUploadsByFingerprint: (fingerprint) =>
      attempt(() => storage.findUploadsByFingerprint(fingerprint), []),
    removeUpload: (key) =>
      attempt(() => storage.removeUpload(key), undefined),
    addUpload: (fingerprint, previous) =>
      attempt(() => storage.addUpload(fingerprint, previous), ""),
  };
}
