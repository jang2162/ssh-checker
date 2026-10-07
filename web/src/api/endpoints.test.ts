import assert from "node:assert/strict";
import { test } from "node:test";
import { createClient, type FetchFn } from "./client.ts";
import { createApi } from "./endpoints.ts";

function setup(body: unknown = {}, status = 200) {
  const calls: { url: string; method: string; body: string | undefined }[] = [];
  const fetchFn: FetchFn = async (url, init = {}) => {
    calls.push({ url, method: String(init.method), body: init.body as string | undefined });
    return status === 204 ? new Response(null, { status }) : new Response(JSON.stringify(body), { status });
  };
  return { calls, api: createApi(createClient(fetchFn)) };
}

test("list endpoints unwrap their envelope", async () => {
  const d = setup({ domains: [{ id: 1 }] });
  assert.deepEqual(await d.api.domains(), [{ id: 1 }]);
  const c = setup({ codes: [{ code: "X" }] });
  assert.deepEqual(await c.api.codes(), [{ code: "X" }]);
  const r = setup({ results: [{ address: "a" }] });
  assert.deepEqual(await r.api.targetResults(7, 10), [{ address: "a" }]);
  assert.equal(r.calls[0]?.url, "/api/targets/7/results?limit=10");
  const s = setup({ admin: true });
  assert.equal(await s.api.session(), true);
});

test("audit paging passes limit and before_id", async () => {
  const a = setup({ entries: [] });
  await a.api.audit();
  await a.api.audit(123, 50);
  assert.equal(a.calls[0]?.url, "/api/audit?limit=100");
  assert.equal(a.calls[1]?.url, "/api/audit?limit=50&before_id=123");
});

test("ACME calls use the documented paths and bodies", async () => {
  const e = setup({});
  await e.api.enableAcme(3, ["10.0.0.0/24"]);
  await e.api.patchAcme(3, []);
  await e.api.rotateAcme(3);
  await e.api.checkAcme(3);
  assert.deepEqual(
    e.calls.map((c) => [c.method, c.url, c.body]),
    [
      ["POST", "/api/domains/3/acme", '{"allowfrom":["10.0.0.0/24"]}'],
      ["PATCH", "/api/domains/3/acme", '{"allowfrom":[]}'],
      ["POST", "/api/domains/3/acme/rotate", undefined],
      ["POST", "/api/domains/3/acme/check", undefined],
    ],
  );
  const d = setup({}, 204);
  await d.api.disableAcme(3);
  assert.deepEqual([d.calls[0]?.method, d.calls[0]?.url], ["DELETE", "/api/domains/3/acme"]);
});

test("domain and target writes", async () => {
  const w = setup({});
  await w.api.createDomain("a.com", "memo");
  await w.api.patchDomain(1, "new");
  await w.api.createTarget(1, { hostname: "www.a.com", port: 443, sni: "", ips: ["10.0.0.1"], memo: "" });
  await w.api.checkTarget(9);
  assert.deepEqual(
    w.calls.map((c) => [c.method, c.url]),
    [
      ["POST", "/api/domains"],
      ["PATCH", "/api/domains/1"],
      ["POST", "/api/domains/1/targets"],
      ["POST", "/api/targets/9/check"],
    ],
  );
  assert.equal(w.calls[0]?.body, '{"fqdn":"a.com","memo":"memo"}');
});

test("login posts the key and logout deletes the session", async () => {
  const l = setup({ admin: true });
  await l.api.login("secret");
  assert.deepEqual([l.calls[0]?.method, l.calls[0]?.url, l.calls[0]?.body], ["POST", "/api/session", '{"key":"secret"}']);
  const o = setup({}, 204);
  await o.api.logout();
  assert.deepEqual([o.calls[0]?.method, o.calls[0]?.url], ["DELETE", "/api/session"]);
});
