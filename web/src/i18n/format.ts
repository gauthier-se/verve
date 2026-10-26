import { getI18n } from "react-i18next";

/** Formats a number with `Intl.NumberFormat` for `lng`. */
export function formatNumber(
  value: number,
  lng: string,
  options?: Intl.NumberFormatOptions,
): string {
  return new Intl.NumberFormat(lng, options).format(value);
}

/** Formats a date with `Intl.DateTimeFormat` for `lng`. */
export function formatDate(
  date: Date,
  lng: string,
  options?: Intl.DateTimeFormatOptions,
): string {
  return new Intl.DateTimeFormat(lng, options).format(date);
}

/**
 * Formats a duration in seconds with its two largest units, for example
 * "7h 32m" in English and "7 h 32 min" in French. Units come from translation
 * keys rather than `Intl.DurationFormat` so the output is the same in every
 * browser. Zero units are left out, and seconds are dropped once there are hours.
 */
export function formatDuration(seconds: number, lng: string): string {
  const t = getI18n().getFixedT(lng);
  const total = Math.max(0, Math.floor(seconds));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;

  if (h > 0) {
    return m > 0 ? t("duration.hm", { h, m }) : t("duration.h", { h });
  }
  if (m > 0) {
    return s > 0 ? t("duration.ms", { m, s }) : t("duration.m", { m });
  }
  return t("duration.s", { s });
}
