import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Import } from "@/api/generated";
import {
  completedImport,
  fakeImports,
  queuedImport,
  runningImport,
  zeroCounters,
} from "@/test/imports";
import { renderRoute } from "@/test/render";
import { fakeAuth, member, server } from "@/test/server";
import { fakeTokens } from "@/test/tokens";

/** The text of the banner of `runningImport`. */
const runningText = "An import is running. 5,000 records received so far.";

/** A running import that has not received a record yet. */
const startingImport: Import = {
  ...runningImport,
  report: { total: zeroCounters, types: {}, workouts: {}, errors: [] },
};

/** Opens `path` for `member`. */
function openPage(path: string) {
  server.use(...fakeAuth({ user: member, signedIn: true }));
  return renderRoute(path);
}

/** The banner of the running import, once it shows. */
function findBanner() {
  return screen.findByRole("region", { name: "Import in progress" });
}

/** The banner of the running import, or null while it does not show. */
function queryBanner() {
  return screen.queryByRole("region", { name: "Import in progress" });
}

/** Lets `ms` of time pass for the timers of the page and its requests. */
async function elapse(ms: number) {
  await act(() => vi.advanceTimersByTimeAsync(ms));
}

beforeEach(() => {
  // The timers of the polling are fake, and advance on their own with real
  // time, so that the requests and the queries of Testing Library complete.
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

describe("import banner", () => {
  it.each(["/", "/settings/tokens"])(
    "given a running import of the user, when the page %s opens, then a banner shows the received count and links to the imports page",
    async (path) => {
      fakeImports([runningImport, completedImport]);
      fakeTokens();
      openPage(path);

      const banner = await findBanner();
      expect(banner).toHaveTextContent(runningText);
      expect(
        within(banner).getByRole("link", { name: "View imports" }),
      ).toHaveAttribute("href", "/imports");
    },
  );

  it("given no running import, when one starts, then a live region that stays on the page announces it, without the count that changes at every poll", async () => {
    const { replace } = fakeImports([]);
    openPage("/");
    await screen.findByRole("heading", { level: 1 });
    await elapse(1000);
    const status = screen.getByRole("status");
    expect(status).toBeEmptyDOMElement();

    replace([runningImport]);
    await elapse(60_500);
    await findBanner();

    expect(screen.getByRole("status")).toBe(status);
    expect(status).toHaveTextContent(/^An import is running\.$/);
  });

  it("given a running import without a received record yet, then the banner shows without a count", async () => {
    fakeImports([startingImport]);
    openPage("/");

    expect(await findBanner()).toHaveTextContent(
      /^An import is running\.View imports$/,
    );
  });

  it("given no running import, only a queued and a completed one, then no banner shows", async () => {
    fakeImports([queuedImport, completedImport]);
    openPage("/");

    await screen.findByRole("heading", { level: 1 });
    await elapse(1000);
    expect(queryBanner()).not.toBeInTheDocument();
  });

  it("given a running import, when it completes, then the banner disappears at the next poll, 5 seconds later", async () => {
    const { replace } = fakeImports([runningImport]);
    openPage("/");
    await findBanner();

    replace([{ ...runningImport, status: "completed" }]);
    await elapse(4000);
    expect(queryBanner()).toBeInTheDocument();
    await elapse(1500);

    expect(queryBanner()).not.toBeInTheDocument();
  });

  it("given a running import, when its count grows, then the banner shows the new count at the next poll", async () => {
    const { replace } = fakeImports([runningImport]);
    openPage("/");
    expect(await findBanner()).toHaveTextContent(runningText);

    replace([
      {
        ...runningImport,
        report: {
          ...runningImport.report,
          total: { ...zeroCounters, received: 10000, inserted: 10000 },
        },
      },
    ]);
    await elapse(5500);

    expect(await findBanner()).toHaveTextContent(
      "An import is running. 10,000 records received so far.",
    );
  });

  it("given no running import, when one starts, then the banner shows at the next poll, a minute later", async () => {
    const { replace } = fakeImports([]);
    openPage("/");
    await screen.findByRole("heading", { level: 1 });
    await elapse(1000);

    replace([runningImport]);
    await elapse(10_000);
    expect(queryBanner()).not.toBeInTheDocument();
    await elapse(50_000);

    expect(await findBanner()).toHaveTextContent(runningText);
  });

  it("given the banner, when the user follows its link with the keyboard, then the imports page opens", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    fakeImports([runningImport]);
    openPage("/");
    const link = within(await findBanner()).getByRole("link", {
      name: "View imports",
    });

    link.focus();
    await user.keyboard("{Enter}");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Imports" }),
    ).toBeInTheDocument();
  });
});
