import assert from "node:assert/strict";
import { test } from "node:test";
import { ApiError } from "../api/client.ts";
import { setLanguage } from "../i18n/index.ts";
import { errorMessage, KNOWN_ERROR_CODES } from "./errors.ts";

// 서버(internal/adminapi)가 내는 오류 코드 전부. 새 코드를 만들면 여기와 errors.ts에 함께 더한다.
const SERVER_CODES = [
  "unauthorized", "too_many_attempts", "unsupported_media_type", "malformed_json", "method_not_allowed",
  "invalid_status", "invalid_limit", "invalid_before_id", "not_found", "db_error", "invalid_domain",
  "invalid_memo", "domain_exists", "invalid_target", "target_exists", "acme_enabled", "acme_disabled",
  "invalid_allowfrom", "dns_provider_error",
];

test("every server error code has a message", () => {
  for (const code of SERVER_CODES) {
    assert.ok(KNOWN_ERROR_CODES.includes(code), `${code} has no message`);
    assert.ok(errorMessage(new ApiError(400, code)).length > 0);
  }
});

test("an unknown code shows the code itself", () => {
  setLanguage("ko");
  assert.equal(errorMessage(new ApiError(418, "teapot")), "오류가 발생했습니다 (teapot)");
  setLanguage("en");
  assert.equal(errorMessage(new ApiError(418, "teapot")), "An error occurred (teapot)");
});

test("a non-ApiError becomes a generic message", () => {
  setLanguage("ko");
  assert.equal(errorMessage(new Error("boom")), "알 수 없는 오류입니다.");
  assert.equal(errorMessage("x"), "알 수 없는 오류입니다.");
  setLanguage("en");
  assert.equal(errorMessage(new Error("boom")), "An unknown error occurred.");
});

test("network failures are readable", () => {
  setLanguage("ko");
  assert.match(errorMessage(new ApiError(0, "network_error")), /연결/);
  assert.equal(errorMessage(new ApiError(409, "domain_exists")), "이미 등록된 도메인입니다.");
  setLanguage("en");
  assert.match(errorMessage(new ApiError(0, "network_error")), /connect/);
  assert.equal(errorMessage(new ApiError(409, "domain_exists")), "This domain is already registered.");
});
