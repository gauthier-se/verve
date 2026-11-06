/** The timezone of the browser, as an IANA name such as Europe/Paris. */
export function browserTimezone(): string {
  return new Intl.DateTimeFormat().resolvedOptions().timeZone;
}
