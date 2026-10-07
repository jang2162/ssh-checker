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

/** "2026-10-07 21:00" 형식(브라우저 시간대). 값이 없거나 잘못되면 "-". */
export function formatDateTime(iso: string | null | undefined, timeZone?: string): string {
  if (!iso) return "-";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "-";
  const p = parts(d, timeZone);
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}`;
}

/** "2026-10-07". 값이 없거나 잘못되면 "-". */
export function formatDate(iso: string | null | undefined, timeZone?: string): string {
  const s = formatDateTime(iso, timeZone);
  return s === "-" ? s : s.slice(0, 10);
}

/** "3시간 전", "2일 후". 1분 미만은 "방금 전"(과거) 또는 "곧"(미래). */
export function relativeTime(iso: string | null | undefined, now: number): string {
  if (!iso) return "-";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "-";
  const diff = now - t;
  const past = diff >= 0;
  const sec = Math.abs(diff) / 1000;
  if (sec < 60) return past ? "방금 전" : "곧";
  const suffix = past ? "전" : "후";
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}분 ${suffix}`;
  const hour = Math.floor(min / 60);
  if (hour < 24) return `${hour}시간 ${suffix}`;
  return `${Math.floor(hour / 24)}일 ${suffix}`;
}

/** 만료까지 남은 일수 문구. */
export function daysLeftText(days: number | null | undefined): string {
  if (days === null || days === undefined) return "-";
  if (days < 0) return `${-days}일 지남`;
  if (days === 0) return "오늘 만료";
  return `${days}일 남음`;
}

/** 만료 시각까지 남은 일수(내림). 시각이 없으면 null. */
export function daysUntil(iso: string | null | undefined, now: number): number | null {
  if (!iso) return null;
  const t = Date.parse(iso);
  return Number.isNaN(t) ? null : Math.floor((t - now) / 86_400_000);
}
