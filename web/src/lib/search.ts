// One search behaviour for every filter box in the interface.

/** fold prepares a string for matching: lowercased, stripped of accents, and with
 *  underscores read as spaces. A Catalog slug and its humanized label then fold to
 *  the same text, so a caller can match against the raw slug without humanizing it,
 *  and a note typed with accents is found by a query typed without them. */
function fold(s: string): string {
  return s
    .toLowerCase()
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .replace(/_/g, " ");
}

/** textMatcher builds the predicate behind every search box: the query is split on
 *  spaces and every term must appear in at least one of the fields it is handed.
 *
 *  Terms are ANDed rather than matched as one substring because the things being
 *  searched have a word order nobody remembers. "heart rest" and "rest heart" both
 *  have to find `resting_heart_rate`, or the box only helps the people who already
 *  knew the name. Which is also why a Metric's unit is worth passing as a field:
 *  "kcal" is how you ask for the energy Metrics when you cannot recall one of them.
 *
 *  An empty query matches everything, so a caller filters unconditionally rather
 *  than branching on whether the box is empty. */
export function textMatcher(query: string): (...fields: (string | null | undefined)[]) => boolean {
  const terms = fold(query).split(/\s+/).filter(Boolean);
  if (terms.length === 0) return () => true;
  return (...fields) => {
    const hay = fold(fields.filter(Boolean).join(" "));
    return terms.every((t) => hay.includes(t));
  };
}
