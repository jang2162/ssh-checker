import assert from "node:assert/strict";
import { test } from "node:test";
import { setLanguage } from "../i18n/index.ts";
import { daysLeftText, daysUntil, formatDate, formatDateTime, relativeTime, shortSerial } from "./format.ts";

test("shortSerial keeps the first 8 characters", () => {
  assert.equal(shortSerial("04ab12cd34ef56"), "04ab12cd");
  assert.equal(shortSerial("ab"), "ab");
  assert.equal(shortSerial(""), "");
});

test("formatDateTime renders a fixed pattern in the given time zone", () => {
  setLanguage("ko");
  assert.equal(formatDateTime("2026-10-07T12:34:56Z", "UTC"), "2026-10-07 12:34");
  assert.equal(formatDateTime("2026-10-07T12:34:56Z", "Asia/Seoul"), "2026-10-07 21:34");
  assert.equal(formatDateTime("2026-10-07T15:00:00Z", "Asia/Seoul"), "2026-10-08 00:00"); // 자정은 24시가 아니라 00시
});

test("formatDateTime and formatDate give - for missing or invalid values", () => {
  for (const l of ["ko", "en"] as const) {
    setLanguage(l);
    for (const v of [null, undefined, "", "garbage"]) {
      assert.equal(formatDateTime(v), "-");
      assert.equal(formatDate(v), "-");
    }
  }
  setLanguage("ko");
  assert.equal(formatDate("2026-10-07T12:34:56Z", "UTC"), "2026-10-07");
});

test("English dates use the en-US month style with a 24-hour clock", () => {
  setLanguage("en");
  assert.equal(formatDateTime("2026-10-07T12:34:56Z", "UTC"), "Oct 7, 2026, 12:34");
  assert.equal(formatDateTime("2026-10-07T03:04:56Z", "UTC"), "Oct 7, 2026, 03:04");
  assert.equal(formatDateTime("2026-10-07T15:00:00Z", "Asia/Seoul"), "Oct 8, 2026, 00:00");
  assert.equal(formatDate("2026-10-07T12:34:56Z", "UTC"), "Oct 7, 2026");
  assert.equal(formatDate("2026-10-07T15:00:00Z", "Asia/Seoul"), "Oct 8, 2026");
});

test("relativeTime counts in minutes, hours and days", () => {
  const now = Date.parse("2026-10-07T12:00:00Z");
  const at = (ms: number) => new Date(now - ms).toISOString();
  setLanguage("ko");
  assert.equal(relativeTime(at(10_000), now), "방금 전");
  assert.equal(relativeTime(at(5 * 60_000), now), "5분 전");
  assert.equal(relativeTime(at(3 * 3600_000), now), "3시간 전");
  assert.equal(relativeTime(at(50 * 3600_000), now), "2일 전");
  assert.equal(relativeTime(at(-2 * 3600_000), now), "2시간 후");
  assert.equal(relativeTime(at(-10_000), now), "곧");
  assert.equal(relativeTime(null, now), "-");
  assert.equal(relativeTime("garbage", now), "-");
});

test("relativeTime in English handles singular and plural", () => {
  const now = Date.parse("2026-10-07T12:00:00Z");
  const at = (ms: number) => new Date(now - ms).toISOString();
  setLanguage("en");
  assert.equal(relativeTime(at(10_000), now), "just now");
  assert.equal(relativeTime(at(-10_000), now), "soon");
  assert.equal(relativeTime(at(60_000), now), "1 minute ago");
  assert.equal(relativeTime(at(5 * 60_000), now), "5 minutes ago");
  assert.equal(relativeTime(at(3600_000), now), "1 hour ago");
  assert.equal(relativeTime(at(3 * 3600_000), now), "3 hours ago");
  assert.equal(relativeTime(at(26 * 3600_000), now), "1 day ago");
  assert.equal(relativeTime(at(50 * 3600_000), now), "2 days ago");
  assert.equal(relativeTime(at(-2 * 3600_000), now), "in 2 hours");
  assert.equal(relativeTime(at(-61_000), now), "in 1 minute");
  assert.equal(relativeTime(at(-3 * 86_400_000), now), "in 3 days");
  assert.equal(relativeTime(null, now), "-");
});

test("daysLeftText", () => {
  setLanguage("ko");
  assert.equal(daysLeftText(13), "13일 남음");
  assert.equal(daysLeftText(0), "오늘 만료");
  assert.equal(daysLeftText(-3), "3일 지남");
  assert.equal(daysLeftText(null), "-");
  assert.equal(daysLeftText(undefined), "-");
});

test("daysLeftText in English", () => {
  setLanguage("en");
  assert.equal(daysLeftText(13), "13 days left");
  assert.equal(daysLeftText(1), "1 day left");
  assert.equal(daysLeftText(0), "expires today");
  assert.equal(daysLeftText(-1), "expired 1 day ago");
  assert.equal(daysLeftText(-3), "expired 3 days ago");
  assert.equal(daysLeftText(null), "-");
});

test("daysUntil floors toward the past", () => {
  const now = Date.parse("2026-10-07T12:00:00Z");
  assert.equal(daysUntil("2026-10-20T00:00:00Z", now), 12);
  assert.equal(daysUntil("2026-10-07T13:00:00Z", now), 0);
  assert.equal(daysUntil("2026-10-06T00:00:00Z", now), -2);
  assert.equal(daysUntil(undefined, now), null);
  assert.equal(daysUntil("garbage", now), null);
});
