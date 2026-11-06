import { createBrowserHistory } from "@tanstack/react-router";
import { afterEach, describe, expect, it } from "vitest";

import { createFragmentSecretReader } from "@/lib/fragment-secret";

/** Opens `url` in the browser, as when the user follows a link. */
function openInBrowser(url: string) {
  window.history.replaceState(null, "", url);
  const history = createBrowserHistory();
  histories.push(history);
  return history;
}

const histories: { destroy: () => void }[] = [];

afterEach(() => {
  // The browser history patches window.history until it is destroyed.
  for (const history of histories.splice(0)) {
    history.destroy();
  }
  window.history.replaceState(null, "", "/");
});

describe("createFragmentSecretReader", () => {
  it("given a link with a secret in its fragment, when the secret is read, then it is returned and the fragment has left the address bar before the read returns", () => {
    const readSecret = createFragmentSecretReader(
      openInBrowser("/setup#s3cr3t"),
    );
    const entries = window.history.length;

    const secret = readSecret();
    const addressBar = window.location.href;

    expect(secret).toBe("s3cr3t");
    expect(addressBar).toBe("http://localhost:3000/setup");
    // The fragment is replaced, not pushed: no history entry keeps it.
    expect(window.history.length).toBe(entries);
  });

  it("given a secret already read, when the secret is read again on the same page, then the second read returns it too", () => {
    const readSecret = createFragmentSecretReader(
      openInBrowser("/setup#s3cr3t"),
    );

    readSecret();

    expect(readSecret()).toBe("s3cr3t");
    expect(window.location.href).toBe("http://localhost:3000/setup");
  });

  it("given a secret read on one page, when the secret is read on another page, then there is none", () => {
    const history = openInBrowser("/setup#s3cr3t");
    const readSecret = createFragmentSecretReader(history);
    readSecret();

    history.push("/invite");

    expect(readSecret()).toBeUndefined();
  });

  it("given a link with a query string and a secret, when the secret is read, then only the fragment leaves the address bar", () => {
    const readSecret = createFragmentSecretReader(
      openInBrowser("/reset-password?lang=fr#s3cr3t"),
    );

    expect(readSecret()).toBe("s3cr3t");
    expect(window.location.href).toBe(
      "http://localhost:3000/reset-password?lang=fr",
    );
  });

  it.each(["/setup", "/setup#"])(
    "given %s, when the secret is read, then there is none",
    (url) => {
      const readSecret = createFragmentSecretReader(openInBrowser(url));

      expect(readSecret()).toBeUndefined();
    },
  );
});
