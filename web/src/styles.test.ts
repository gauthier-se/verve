import { describe, expect, it } from "vitest";

import styles from "@/styles.css?inline";

const darkMedia = "@media (prefers-color-scheme: dark)";

/**
 * Splits `css` into the bodies of its `@media (prefers-color-scheme: dark)`
 * blocks and the rest of the stylesheet.
 */
function splitDarkMedia(css: string) {
  const dark: string[] = [];
  let rest = "";
  let index = 0;
  for (;;) {
    const start = css.indexOf(darkMedia, index);
    if (start === -1) {
      break;
    }
    const open = css.indexOf("{", start);
    let depth = 0;
    let end = open;
    for (; end < css.length; end++) {
      if (css[end] === "{") {
        depth++;
      } else if (css[end] === "}" && --depth === 0) {
        break;
      }
    }
    rest += css.slice(index, start);
    dark.push(css.slice(open + 1, end));
    index = end + 1;
  }
  rest += css.slice(index);
  return { dark: dark.join("\n"), rest };
}

describe("stylesheet", () => {
  it("given the system prefers a dark color scheme, then the dark theme colors apply", () => {
    const { dark, rest } = splitDarkMedia(styles);

    expect(dark).toMatch(/--background:\s*oklch\(0\.145 0 0\)/);
    expect(dark).toMatch(/--foreground:\s*oklch\(0\.985 0 0\)/);
    expect(rest).toMatch(/--background:\s*oklch\(1 0 0\)/);
  });

  it("given the system prefers a dark color scheme, then the Tailwind dark variant applies through the media query, not a class", () => {
    const { dark, rest } = splitDarkMedia(styles);

    expect(dark).toMatch(/\.dark\\:/);
    expect(rest).not.toMatch(/\.dark\\:/);
    expect(styles).not.toMatch(/\.dark\b(?!\\:)/);
  });
});
