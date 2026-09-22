import { describe, expect, it } from "vitest";

import { coverageText, createBody, emptyDraft, pick, typeName } from "./new-dashboard";
import type { DashboardTemplate } from "./types";

const template = (slug: string, name: string): DashboardTemplate => ({
  slug,
  name,
  description: "",
  metrics: [],
  with_data: 0,
  panels: 1,
});
const sleep = template("sleep", "Sleep");
const cut = template("cut", "Cut");

describe("the draft of a new Dashboard", () => {
  it("starts empty, on no template", () => {
    expect(emptyDraft).toEqual({ template: null, name: "", typed: false });
  });

  it("takes the template's name when one is picked", () => {
    expect(pick(emptyDraft, sleep)).toEqual({ template: "sleep", name: "Sleep", typed: false });
  });

  it("follows the pick while the name was never typed", () => {
    expect(pick(pick(emptyDraft, sleep), cut).name).toBe("Cut");
  });

  it("clears a name it filled when going back to an empty grid", () => {
    expect(pick(pick(emptyDraft, sleep), null)).toEqual(emptyDraft);
  });

  it("keeps a typed name through every pick", () => {
    const typed = typeName(pick(emptyDraft, sleep), "Sleep, travel weeks");
    expect(pick(typed, cut)).toEqual({ template: "cut", name: "Sleep, travel weeks", typed: true });
    expect(pick(typed, null).name).toBe("Sleep, travel weeks");
  });

  it("hands the name back to the pick once it is cleared", () => {
    const cleared = typeName(typeName(emptyDraft, "Mine"), "");
    expect(pick(cleared, cut).name).toBe("Cut");
  });
});

describe("createBody", () => {
  it("sends the name alone for an empty grid", () => {
    expect(createBody({ template: null, name: " Training ", typed: true })).toEqual({ name: "Training" });
  });

  it("sends the template, and the name as given", () => {
    expect(createBody(pick(emptyDraft, sleep))).toEqual({ template: "sleep", name: "Sleep" });
  });
});

describe("coverageText", () => {
  it("states the count with its denominator", () => {
    expect(coverageText({ ...sleep, metrics: ["a", "b", "c", "d", "e"], with_data: 4 })).toBe(
      "4 of 5 metrics have data",
    );
  });

  it("says so plainly when none has", () => {
    expect(coverageText({ ...sleep, metrics: ["a", "b"], with_data: 0 })).toBe("No data yet for its 2 metrics");
  });

  it("agrees in number for one", () => {
    expect(coverageText({ ...sleep, metrics: ["a"], with_data: 1 })).toBe("1 of 1 metric has data");
  });
});
