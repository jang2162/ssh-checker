import assert from "node:assert/strict";
import { test } from "node:test";
import { ApiError, createClient, type FetchFn } from "./client.ts";

interface Call {
  url: string;
  init: RequestInit;
}

function fake(respond: (call: Call) => Response | Error) {
  const calls: Call[] = [];
  const fetchFn: FetchFn = async (url, init = {}) => {
    const call = { url, init };
    calls.push(call);
    const r = respond(call);
    if (r instanceof Error) throw r;
    return r;
  };
  return { calls, client: createClient(fetchFn) };
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

function header(call: Call, name: string): string | undefined {
  return (call.init.headers as Record<string, string>)[name];
}

test("GET sends no Content-Type and no body", async () => {
  const { calls, client } = fake(() => json({ ok: 1 }));
  assert.deepEqual(await client.get("/api/x"), { ok: 1 });
  assert.equal(calls[0]?.url, "/api/x");
  assert.equal(calls[0]?.init.method, "GET");
  assert.equal(header(calls[0]!, "Content-Type"), undefined);
  assert.equal(calls[0]?.init.body, undefined);
  assert.equal(calls[0]?.init.credentials, "same-origin");
});

test("writes always send Content-Type: application/json, even without a body", async () => {
  // 서버는 본문이 없는 요청에도 JSON 헤더를 요구한다(415를 막는다).
  const { calls, client } = fake(() => new Response(null, { status: 204 }));
  await client.send("DELETE", "/api/domains/1");
  assert.equal(header(calls[0]!, "Content-Type"), "application/json");
  assert.equal(calls[0]?.init.body, undefined);
  await client.send("POST", "/api/session", { key: "k" });
  assert.equal(header(calls[1]!, "Content-Type"), "application/json");
  assert.equal(calls[1]?.init.body, '{"key":"k"}');
});

test("204 resolves to undefined", async () => {
  const { client } = fake(() => new Response(null, { status: 204 }));
  assert.equal(await client.send("DELETE", "/api/x"), undefined);
});

test("an error body becomes ApiError with status and code", async () => {
  const { client } = fake(() => json({ error: "domain_exists" }, 409));
  await assert.rejects(client.send("POST", "/api/domains", {}), (e: unknown) => {
    assert.ok(e instanceof ApiError);
    assert.equal(e.status, 409);
    assert.equal(e.code, "domain_exists");
    return true;
  });
});

test("an error response that is not JSON still becomes ApiError", async () => {
  const { client } = fake(() => new Response("<html>Bad Gateway</html>", { status: 502 }));
  await assert.rejects(client.get("/api/x"), (e: unknown) => e instanceof ApiError && e.status === 502 && e.code === "unknown");
});

test("a JSON error without an error field is unknown", async () => {
  const { client } = fake(() => json({ message: "x" }, 500));
  await assert.rejects(client.get("/api/x"), (e: unknown) => e instanceof ApiError && e.code === "unknown");
});

test("a network failure becomes status 0", async () => {
  const { client } = fake(() => new TypeError("Failed to fetch"));
  await assert.rejects(client.get("/api/x"), (e: unknown) => e instanceof ApiError && e.status === 0 && e.code === "network_error");
});
