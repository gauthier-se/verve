import { expect, test } from "./fixtures";

// The pages that need a session check it with a request that answers 401
// without one, which the browser reports as a console error: the smoke tests
// stay on public pages, so that they pass whether setup is done or not.

test("given a public page, then the footer shows the version and the console has no error", async ({
  page,
  request,
}) => {
  const meta = await request.get("/api/v1/meta");
  const { version } = (await meta.json()) as { version: string };
  // A binary built without the version metadata reports "dev".
  expect(version).not.toBe("dev");

  await page.goto("/login");

  await expect(page.getByRole("contentinfo")).toHaveText(`Verve ${version}`);
});

test("given a direct load of a deep client route, then HTTP 200 and the SPA renders its not-found page", async ({
  page,
}) => {
  const response = await page.goto("/some/client/route");

  expect(response?.status()).toBe(200);
  await expect(
    page.getByRole("heading", { level: 1, name: "Page not found" }),
  ).toBeVisible();
});

test("given GET /api/v1/does-not-exist, then 404 JSON with code not_found", async ({
  request,
}) => {
  const response = await request.get("/api/v1/does-not-exist");

  expect(response.status()).toBe(404);
  expect(response.headers()["content-type"]).toBe("application/problem+json");
  expect(await response.json()).toMatchObject({ code: "not_found" });
});

for (const path of ["/healthz", "/readyz"]) {
  test(`given GET ${path}, then 200`, async ({ request }) => {
    const response = await request.get(path);

    expect(response.status()).toBe(200);
  });
}
