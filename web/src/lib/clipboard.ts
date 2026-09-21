// Copying a Ledger table out (ADR 0021). TSV — tab-separated, dot decimals — is the
// format that pastes into columns cleanly across Sheets, Excel, and Notion without a
// delimiter dialog, and dot decimals avoid the comma-decimal / comma-separator clash.
import { toDisplayValue } from "./metrics";

/** tsvNumber renders a number for copy-out: a plain dot decimal, never grouped or
 *  comma-decimal, so a spreadsheet reads it as a number. */
export function tsvNumber(value: number): string {
  return String(value);
}

/** tsvFigure renders a Metric's stored figure for copy-out as the screen shows it: a
 *  percent Metric's fraction in 0–100 (96.9, not 0.969). Copy is the table in front of
 *  you, and its column says "%"; the CSV download stays the canonical stored value. */
export function tsvFigure(value: number, unit: string): string {
  return tsvNumber(toDisplayValue(unit, value));
}

/** copyTsv joins a header row and body rows with tabs and newlines and writes them to
 *  the clipboard. Cells are pre-stringified by the caller (numbers via tsvNumber). */
export async function copyTsv(headers: string[], rows: string[][]): Promise<void> {
  const lines = [headers, ...rows].map((cells) => cells.join("\t"));
  await navigator.clipboard.writeText(lines.join("\n"));
}
