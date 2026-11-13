import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { initI18n } from "@/i18n";
import {
  completedImport,
  failedImport,
  fakeImports,
  importsPath,
  queuedImport,
  runningImport,
} from "@/test/imports";
import { problemResponse } from "@/test/problem";
import { renderRoute } from "@/test/render";
import { fakeAuth, member, server } from "@/test/server";

/** The current time of every test: the fixtures date from shortly before. */
const now = new Date("2026-11-11T12:00:00Z");

/** The creation of `completedImport`, in the timezone of `member`. */
const completedDate = "Nov 10, 2026, 9:00 AM";

/** The creation of `queuedImport`, in the timezone of `member`. */
const queuedDate = "Nov 9, 2026, 8:30 PM";

/** The creation of `failedImport`, in the timezone of `member`. */
const failedDate = "Nov 8, 2026, 8:00 AM";

/**
 * `text` with each run of white space, such as the narrow no-break space of
 * a formatted time or number, as one space.
 */
function normalize(text: string | null) {
  return (text ?? "").replace(/\s+/g, " ");
}

/** Matches an accessible name equal to `expected` once normalized. */
function named(expected: string) {
  return (name: string) => normalize(name) === expected;
}

/** Opens the imports page for `member`, once it shows its heading. */
async function openImportsPage() {
  server.use(...fakeAuth({ user: member, signedIn: true }));
  const rendered = renderRoute("/imports");
  await screen.findByRole("heading", { level: 1, name: "Imports" });
  return rendered;
}

/** The rows of the table of imports, without its header row. */
async function findImportRows() {
  const table = await screen.findByRole("table", { name: "Imports" });
  return within(table).getAllByRole("row").slice(1);
}

/** The texts of the cells of `row`, in the order of the columns. */
function cellTexts(row: HTMLElement) {
  return within(row)
    .getAllByRole("cell")
    .map((cell) => normalize(cell.textContent));
}

/** Opens the drawer of the import created at `date`, with a click. */
async function openDrawer(user: UserEvent, date: string) {
  await user.click(
    await screen.findByRole("button", {
      name: named(`Details of the import of ${date}`),
    }),
  );
  return screen.findByRole("dialog", { name: named(`Import of ${date}`) });
}

/** The terms and definitions of the description list in `element`. */
function descriptions(element: HTMLElement) {
  const terms = within(element).getAllByRole("term");
  const definitions = within(element).getAllByRole("definition");
  return terms.map((term, i) => [
    normalize(term.textContent),
    normalize(definitions[i]?.textContent ?? null),
  ]);
}

/** Moves the focus with Tab until it reaches `target`. */
async function tabTo(user: UserEvent, target: HTMLElement) {
  for (let i = 0; i < 60 && document.activeElement !== target; i++) {
    await user.tab();
  }
  expect(target).toHaveFocus();
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(now);
});

afterEach(() => {
  vi.useRealTimers();
});

describe("imports page", () => {
  it("given the navigation, when the user chooses Imports, then the imports page opens", async () => {
    const user = userEvent.setup();
    fakeImports([completedImport]);
    server.use(...fakeAuth({ user: member, signedIn: true }));
    renderRoute("/");

    const nav = await screen.findByRole("navigation", { name: "Main" });
    await user.click(within(nav).getByRole("link", { name: "Imports" }));

    expect(
      await screen.findByRole("heading", { level: 1, name: "Imports" }),
    ).toBeInTheDocument();
  });

  describe("table", () => {
    it("given imports, then a table shows the origin, client, date, duration, status and counters of each, newest first", async () => {
      fakeImports([completedImport, queuedImport, failedImport]);
      await openImportsPage();

      const table = await screen.findByRole("table", { name: "Imports" });
      expect(
        within(table)
          .getAllByRole("columnheader")
          .map((header) => header.textContent),
      ).toEqual([
        "Origin",
        "Client",
        "Date",
        "Duration",
        "Status",
        "Received",
        "Added",
        "Updated",
        "Duplicates",
        "Invalid",
        "Actions",
      ]);
      const [completed, queued, failed] = await findImportRows();
      expect(completed && cellTexts(completed)).toEqual([
        "Native format",
        "Verve for iOS 1.0.0via iPhone",
        completedDate,
        "2m 5s",
        "Completed",
        "1,201",
        "1,180",
        "0",
        "18",
        "2",
        "Details",
      ]);
      expect(queued && cellTexts(queued)).toEqual([
        "Apple Health export",
        "None",
        queuedDate,
        "Not started",
        "Queued",
        "0",
        "0",
        "0",
        "0",
        "0",
        "Details",
      ]);
      expect(failed && cellTexts(failed)).toEqual([
        "Health Auto Export",
        "via Health Auto Export",
        failedDate,
        "2s",
        "Failed",
        "0",
        "0",
        "0",
        "0",
        "0",
        "Details",
      ]);
    });

    it("given a running import, then its duration says that it is in progress", async () => {
      fakeImports([runningImport]);
      await openImportsPage();

      const [row] = await findImportRows();
      expect(row && cellTexts(row).slice(3, 5)).toEqual([
        "In progress",
        "Running",
      ]);
    });

    it("given more imports than a page, when the user asks for more, then the next page is added below", async () => {
      const user = userEvent.setup();
      fakeImports([completedImport, queuedImport, failedImport], {
        pageSize: 2,
      });
      await openImportsPage();

      expect(await findImportRows()).toHaveLength(2);
      await user.click(screen.getByRole("button", { name: "Show more" }));

      await waitFor(async () => {
        expect(await findImportRows()).toHaveLength(3);
      });
      const rows = await findImportRows();
      expect(rows.map((row) => cellTexts(row)[2])).toEqual([
        completedDate,
        queuedDate,
        failedDate,
      ]);
      expect(
        screen.queryByRole("button", { name: "Show more" }),
      ).not.toBeInTheDocument();
    });

    it("given the imports cannot be loaded, then the page says so", async () => {
      server.use(
        http.get(importsPath, () =>
          problemResponse({
            title: "Internal Server Error",
            status: 500,
            code: "internal",
          }),
        ),
      );
      await openImportsPage();

      expect(await screen.findByRole("alert")).toHaveTextContent(
        "Something went wrong. Try again.",
      );
    });
  });

  describe("empty state", () => {
    it("given no import, then the page says how to send data and links to the API tokens page", async () => {
      fakeImports();
      await openImportsPage();

      const empty = await screen.findByRole("region", {
        name: "No import yet",
      });
      expect(
        within(empty).getByRole("link", { name: "Create an API token" }),
      ).toHaveAttribute("href", "/settings/tokens");
      expect(screen.queryByRole("table")).not.toBeInTheDocument();
    });
  });

  describe("drawer", () => {
    it("given an import, when the user opens it, then the drawer shows its total counters", async () => {
      const user = userEvent.setup();
      fakeImports([completedImport]);
      await openImportsPage();

      const drawer = await openDrawer(user, completedDate);

      const totals = await within(drawer).findByRole("region", {
        name: "Totals",
      });
      expect(descriptions(totals)).toEqual([
        ["Received", "1,201"],
        ["Added", "1,180"],
        ["Updated", "0"],
        ["Duplicates", "18"],
        ["Blocked", "0"],
        ["Invalid", "2"],
        ["Unsupported", "1"],
        ["Suspicious", "0"],
        ["Warnings", "0"],
        ["Deleted", "0"],
        ["Not found", "0"],
      ]);
    });

    it("given an import, when the user opens it, then the drawer shows the counters of each type by its translated name, or its identifier for an uncurated or unsupported type, and the workouts last", async () => {
      const user = userEvent.setup();
      fakeImports([completedImport]);
      await openImportsPage();

      const drawer = await openDrawer(user, completedDate);

      const byType = await within(drawer).findByRole("region", {
        name: "By type",
      });
      const items = within(byType).getAllByRole("listitem");
      expect(
        items.map((item) => within(item).getByRole("heading").textContent),
      ).toEqual([
        "Steps",
        "HKQuantityTypeIdentifierUVExposure",
        "HKDataTypeIdentifierElectrocardiogram",
        "Workouts",
      ]);
      const [steps, uv, ecg, workouts] = items;
      expect(steps && descriptions(steps)).toEqual([
        ["Received", "1,100"],
        ["Added", "1,082"],
        ["Duplicates", "18"],
      ]);
      expect(uv && descriptions(uv)).toEqual([
        ["Received", "99"],
        ["Added", "97"],
        ["Invalid", "2"],
      ]);
      expect(ecg && descriptions(ecg)).toEqual([
        ["Received", "1"],
        ["Unsupported", "1"],
      ]);
      expect(workouts && descriptions(workouts)).toEqual([
        ["Received", "1"],
        ["Added", "1"],
      ]);
    });

    it("given an import with sample errors, when the user opens it, then the drawer lists them", async () => {
      const user = userEvent.setup();
      fakeImports([failedImport]);
      await openImportsPage();

      const drawer = await openDrawer(user, failedDate);

      const errors = await within(drawer).findByRole("region", {
        name: "Sample errors",
      });
      expect(
        within(errors)
          .getAllByRole("listitem")
          .map((item) => item.textContent),
      ).toEqual(["the payload is not valid JSON"]);
    });

    it("given an import without sample errors or types, when the user opens it, then the drawer says so", async () => {
      const user = userEvent.setup();
      fakeImports([queuedImport]);
      await openImportsPage();

      const drawer = await openDrawer(user, queuedDate);

      expect(
        await within(
          await within(drawer).findByRole("region", { name: "By type" }),
        ).findByText("No record yet."),
      ).toBeInTheDocument();
      expect(
        within(
          within(drawer).getByRole("region", { name: "Sample errors" }),
        ).getByText("No error."),
      ).toBeInTheDocument();
    });

    it("given the drawer opened with the keyboard, when the user presses Escape, then it closes and the focus returns to its button", async () => {
      const user = userEvent.setup();
      fakeImports([completedImport]);
      await openImportsPage();
      const details = await screen.findByRole("button", {
        name: named(`Details of the import of ${completedDate}`),
      });

      await tabTo(user, details);
      await user.keyboard("{Enter}");
      expect(
        await screen.findByRole("dialog", {
          name: named(`Import of ${completedDate}`),
        }),
      ).toBeInTheDocument();
      await user.keyboard("{Escape}");

      await waitFor(() => {
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      });
      expect(details).toHaveFocus();
    });

    it("given the drawer, when the user chooses Close, then it closes", async () => {
      const user = userEvent.setup();
      fakeImports([completedImport]);
      await openImportsPage();
      const drawer = await openDrawer(user, completedDate);

      await user.click(within(drawer).getByRole("button", { name: "Close" }));

      await waitFor(() => {
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      });
    });
  });

  describe("cancellation", () => {
    it("given a queued import, when the user cancels it and confirms, then it is cancelled and its row says so", async () => {
      const user = userEvent.setup();
      const { cancelled } = fakeImports([queuedImport]);
      await openImportsPage();
      const drawer = await openDrawer(user, queuedDate);

      await user.click(
        await within(drawer).findByRole("button", { name: "Cancel import" }),
      );
      const confirmation = await screen.findByRole("alertdialog", {
        name: "Cancel this import?",
      });
      expect(confirmation).toHaveAccessibleDescription(
        "Verve stops the import. The data that it already stored stays.",
      );
      await user.click(
        within(confirmation).getByRole("button", { name: "Cancel import" }),
      );

      await waitFor(() => {
        expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
      });
      expect(cancelled).toEqual([queuedImport.id]);
      await waitFor(() => {
        expect(drawer).toHaveAccessibleDescription(
          "Apple Health export, Cancelled",
        );
      });
      expect(
        within(drawer).queryByRole("button", { name: "Cancel import" }),
      ).not.toBeInTheDocument();
      // The table is outside the accessibility tree while the drawer is open.
      await user.keyboard("{Escape}");
      const [row] = await findImportRows();
      expect(row && cellTexts(row)[4]).toBe("Cancelled");
    });

    it("given the confirmation, when the user keeps the import, then nothing is cancelled", async () => {
      const user = userEvent.setup();
      const { cancelled } = fakeImports([queuedImport]);
      await openImportsPage();
      const drawer = await openDrawer(user, queuedDate);

      await user.click(
        await within(drawer).findByRole("button", { name: "Cancel import" }),
      );
      const confirmation = await screen.findByRole("alertdialog");
      await user.click(
        within(confirmation).getByRole("button", { name: "Keep import" }),
      );

      await waitFor(() => {
        expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
      });
      expect(cancelled).toEqual([]);
      await user.keyboard("{Escape}");
      const [row] = await findImportRows();
      expect(row && cellTexts(row)[4]).toBe("Queued");
    });

    it("given a finished import, when the user opens it, then the drawer offers no cancellation", async () => {
      const user = userEvent.setup();
      fakeImports([completedImport]);
      await openImportsPage();

      const drawer = await openDrawer(user, completedDate);

      await within(drawer).findByRole("region", { name: "Totals" });
      expect(
        within(drawer).queryByRole("button", { name: "Cancel import" }),
      ).not.toBeInTheDocument();
    });

    it("given an import that ended meanwhile, when the user cancels it, then the confirmation says so and stays open", async () => {
      const user = userEvent.setup();
      fakeImports([queuedImport]);
      server.use(
        http.post(`${importsPath}/:id/cancel`, () =>
          problemResponse({
            title: "Conflict",
            status: 409,
            code: "import_finished",
          }),
        ),
      );
      await openImportsPage();
      const drawer = await openDrawer(user, queuedDate);

      await user.click(
        await within(drawer).findByRole("button", { name: "Cancel import" }),
      );
      const confirmation = await screen.findByRole("alertdialog");
      await user.click(
        within(confirmation).getByRole("button", { name: "Cancel import" }),
      );

      expect(await within(confirmation).findByRole("alert")).toHaveTextContent(
        "This import already ended.",
      );
      expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    });
  });

  describe("translation", () => {
    it("given the app in French, then the page, its table and its drawer are translated", async () => {
      const user = userEvent.setup();
      const i18n = await initI18n();
      await i18n.changeLanguage("fr");
      fakeImports([completedImport]);
      server.use(...fakeAuth({ user: member, signedIn: true }));
      renderRoute("/imports");

      await screen.findByRole("heading", { level: 1, name: "Importations" });
      const table = await screen.findByRole("table", { name: "Importations" });
      expect(
        within(table)
          .getAllByRole("columnheader")
          .map((header) => header.textContent),
      ).toEqual([
        "Origine",
        "Client",
        "Date",
        "Durée",
        "Statut",
        "Reçus",
        "Ajoutés",
        "Mis à jour",
        "Doublons",
        "Invalides",
        "Actions",
      ]);
      const [, row] = within(table).getAllByRole("row");
      expect(row && cellTexts(row)).toEqual([
        "Format natif",
        "Verve for iOS 1.0.0via iPhone",
        "10 nov. 2026, 09:00",
        "2 min 5 s",
        "Terminée",
        "1 201",
        "1 180",
        "0",
        "18",
        "2",
        "Détails",
      ]);

      await user.click(
        screen.getByRole("button", {
          name: named("Détails de l'importation du 10 nov. 2026, 09:00"),
        }),
      );
      const drawer = await screen.findByRole("dialog", {
        name: named("Importation du 10 nov. 2026, 09:00"),
      });
      const byType = await within(drawer).findByRole("region", {
        name: "Par type",
      });
      expect(
        within(byType)
          .getAllByRole("listitem")
          .map((item) => within(item).getByRole("heading").textContent),
      ).toEqual([
        "Pas",
        "HKQuantityTypeIdentifierUVExposure",
        "HKDataTypeIdentifierElectrocardiogram",
        "Entraînements",
      ]);
    });

    it("given the app in French and no import, then the empty state is translated", async () => {
      const i18n = await initI18n();
      await i18n.changeLanguage("fr");
      fakeImports();
      server.use(...fakeAuth({ user: member, signedIn: true }));
      renderRoute("/imports");

      const empty = await screen.findByRole("region", {
        name: "Aucune importation",
      });
      expect(
        within(empty).getByRole("link", { name: "Créer un jeton d'API" }),
      ).toBeInTheDocument();
    });
  });
});
