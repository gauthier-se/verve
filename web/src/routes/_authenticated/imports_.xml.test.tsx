import {
  act,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  onTestFinished,
  vi,
} from "vitest";

import {
  completedXmlImport,
  fakeImports,
  runningXmlImport,
} from "@/test/imports";
import { initI18n } from "@/i18n";
import { renderRoute } from "@/test/render";
import { fakeAuth, member, server } from "@/test/server";
import { exportFile, fakeUploads, MiB } from "@/test/uploads";

/** Opens the upload page for `member`, once it shows its heading. */
async function openUploadPage() {
  server.use(...fakeAuth({ user: member, signedIn: true }));
  const rendered = renderRoute("/imports/xml");
  await screen.findByRole("heading", {
    level: 1,
    name: "Import an Apple Health export",
  });
  return rendered;
}

/** The file picker of the drop zone. */
function getFilePicker() {
  return screen.getByLabelText("Choose a file");
}

/**
 * `text` with each run of white space, such as the narrow no-break space of
 * a formatted number, as one space.
 */
function normalize(text: string | null) {
  return (text ?? "").replace(/\s+/g, " ");
}

/** Lets `ms` of time pass for the timers of the page and its requests. */
async function elapse(ms: number) {
  await act(() => vi.advanceTimersByTimeAsync(ms));
}

/** Moves the focus with the Tab key until it reaches `element`. */
async function tabTo(user: UserEvent, element: HTMLElement) {
  for (let i = 0; i < 30 && document.activeElement !== element; i++) {
    await user.tab();
  }
  expect(element).toHaveFocus();
}

describe("upload page", () => {
  it("given /imports/xml, then the page shows the numbered guide, the expected duration on a Raspberry Pi and a drop zone with a file picker for .zip and .xml files", async () => {
    fakeImports();
    await openUploadPage();

    const guide = screen.getByRole("list", { name: "Export your data" });
    expect(guide.tagName).toBe("OL");
    expect(
      within(guide)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual([
      "Open the Health app on your iPhone.",
      "Tap your profile picture, at the top right.",
      "Tap Export All Health Data, then Export, and wait while the iPhone prepares the export.zip file.",
      "Share the file to Files and upload it here, or open this page in Safari on the iPhone and upload it directly.",
    ]);
    expect(
      screen.getByText(
        "An export of several years can take 10 to 30 minutes to process on a Raspberry Pi.",
      ),
    ).toBeInTheDocument();
    const dropZone = screen.getByRole("region", { name: "Upload the export" });
    expect(dropZone).toHaveTextContent(
      "Drop the export.zip or export.xml file here, or choose it.",
    );
    expect(getFilePicker()).toHaveAttribute("type", "file");
    expect(getFilePicker()).toHaveAttribute("accept", ".zip,.xml");
  });

  it("given the drop zone, when the user reaches its file picker with the keyboard and selects a file, then the file is uploaded", async () => {
    const user = userEvent.setup();
    fakeImports([completedXmlImport]);
    const uploads = fakeUploads();
    await openUploadPage();

    await tabTo(user, getFilePicker());
    await user.upload(getFilePicker(), exportFile(MiB));

    await waitFor(() => {
      expect(uploads.patches).toHaveLength(1);
    });
  });

  it("given the drop zone, when the user drops a file on it, then the file is uploaded", async () => {
    fakeImports([completedXmlImport]);
    const uploads = fakeUploads();
    await openUploadPage();

    fireEvent.drop(screen.getByRole("region", { name: "Upload the export" }), {
      dataTransfer: { files: [exportFile(MiB)], types: ["Files"] },
    });

    await waitFor(() => {
      expect(uploads.patches).toHaveLength(1);
    });
  });

  it("given a file that is neither a .zip nor an .xml file, when the user drops it, then the page asks for the export file and uploads nothing", async () => {
    fakeImports();
    const uploads = fakeUploads();
    await openUploadPage();

    fireEvent.drop(screen.getByRole("region", { name: "Upload the export" }), {
      dataTransfer: {
        files: [new File(["notes"], "notes.txt", { type: "text/plain" })],
        types: ["Files"],
      },
    });

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Choose the export.zip or export.xml file of the Health app.",
    );
    expect(uploads.creations).toEqual([]);
  });

  it("given local storage that throws, as in a private window, when a file is selected, then it is uploaded", async () => {
    const denied = () => {
      throw new DOMException("The storage is not available.", "SecurityError");
    };
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(denied);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(denied);
    onTestFinished(() => {
      vi.restoreAllMocks();
    });
    const user = userEvent.setup();
    fakeImports([completedXmlImport]);
    const uploads = fakeUploads();
    await openUploadPage();

    await user.upload(getFilePicker(), exportFile(MiB));

    await waitFor(() => {
      expect(uploads.patches).toHaveLength(1);
    });
  });

  it("given a file selected, then it is uploaded with tus to /api/v1/uploads/ in chunks of 8 MiB, with the CSRF header and its file name", async () => {
    const user = userEvent.setup();
    fakeImports([completedXmlImport]);
    const uploads = fakeUploads();
    await openUploadPage();

    await user.upload(getFilePicker(), exportFile(20 * MiB));

    await waitFor(() => {
      expect(uploads.patches).toHaveLength(3);
    });
    expect(uploads.creations).toEqual([
      { length: 20 * MiB, fileName: "export.zip", csrf: "1" },
    ]);
    expect(
      uploads.patches.map(({ offset, size, csrf }) => ({ offset, size, csrf })),
    ).toEqual([
      { offset: 0, size: 8 * MiB, csrf: "1" },
      { offset: 8 * MiB, size: 8 * MiB, csrf: "1" },
      { offset: 16 * MiB, size: 4 * MiB, csrf: "1" },
    ]);
  });

  it("given an upload running, then the page shows its percentage, and screen readers hear it at steps of 10 percent", async () => {
    const user = userEvent.setup();
    fakeImports([completedXmlImport]);
    const uploads = fakeUploads();
    const { release } = uploads.holdAt(8 * MiB);
    await openUploadPage();

    await user.upload(getFilePicker(), exportFile(12 * MiB));

    const main = within(screen.getByRole("main"));
    expect(
      await main.findByText("Uploading export.zip: 67%"),
    ).toBeInTheDocument();
    expect(
      main.getByRole("progressbar", { name: "Upload of export.zip" }),
    ).toHaveAttribute("aria-valuenow", "67");
    expect(
      main
        .getAllByRole("status")
        .some((status) => status.textContent === "Upload 60% done."),
    ).toBe(true);
    release();
  });

  it("given a reload during an upload, when the same file is selected again, then the upload resumes at the offset of the server", async () => {
    const user = userEvent.setup();
    fakeImports([completedXmlImport]);
    const uploads = fakeUploads();
    const { drop } = uploads.holdAt(8 * MiB);
    const first = await openUploadPage();
    await user.upload(getFilePicker(), exportFile(12 * MiB));
    await screen.findByText("Uploading export.zip: 67%");

    // The page goes away with its requests, and the held one never arrives.
    first.unmount();
    drop();
    await openUploadPage();
    await user.upload(getFilePicker(), exportFile(12 * MiB));

    await waitFor(() => {
      expect(uploads.patches).toHaveLength(2);
    });
    expect(uploads.creations).toHaveLength(1);
    expect(
      uploads.patches.map(({ offset, size }) => ({ offset, size })),
    ).toEqual([
      { offset: 0, size: 8 * MiB },
      { offset: 8 * MiB, size: 4 * MiB },
    ]);
  });
});

describe("upload page in French", () => {
  it("given the app in French, then the guide, the drop zone, the progress and the import are translated", async () => {
    const user = userEvent.setup();
    const i18n = await initI18n();
    await i18n.changeLanguage("fr");
    fakeImports([runningXmlImport]);
    const uploads = fakeUploads({ importId: runningXmlImport.id });
    const { release } = uploads.holdAt(8 * MiB);
    server.use(...fakeAuth({ user: member, signedIn: true }));
    renderRoute("/imports/xml");

    await screen.findByRole("heading", {
      level: 1,
      name: "Importer un export Apple Health",
    });
    expect(
      within(screen.getByRole("list", { name: "Exportez vos données" }))
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual([
      "Ouvrez l'app Santé sur votre iPhone.",
      "Touchez votre photo de profil, en haut à droite.",
      "Touchez Exporter toutes les données de santé, puis Exporter, et patientez pendant que l'iPhone prépare le fichier export.zip.",
      "Partagez le fichier vers Fichiers et envoyez-le ici, ou ouvrez cette page dans Safari sur l'iPhone et envoyez-le directement.",
    ]);
    expect(
      screen.getByText(
        "Le traitement d'un export de plusieurs années peut prendre de 10 à 30 minutes sur un Raspberry Pi.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("region", { name: "Envoyer l'export" }),
    ).toHaveTextContent(
      "Déposez ici le fichier export.zip ou export.xml, ou choisissez-le.",
    );

    await user.upload(
      screen.getByLabelText("Choisir un fichier"),
      exportFile(12 * MiB),
    );

    expect(
      await screen.findByText(
        (_, element) =>
          element?.tagName === "P" &&
          normalize(element.textContent) ===
            "Envoi du fichier export.zip : 67 %",
      ),
    ).toBeInTheDocument();
    expect(
      screen
        .getAllByRole("status")
        .some((status) => normalize(status.textContent) === "60 % envoyés."),
    ).toBe(true);
    release();
    const imported = await screen.findByRole("region", {
      name: "Importation du fichier export.zip",
    });
    expect(await within(imported).findByText("En cours")).toBeInTheDocument();
    expect(
      within(imported).getByRole("progressbar", {
        name: "Avancement de l'importation",
      }),
    ).toBeInTheDocument();
  });
});

describe("upload page, once the upload ends", () => {
  beforeEach(() => {
    // The timers of the polling and of the retries are fake, and advance on
    // their own with real time, so that the requests complete.
    vi.useFakeTimers({
      toFake: [
        "setTimeout",
        "clearTimeout",
        "setInterval",
        "clearInterval",
        "Date",
      ],
      shouldAdvanceTime: true,
    });
    vi.setSystemTime(new Date("2026-11-11T12:00:00Z"));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function setupUser() {
    return userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
  }

  /** The section of the import of the completed upload, once it shows. */
  function findImportSection() {
    return screen.findByRole("region", { name: "Import of export.zip" });
  }

  it("given an upload that completes, then the page follows the import of Verve-Import-Id: its progress while it runs, then its status, the counters per type, the period and the routes", async () => {
    const user = setupUser();
    const { replace } = fakeImports([runningXmlImport]);
    fakeUploads({ importId: runningXmlImport.id });
    await openUploadPage();

    await user.upload(getFilePicker(), exportFile(MiB));

    const running = await findImportSection();
    expect(await within(running).findByText("Running")).toBeInTheDocument();
    expect(
      within(running).getByRole("progressbar", { name: "Import progress" }),
    ).toHaveAttribute("aria-valuenow", "42");
    expect(within(running).getByText("42% processed")).toBeInTheDocument();

    replace([completedXmlImport]);
    await elapse(5500);

    const completed = await findImportSection();
    expect(await within(completed).findByText("Completed")).toBeInTheDocument();
    expect(
      within(completed).queryByRole("progressbar"),
    ).not.toBeInTheDocument();
    const byType = within(completed).getByRole("region", { name: "By type" });
    expect(
      within(byType)
        .getAllByRole("heading", { level: 4 })
        .map((heading) => heading.textContent),
    ).toEqual(["Steps", "Workouts"]);
    expect(
      normalize(
        within(completed).getByRole("region", { name: "Period" }).textContent,
      ),
    ).toBe("PeriodFrom Dec 30, 2025 to Jan 5, 2026");
    expect(
      within(completed).getByRole("region", { name: "Routes" }),
    ).toHaveTextContent("1 route stored with its workout.");
  });

  it("given an upload that completes without Verve-Import-Id, then the page says that it is complete and links to the imports page", async () => {
    const user = setupUser();
    fakeImports();
    fakeUploads({ importId: null });
    await openUploadPage();

    await user.upload(getFilePicker(), exportFile(MiB));

    expect(
      await screen.findByText(
        "The upload of export.zip is complete. Its import shows on the imports page.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View imports" })).toHaveAttribute(
      "href",
      "/imports",
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("given the app in French and a file above the limit, then the explanation and the offer to try again are translated", async () => {
    const user = setupUser();
    const i18n = await initI18n();
    await i18n.changeLanguage("fr");
    fakeImports();
    fakeUploads({ maxSize: MiB });
    server.use(...fakeAuth({ user: member, signedIn: true }));
    renderRoute("/imports/xml");

    await user.upload(
      await screen.findByLabelText("Choisir un fichier"),
      exportFile(2 * MiB),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Le fichier dépasse la taille que ce serveur accepte.",
    );
    expect(
      screen.getByRole("button", { name: "Réessayer" }),
    ).toBeInTheDocument();
  });

  it.each([
    {
      failure: "a file above the limit",
      message:
        "The file is larger than this server accepts. Its administrator can raise the limit, VERVE_MAX_UPLOAD_SIZE, then you can try again.",
      setup: () => fakeUploads({ maxSize: MiB }),
    },
    {
      failure: "a disk full",
      message:
        "The server has run out of disk space. Once space is freed, try again: the upload resumes where it stopped.",
      setup: () => {
        const uploads = fakeUploads();
        uploads.fail("diskFull");
        return uploads;
      },
    },
    {
      failure: "a network loss that outlasts the retries",
      message:
        "The connection to the server was lost. Check your network, then try again: the upload resumes where it stopped.",
      setup: () => {
        const uploads = fakeUploads();
        uploads.fail("network");
        return uploads;
      },
    },
  ])(
    "given $failure, then the page explains it in plain words and offers to try again",
    async ({ setup, message }) => {
      const user = setupUser();
      fakeImports([completedXmlImport]);
      setup();
      await openUploadPage();

      await user.upload(getFilePicker(), exportFile(2 * MiB));
      // Past every retry of tus.
      await elapse(30_000);

      expect(await screen.findByRole("alert")).toHaveTextContent(message);
      expect(
        screen.getByRole("button", { name: "Try again" }),
      ).toBeInTheDocument();
    },
  );

  it.each(["diskFull", "network"] as const)(
    "given an upload stopped by %s, when the user tries again once it is over, then the upload resumes where it stopped",
    async (failure) => {
      const user = setupUser();
      fakeImports([completedXmlImport]);
      const uploads = fakeUploads();
      const { release } = uploads.holdAt(8 * MiB);
      await openUploadPage();
      await user.upload(getFilePicker(), exportFile(12 * MiB));
      await screen.findByText("Uploading export.zip: 67%");
      uploads.fail(failure);
      release();
      // Past every retry of tus.
      await elapse(30_000);
      await screen.findByRole("alert");

      uploads.recover();
      await user.click(screen.getByRole("button", { name: "Try again" }));

      await waitFor(() => {
        expect(uploads.patches).toHaveLength(2);
      });
      expect(uploads.creations).toHaveLength(1);
      expect(uploads.patches[1]).toMatchObject({
        offset: 8 * MiB,
        size: 4 * MiB,
      });
      expect(await findImportSection()).toBeInTheDocument();
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    },
  );

  it("given local storage that throws, as in a private window, when an upload stopped by a network loss is tried again, then it resumes where it stopped", async () => {
    const denied = () => {
      throw new DOMException("The storage is not available.", "SecurityError");
    };
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(denied);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(denied);
    vi.spyOn(Storage.prototype, "removeItem").mockImplementation(denied);
    onTestFinished(() => {
      vi.restoreAllMocks();
    });
    const user = setupUser();
    fakeImports([completedXmlImport]);
    const uploads = fakeUploads();
    const { release } = uploads.holdAt(8 * MiB);
    await openUploadPage();
    await user.upload(getFilePicker(), exportFile(12 * MiB));
    await screen.findByText("Uploading export.zip: 67%");
    uploads.fail("network");
    release();
    // Past every retry of tus.
    await elapse(30_000);
    await screen.findByRole("alert");

    uploads.recover();
    await user.click(screen.getByRole("button", { name: "Try again" }));

    expect(await findImportSection()).toBeInTheDocument();
    expect(uploads.creations).toHaveLength(1);
    expect(uploads.patches[1]).toMatchObject({
      offset: 8 * MiB,
      size: 4 * MiB,
    });
  });
});
