import assert from "node:assert/strict";
import { test } from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { en } from "./en.ts";
import { lang, locale, parseRich, setLanguage, T, t, tn } from "./index.ts";
import { ko } from "./ko.ts";

const placeholders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
const tags = (s: string) => [...s.matchAll(/<\/?(?:b|code)>/g)].map((m) => m[0]).sort();

test("ko and en have exactly the same keys", () => {
  assert.deepEqual(Object.keys(en).sort(), Object.keys(ko).sort());
});

test("every key has the same placeholders and rich-text tags in both languages", () => {
  for (const [key, k] of Object.entries(ko)) {
    const e = (en as Record<string, string>)[key] ?? "";
    assert.deepEqual(placeholders(e), placeholders(k), `placeholders of ${key}`);
    assert.deepEqual(tags(e), tags(k), `tags of ${key}`);
  }
});

test("no English message is empty or still Korean", () => {
  for (const [key, v] of Object.entries(en)) {
    assert.ok(v.trim().length > 0, `${key} is empty`);
    assert.doesNotMatch(v, /[ㄱ-ㆎ가-힣]/, `${key} contains Hangul`);
  }
});

test("every _one message has a matching plural message", () => {
  for (const key of Object.keys(ko)) {
    if (key.endsWith("_one")) assert.ok(key.slice(0, -4) in ko, `${key} has no plural form`);
  }
});

test("without a document the language defaults to English", () => {
  assert.equal(lang(), "en");
  assert.equal(locale(), "en-US");
  setLanguage("ko");
  assert.equal(lang(), "ko");
  assert.equal(locale(), "ko-KR");
});

test("t fills placeholders and leaves unknown ones", () => {
  setLanguage("ko");
  assert.equal(t("common.refreshFailed", { error: "x" }), "새로 고치지 못했습니다: x");
  assert.equal(t("common.refreshFailed"), "새로 고치지 못했습니다: {error}");
  setLanguage("en");
  assert.equal(t("common.refreshFailed", { error: "x" }), "Could not refresh: x");
  assert.equal(t("dashboard.headingCount", { count: 3 }), "Domains (3)");
});

test("tn picks the singular form only where the language has one", () => {
  setLanguage("en");
  assert.equal(tn("acme.consecutiveFailures", 1), "1 consecutive failure");
  assert.equal(tn("acme.consecutiveFailures", 2), "2 consecutive failures");
  setLanguage("ko");
  assert.equal(tn("acme.consecutiveFailures", 1), "연속 1회 실패");
  assert.equal(tn("acme.consecutiveFailures", 2), "연속 2회 실패");
});

test("parseRich builds nested b/code nodes and keeps other angle brackets as text", () => {
  assert.deepEqual(parseRich("a <b>x <code>y</code></b> {n} <domain>"), [
    "a ",
    { tag: "b", children: ["x ", { tag: "code", children: ["y"] }] },
    " ",
    { var: "n" },
    " <domain>",
  ]);
  assert.deepEqual(parseRich("x</b>y"), ["x</b>y"]); // 짝 없는 닫는 태그는 글자로 남는다
  assert.deepEqual(parseRich("<b>open"), [{ tag: "b", children: ["open"] }]);
});

const html = (k: Parameters<typeof T>[0]["k"], vars?: Parameters<typeof T>[0]["vars"]) =>
  renderToStaticMarkup(createElement(T, { k, vars }));

test("T renders <b> as <strong> and <code>, matching the old Korean markup", () => {
  setLanguage("ko");
  assert.equal(
    html("guide.sudo"),
    "<strong>reload에 <code>sudo</code>가 필요하면</strong> root 셸(<code>sudo -i</code>)에서 실행하세요. 자동 갱신은 sudo 비밀번호를 입력할 수 없습니다. root에서는 스크립트가 <code>sudo</code>를 빼고 설정합니다.",
  );
  assert.equal(
    html("guide.checkCertDir"),
    "<strong>저장 경로</strong>: 기본 <code>/etc/ssl/&lt;도메인&gt;</code>. 발급, 설치, 갱신 시 reload까지 합니다.",
  );
  assert.equal(html("detail.deleteBody", { fqdn: "a.com" }), "<strong>a.com</strong>과 모든 대상, 점검 이력을 삭제합니다.");
});

test("T never interprets markup inside vars", () => {
  setLanguage("en");
  assert.equal(
    html("acme.cnameMismatch", { value: "<b>x</b>" }),
    "Current value: <code>&lt;b&gt;x&lt;/b&gt;</code>. It must be changed to the value of the record above.",
  );
  assert.equal(
    html("audit.adminOnly", { link: createElement("a", { href: "/admin" }, "Admin login") }),
    'Only admins can view the audit log. <a href="/admin">Admin login</a>',
  );
});
