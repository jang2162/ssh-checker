import { useState } from "react";

/** 클립보드는 https나 localhost에서만 된다. 안 되면 "복사 실패"를 보여 직접 선택해 복사하게 한다. */
export function CopyButton({ text, label = "복사" }: { text: string; label?: string }) {
  const [state, setState] = useState<"idle" | "done" | "fail">("idle");
  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setState("done");
    } catch {
      setState("fail");
    }
    setTimeout(() => setState("idle"), 2000);
  }
  return (
    <button type="button" className="btn small" onClick={() => void copy()}>
      {state === "done" ? "복사됨" : state === "fail" ? "복사 실패" : label}
    </button>
  );
}
