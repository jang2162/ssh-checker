import assert from "node:assert/strict";
import { test } from "node:test";
import { parsePort, splitList } from "./input.ts";

test("splitList splits on newlines, commas and spaces", () => {
  assert.deepEqual(splitList("10.0.0.1\n10.0.0.2, 10.0.0.3  10.0.0.4"), ["10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"]);
});

test("splitList drops empty items and duplicates but keeps order", () => {
  assert.deepEqual(splitList(" a,, b\n\n a ,c "), ["a", "b", "c"]);
  assert.deepEqual(splitList(""), []);
  assert.deepEqual(splitList(" \n , "), []);
});

test("parsePort accepts 1..65535 only", () => {
  assert.equal(parsePort("443"), 443);
  assert.equal(parsePort(" 8443 "), 8443);
  assert.equal(parsePort("1"), 1);
  assert.equal(parsePort("65535"), 65535);
  for (const bad of ["", "0", "65536", "-1", "44.3", "abc", "443a", "123456"]) assert.equal(parsePort(bad), null, bad);
});
