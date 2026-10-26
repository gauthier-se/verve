import type { PostProcessorModule } from "i18next";

const plainLetters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ";
const accentedLetters = "áƀçďéƒĝĥíĵķĺḿñóṕʠŕšţúṽŵẋýžÁƁÇĎÉƑĜĤÍĴĶĹḾÑÓṔǪŔŠŢÚṼŴẊÝŽ";

const accented = new Map(
  Array.from(accentedLetters, (letter, index) => [plainLetters[index], letter]),
);

/** Share of the source length added as padding, like long German or French text. */
const expansion = 0.4;

/**
 * Accents every letter, pads the text by about 40 percent and wraps it in
 * brackets, so untranslated strings, truncation and layout overflow stand out.
 */
export function pseudoLocalize(text: string): string {
  const letters = Array.from(text, (char) => accented.get(char) ?? char).join(
    "",
  );
  return `[${letters}${"~".repeat(Math.round(text.length * expansion))}]`;
}

/** i18next post-processor applying {@link pseudoLocalize} to every translation. */
export const pseudoPostProcessor: PostProcessorModule = {
  type: "postProcessor",
  name: "pseudo",
  process: (value: string) => pseudoLocalize(value),
};
