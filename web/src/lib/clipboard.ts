// Copying a Ledger table out (ADR 0021). TSV — tab-separated, dot decimals — is the
// format that pastes into columns cleanly across Sheets, Excel, and Notion without a
// delimiter dialog, and dot decimals avoid the comma-decimal / comma-separator clash.

/** tsvNumber renders a number for copy-out: a plain dot decimal, never grouped or
 *  comma-decimal, so a spreadsheet reads it as a number. */
export function tsvNumber(value: number): string {
  return String(value);
}

/** copyTsv joins a header row and body rows with tabs and newlines and writes them to
 *  the clipboard. Cells are pre-stringified by the caller (numbers via tsvNumber). */
export async function copyTsv(headers: string[], rows: string[][]): Promise<void> {
  const lines = [headers, ...rows].map((cells) => cells.join("\t"));
  await navigator.clipboard.writeText(lines.join("\n"));
}
