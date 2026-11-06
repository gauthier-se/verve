import type { RouterHistory } from "@tanstack/react-router";

/**
 * Returns a function that reads the secret that a link carries in its URL
 * fragment, such as the setup link (ADR 0010). The first read takes the secret
 * from the fragment and removes the fragment from the address bar before it
 * returns, by replacing the history entry: no request leaves the page while
 * the fragment is there, and no history entry keeps it. The secret stays in
 * the memory of this function only. A later read on the same page returns it
 * again, since a route can run twice, for example once the fragment is gone.
 * A read on another page returns nothing and forgets it: a link token may be
 * used once, so coming back to the page does not bring it back.
 */
export function createFragmentSecretReader(
  history: RouterHistory,
): () => string | undefined {
  let captured: { pathname: string; secret: string } | undefined;

  return () => {
    const { pathname, search, hash, state } = history.location;
    if (hash !== "") {
      captured = { pathname, secret: hash.slice(1) };
      history.replace(`${pathname}${search}`, state);
      // The browser history writes the address bar a microtask later.
      history.flush();
    } else if (captured?.pathname !== pathname) {
      captured = undefined;
    }
    return captured?.secret;
  };
}
