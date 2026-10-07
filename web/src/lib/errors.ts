import { ApiError } from "../api/client.ts";

/** 서버 오류 코드의 화면용 문구. 코드 목록은 docs/guide/api.md의 오류 표와 같다. */
const MESSAGES: Record<string, string> = {
  unauthorized: "관리자 로그인이 필요하거나 로그인이 만료됐습니다.",
  too_many_attempts: "시도가 너무 많습니다. 잠시 뒤 다시 시도하세요.",
  unsupported_media_type: "요청 형식이 올바르지 않습니다.",
  malformed_json: "요청 내용이 올바르지 않습니다.",
  method_not_allowed: "지원하지 않는 요청입니다.",
  invalid_status: "상태 값이 올바르지 않습니다.",
  invalid_limit: "목록 범위 값이 올바르지 않습니다.",
  invalid_before_id: "목록 범위 값이 올바르지 않습니다.",
  not_found: "찾을 수 없습니다. 이미 삭제됐을 수 있습니다.",
  db_error: "서버 데이터베이스 오류입니다. 잠시 뒤 다시 시도하세요.",
  invalid_domain: "도메인 형식이 올바르지 않습니다. 와일드카드 없이 a.com 형태로 입력하세요.",
  invalid_memo: "메모는 500자까지 쓸 수 있습니다.",
  domain_exists: "이미 등록된 도메인입니다.",
  invalid_target: "대상 값이 올바르지 않습니다. 호스트명, 포트(1~65535), SNI, IP(최대 16개)를 확인하세요.",
  target_exists: "같은 호스트명·포트·SNI의 대상이 이미 있습니다.",
  acme_enabled: "이미 ACME가 켜져 있습니다.",
  acme_disabled: "ACME가 꺼져 있습니다.",
  invalid_allowfrom: "허용 IP 형식이 올바르지 않습니다. IP나 CIDR을 최대 64개까지 입력하세요.",
  dns_provider_error: "DNS 공급자 호출에 실패했습니다. 서버의 DNS 설정과 /health를 확인하세요.",
  network_error: "서버에 연결할 수 없습니다.",
  unknown: "알 수 없는 오류입니다.",
};

export const KNOWN_ERROR_CODES: readonly string[] = Object.keys(MESSAGES);

/** 화면에 보여 줄 오류 문구. 모르는 코드는 코드를 그대로 붙여 원인을 찾을 수 있게 한다. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return MESSAGES[e.code] ?? `오류가 발생했습니다 (${e.code})`;
  return MESSAGES.unknown ?? "";
}
