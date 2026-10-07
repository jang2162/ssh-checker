import assert from "node:assert/strict";
import { test } from "node:test";
import { daysLeftText, daysUntil, formatDate, formatDateTime, relativeTime, shortSerial } from "./format.ts";

test("shortSerial keeps the first 8 characters", () => {
  assert.equal(shortSerial("04ab12cd34ef56"), "04ab12cd");
  assert.equal(shortSerial("ab"), "ab");
  assert.equal(shortSerial(""), "");
});

test("formatDateTime renders a fixed pattern in the given time zone", () => {
  assert.equal(formatDateTime("2026-10-07T12:34:56Z", "UTC"), "2026-10-07 12:34");
  assert.equal(formatDateTime("2026-10-07T12:34:56Z", "Asia/Seoul"), "2026-10-07 21:34");
  assert.equal(formatDateTime("2026-10-07T15:00:00Z", "Asia/Seoul"), "2026-10-08 00:00"); // 자정은 24시가 아니라 00시
});

test("formatDateTime and formatDate give - for missing or invalid values", () => {
  for (const v of [null, undefined, "", "garbage"]) {
    assert.equal(formatDateTime(v), "-");
    assert.equal(formatDate(v), "-");
  }
  assert.equal(formatDate("2026-10-07T12:34:56Z", "UTC"), "2026-10-07");
});

test("relativeTime counts in minutes, hours and days", () => {
  const now = Date.parse("2026-10-07T12:00:00Z");
  const at = (ms: number) => new Date(now - ms).toISOString();
  assert.equal(relativeTime(at(10_000), now), "방금 전");
  assert.equal(relativeTime(at(5 * 60_000), now), "5분 전");
  assert.equal(relativeTime(at(3 * 3600_000), now), "3시간 전");
  assert.equal(relativeTime(at(50 * 3600_000), now), "2일 전");
  assert.equal(relativeTime(at(-2 * 3600_000), now), "2시간 후");
  assert.equal(relativeTime(at(-10_000), now), "곧");
  assert.equal(relativeTime(null, now), "-");
  assert.equal(relativeTime("garbage", now), "-");
});

test("daysLeftText", () => {
  assert.equal(daysLeftText(13), "13일 남음");
  assert.equal(daysLeftText(0), "오늘 만료");
  assert.equal(daysLeftText(-3), "3일 지남");
  assert.equal(daysLeftText(null), "-");
  assert.equal(daysLeftText(undefined), "-");
});

test("daysUntil floors toward the past", () => {
  const now = Date.parse("2026-10-07T12:00:00Z");
  assert.equal(daysUntil("2026-10-20T00:00:00Z", now), 12);
  assert.equal(daysUntil("2026-10-07T13:00:00Z", now), 0);
  assert.equal(daysUntil("2026-10-06T00:00:00Z", now), -2);
  assert.equal(daysUntil(undefined, now), null);
  assert.equal(daysUntil("garbage", now), null);
});
