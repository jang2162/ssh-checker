import { useState, type FormEvent } from "react";
import { api } from "../api/endpoints.ts";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";

export function MemoDialog({ domainId, initial, onClose, onSaved }: { domainId: number; initial: string; onClose: () => void; onSaved: () => void }) {
  const [memo, setMemo] = useState(initial);
  const { run, busy, error } = useAction();

  async function submit(e: FormEvent) {
    e.preventDefault();
    const ok = await run(async () => {
      await api.patchDomain(domainId, memo);
      return true;
    });
    if (ok) {
      onSaved();
      onClose();
    }
  }

  return (
    <Dialog title="메모 수정" onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>메모</span>
          <input value={memo} onChange={(e) => setMemo(e.target.value)} maxLength={500} autoFocus />
        </label>
        {error && <Notice kind="error">{error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>취소</button>
          <button type="submit" className="btn primary" disabled={busy}>저장</button>
        </div>
      </form>
    </Dialog>
  );
}
