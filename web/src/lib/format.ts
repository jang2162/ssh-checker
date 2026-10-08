import { lang, locale, t as tr, tn } from "../i18n/index.ts";

/** serial 앞 8자. 표에서는 이것만 보이고, 전체는 title로 보여 준다. */
export function shortSerial(serial: string): string {
  return serial.slice(0, 8);
}

function parts(d: Date, timeZone?: string): Record<string, string> {
  const fmt = new Intl.DateTimeFormat("en-CA", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
    timeZone,
  });
  return Object.fromEntries(fmt.formatToParts(d).map((p) => [p.type, p.value]));
}

function parse(iso: string | null | undefined): Date | null {
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d;
}

/**
 * 시각(브라우저 시간대). 한국어는 "2026-10-07 21:00", 영어는 "Oct 7, 2026, 21:00"(24시간제).
 * 값이 없거나 잘못되면 "-".
 */
export function formatDateTime(iso: string | null | undefined, timeZone?: string): string {
  const d = parse(iso);
  if (!d) return "-";
  if (lang() === "en") {
    return new Intl.DateTimeFormat(locale("en"), {
      year: "numeric",
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
      timeZone,
    }).format(d);
  }
  const p = parts(d, timeZone);
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}`;
}

/** 날짜. 한국어는 "2026-10-07", 영어는 "Oct 7, 2026". 값이 없거나 잘못되면 "-". */
export function formatDate(iso: string | null | undefined, timeZone?: string): string {
  const d = parse(iso);
  if (!d) return "-";
  if (lang() === "en") {
    return new Intl.DateTimeFormat(locale("en"), { year: "numeric", month: "short", day: "numeric", timeZone }).format(d);
  }
  const p = parts(d, timeZone);
  return `${p.year}-${p.month}-${p.day}`;
}

/** "3시간 전"/"3 hours ago", "2일 후"/"in 2 days". 1분 미만은 "방금 전"/"just now"(과거) 또는 "곧"/"soon"(미래). */
export function relativeTime(iso: string | null | undefined, now: number): string {
  if (!iso) return "-";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "-";
  const diff = now - t;
  const past = diff >= 0;
  const sec = Math.abs(diff) / 1000;
  if (sec < 60) return tr(past ? "time.justNow" : "time.soon");
  const min = Math.floor(sec / 60);
  if (min < 60) return tn(past ? "time.minutesAgo" : "time.minutesLater", min);
  const hour = Math.floor(min / 60);
  if (hour < 24) return tn(past ? "time.hoursAgo" : "time.hoursLater", hour);
  return tn(past ? "time.daysAgo" : "time.daysLater", Math.floor(hour / 24));
}

/** 만료까지 남은 일수 문구. "13일 남음"/"13 days left", "오늘 만료"/"expires today", "3일 지남"/"expired 3 days ago". */
export function daysLeftText(days: number | null | undefined): string {
  if (days === null || days === undefined) return "-";
  if (days < 0) return tn("days.over", -days);
  if (days === 0) return tr("days.today");
  return tn("days.left", days);
}

/** 만료 시각까지 남은 일수(내림). 시각이 없으면 null. */
export function daysUntil(iso: string | null | undefined, now: number): number | null {
  if (!iso) return null;
  const t = Date.parse(iso);
  return Number.isNaN(t) ? null : Math.floor((t - now) / 86_400_000);
}
