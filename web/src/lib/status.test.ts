import assert from "node:assert/strict";
import { test } from "node:test";
import type { Status } from "../api/types.ts";
import { isStale, latestCheckedAt, parseGoDuration, parseStatus, sortByUrgency, STATUS_LABEL, STATUS_ORDER } from "./status.ts";

test("parseGoDuration reads Go duration strings", () => {
  assert.equal(parseGoDuration("6h0m0s"), 6 * 3600_000);
  assert.equal(parseGoDuration("5m0s"), 5 * 60_000);
  assert.equal(parseGoDuration("1h30m0s"), 90 * 60_000);
  assert.equal(parseGoDuration("30s"), 30_000);
  assert.equal(parseGoDuration("1.5s"), 1500);
});

test("parseGoDuration rejects what it does not understand", () => {
  for (const bad of ["", "6", "abc", "6x", "-5m", "1d"]) assert.equal(parseGoDuration(bad), null, bad);
});

test("parseStatus accepts only known statuses", () => {
  assert.equal(parseStatus("critical"), "critical");
  assert.equal(parseStatus("unknown"), "unknown");
  assert.equal(parseStatus("nope"), null);
  assert.equal(parseStatus(null), null);
});

test("every status has a label and order is worst first", () => {
  assert.deepEqual([...STATUS_ORDER], ["critical", "warning", "info", "ok", "unknown"]);
  for (const s of STATUS_ORDER) assert.ok(STATUS_LABEL[s].length > 0);
});

test("isStale flags results older than twice the interval", () => {
  const interval = 6 * 3600_000;
  const checked = "2026-10-07T00:00:00Z";
  const at = Date.parse(checked);
  assert.equal(isStale(checked, interval, at + 2 * interval), false); // 딱 2배는 아직 괜찮다
  assert.equal(isStale(checked, interval, at + 2 * interval + 1), true);
  assert.equal(isStale(checked, interval, at + interval), false);
});

test("isStale is false when there is nothing to compare", () => {
  const now = Date.parse("2030-01-01T00:00:00Z");
  assert.equal(isStale(null, 3600_000, now), false); // 미점검은 stale이 아니라 unknown이다
  assert.equal(isStale("2026-10-07T00:00:00Z", null, now), false); // 주기를 모르면 판단하지 않는다
  assert.equal(isStale("not a date", 3600_000, now), false);
});

test("sortByUrgency puts worse statuses first, then sooner expiry, then name", () => {
  const d = (fqdn: string, status: Status, days_left: number | null) => ({ fqdn, status, days_left });
  const sorted = sortByUrgency([
    d("z.com", "ok", 80),
    d("b.com", "critical", null),
    d("a.com", "ok", 10),
    d("c.com", "critical", 2),
    d("e.com", "unknown", null),
    d("d.com", "critical", null),
    d("f.com", "warning", 5),
  ]).map((x) => x.fqdn);
  assert.deepEqual(sorted, ["c.com", "b.com", "d.com", "f.com", "a.com", "z.com", "e.com"]);
});

test("sortByUrgency does not change its input", () => {
  const input = [
    { fqdn: "b", status: "ok" as Status, days_left: 1 },
    { fqdn: "a", status: "critical" as Status, days_left: 1 },
  ];
  sortByUrgency(input);
  assert.equal(input[0]?.fqdn, "b");
});

test("latestCheckedAt picks the newest valid time", () => {
  assert.equal(latestCheckedAt([]), null);
  assert.equal(latestCheckedAt([{ checked_at: "2026-01-01T00:00:00Z" }]), "2026-01-01T00:00:00Z");
  assert.equal(
    latestCheckedAt([{ checked_at: "2026-01-01T00:00:00Z" }, { checked_at: "2026-03-01T00:00:00Z" }, { checked_at: "2026-02-01T00:00:00Z" }]),
    "2026-03-01T00:00:00Z",
  );
  assert.equal(latestCheckedAt([{ checked_at: "garbage" }, { checked_at: "2026-02-01T00:00:00Z" }]), "2026-02-01T00:00:00Z");
  assert.equal(latestCheckedAt([{ checked_at: "garbage" }]), null);
});
