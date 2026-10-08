import { ApiError } from "../api/client.ts";
import { t, type MessageKey } from "../i18n/index.ts";

/** i18n의 error.<코드> 키에서 코드만 뽑는다. */
type MessageCode<K = MessageKey> = K extends `error.${infer C}` ? C : never;

/** 화면용 문구가 있는 서버 오류 코드. 코드 목록은 docs/guide/api.md의 오류 표와 같다. 문구는 i18n의 error.<코드>. */
const CODES = [
  "unauthorized", "too_many_attempts", "unsupported_media_type", "malformed_json", "method_not_allowed",
  "invalid_status", "invalid_limit", "invalid_before_id", "not_found", "db_error", "invalid_domain",
  "invalid_memo", "domain_exists", "invalid_target", "target_exists", "acme_enabled", "acme_disabled",
  "invalid_allowfrom", "dns_provider_error", "network_error", "unknown",
] as const satisfies readonly MessageCode[];

type Code = (typeof CODES)[number];

function isCode(c: string): c is Code {
  return (CODES as readonly string[]).includes(c);
}

export const KNOWN_ERROR_CODES: readonly string[] = CODES;

/** 화면에 보여 줄 오류 문구. 모르는 코드는 코드를 그대로 붙여 원인을 찾을 수 있게 한다. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return isCode(e.code) ? t(`error.${e.code}`) : t("error.other", { code: e.code });
  return t("error.unknown");
}
