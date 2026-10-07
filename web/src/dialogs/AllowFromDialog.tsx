import { useState, type FormEvent, type ReactNode } from "react";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";
import { splitList } from "../lib/input.ts";

interface Props {
  title: string;
  submitLabel: string;
  initial: readonly string[];
  hint?: ReactNode;
  /** 실패하면 던진다. 성공하면 창이 닫힌다. */
  onSubmit: (allowfrom: string[]) => Promise<unknown>;
  onClose: () => void;
}

/** ACME를 켤 때와 허용 IP를 고칠 때 함께 쓴다. /update를 부를 수 있는 서버의 IP·CIDR 목록을 받는다. */
export function AllowFromDialog({ title, submitLabel, initial, hint, onSubmit, onClose }: Props) {
  const [text, setText] = useState(initial.join("\n"));
  const { run, busy, error } = useAction();
  const list = splitList(text);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const ok = await run(async () => {
      await onSubmit(list);
      return true;
    });
    if (ok) onClose();
  }

  return (
    <Dialog title={title} onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>허용 IP (한 줄에 하나, IP 또는 CIDR)</span>
          <textarea value={text} onChange={(e) => setText(e.target.value)} rows={4} placeholder={"203.0.113.10/32\n10.0.1.0/24"} autoFocus />
          <small>/update를 부를 수 있는 서버입니다. 최대 64개.</small>
        </label>
        {hint}
        {list.length === 0 && <Notice kind="warning">목록이 비어 있으면 키를 아는 누구나 어디서든 /update를 부를 수 있습니다. 가능하면 서버 IP를 지정하세요.</Notice>}
        {error && <Notice kind="error">{error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>취소</button>
          <button type="submit" className="btn primary" disabled={busy}>{submitLabel}</button>
        </div>
      </form>
    </Dialog>
  );
}
