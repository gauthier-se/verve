import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { completedXmlImport, fakeImports } from "@/test/imports";
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
});
