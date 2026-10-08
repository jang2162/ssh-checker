import assert from "node:assert/strict";
import { test } from "node:test";
import { shellQuote, withEnv, withForce } from "./command.ts";

test("withForce puts FORCE=1 before the run part of the command", () => {
  assert.equal(
    withForce("curl -fsSL https://x/acme/setup.sh -o setup.sh && WEB_SERVER=caddy bash setup.sh"),
    "curl -fsSL https://x/acme/setup.sh -o setup.sh && FORCE=1 WEB_SERVER=caddy bash setup.sh",
  );
  assert.equal(withForce("curl -o a.sh u && bash a.sh"), "curl -o a.sh u && FORCE=1 bash a.sh");
});

test("withEnv adds several variables in order and quotes unsafe values", () => {
  const cmd = "curl -o s u && WEB_SERVER=nginx bash s";
  assert.equal(withEnv(cmd, { FORCE: "1", CERT_DIR: "/srv/tls" }), "curl -o s u && FORCE=1 CERT_DIR=/srv/tls WEB_SERVER=nginx bash s");
  assert.equal(withEnv(cmd, { CERT_DIR: "/srv/o'brien" }), "curl -o s u && CERT_DIR='/srv/o'\\''brien' WEB_SERVER=nginx bash s");
  assert.equal(withEnv(cmd, {}), cmd);
});

test("withEnv leaves a command without && unchanged", () => {
  assert.equal(withForce("bash setup.sh"), "bash setup.sh");
});

test("shellQuote keeps safe values and quotes the rest", () => {
  assert.equal(shellQuote("/etc/ssl/a.com"), "/etc/ssl/a.com");
  assert.equal(shellQuote("a b"), "'a b'");
  assert.equal(shellQuote("$(rm -rf /)"), "'$(rm -rf /)'");
});
