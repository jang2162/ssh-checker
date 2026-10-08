import assert from "node:assert/strict";
import { test } from "node:test";
import { withForce } from "./command.ts";

test("withForce puts FORCE=1 before the run part of the command", () => {
  assert.equal(
    withForce("curl -fsSL https://x/acme/setup.sh -o setup.sh && WEB_SERVER=caddy bash setup.sh"),
    "curl -fsSL https://x/acme/setup.sh -o setup.sh && FORCE=1 WEB_SERVER=caddy bash setup.sh",
  );
  assert.equal(withForce("curl -o a.sh u && bash a.sh"), "curl -o a.sh u && FORCE=1 bash a.sh");
});

test("withForce leaves a command without && unchanged", () => {
  assert.equal(withForce("bash setup.sh"), "bash setup.sh");
});
