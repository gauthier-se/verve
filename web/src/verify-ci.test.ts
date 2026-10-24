import { expect, test } from "vitest";

test("given a temporary failing test, when ci runs, then the web job fails", () => {
  expect(1).toBe(2);
});
