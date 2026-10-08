import type { CnameStatus, Status } from "../api/types.ts";
import { t } from "../i18n/index.ts";

/** 상태의 화면 이름(현재 언어). */
export function statusLabel(s: Status): string {
  return t(`status.${s}`);
}

/** 나쁜 순서. 요약 카드와 필터가 이 순서로 나온다. */
export const STATUS_ORDER: readonly Status[] = ["critical", "warning", "info", "ok", "unknown"];

/** CNAME 확인 상태의 화면 이름(현재 언어). */
export function cnameLabel(s: CnameStatus): string {
  return t(`cname.${s}`);
}

/** 주소창의 ?status= 값을 상태로 바꾼다. 모르는 값이면 null(필터 없음). */
export function parseStatus(v: string | null): Status | null {
  return STATUS_ORDER.find((s) => s === v) ?? null;
}

/** Go의 time.Duration 문자열("6h0m0s", "5m0s", "30s")을 밀리초로 바꾼다. 모르는 형식이면 null. */
export function parseGoDuration(s: string): number | null {
  const m = /^(?:(\d+(?:\.\d+)?)h)?(?:(\d+(?:\.\d+)?)m)?(?:(\d+(?:\.\d+)?)s)?$/.exec(s.trim());
  if (!m || (m[1] === undefined && m[2] === undefined && m[3] === undefined)) return null;
  const [h, min, sec] = [m[1], m[2], m[3]].map((x) => (x === undefined ? 0 : Number(x)));
  return (((h ?? 0) * 60 + (min ?? 0)) * 60 + (sec ?? 0)) * 1000;
}

/** 마지막 점검이 점검 주기의 이 배수보다 오래되면 worker가 멈춘 것으로 본다. */
export const STALE_FACTOR = 2;

/**
 * 점검 결과가 오래됐는지 알려 준다. 상태는 점검할 때 저장되므로, worker가 멈추면 만료된 인증서도
 * 마지막 "정상"으로 계속 보인다. 그래서 화면이 시간으로 따로 알려야 한다.
 * 점검한 적이 없거나(미점검) 주기를 모르면 false다.
 */
export function isStale(lastCheckedAt: string | null, checkIntervalMs: number | null, now: number): boolean {
  if (!lastCheckedAt || !checkIntervalMs) return false;
  const t = Date.parse(lastCheckedAt);
  return Number.isFinite(t) && now - t > STALE_FACTOR * checkIntervalMs;
}

/** 점검 결과 중 가장 최근의 checked_at. 비었거나 모두 해석할 수 없으면 null. */
export function latestCheckedAt(results: readonly { checked_at: string }[]): string | null {
  let best: string | null = null;
  let bestT = -Infinity;
  for (const r of results) {
    const t = Date.parse(r.checked_at);
    if (Number.isFinite(t) && t > bestT) {
      bestT = t;
      best = r.checked_at;
    }
  }
  return best;
}

/**
 * 대시보드 정렬: 나쁜 상태가 먼저, 같은 상태에서는 만료가 가까운 것이 먼저, 그다음 도메인 이름순.
 * 만료일이 없는(인증서를 못 받은) 도메인은 같은 상태 안에서 뒤로 간다.
 */
export function sortByUrgency<T extends { status: Status; days_left: number | null; fqdn: string }>(items: readonly T[]): T[] {
  const rank = (s: Status) => STATUS_ORDER.indexOf(s);
  return [...items].sort(
    (a, b) =>
      rank(a.status) - rank(b.status) ||
      (a.days_left ?? Infinity) - (b.days_left ?? Infinity) || // 둘 다 없으면 NaN이라 다음 비교로 넘어간다
      a.fqdn.localeCompare(b.fqdn),
  );
}
