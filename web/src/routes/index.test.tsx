import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderRoute } from "@/test/render";

describe("index route", () => {
  it("given the index route, when it renders, then it shows Verve as the level 1 heading", async () => {
    renderRoute("/");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Verve" }),
    ).toBeInTheDocument();
  });
});
