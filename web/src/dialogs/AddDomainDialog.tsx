import { useState, type FormEvent } from "react";
import { api } from "../api/endpoints.ts";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";

export function AddDomainDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (id: number) => void }) {
  const [fqdn, setFqdn] = useState("");
  const [memo, setMemo] = useState("");
  const { run, busy, error } = useAction();

  async function submit(e: FormEvent) {
    e.preventDefault();
    const d = await run(() => api.createDomain(fqdn.trim(), memo));
    if (d) onCreated(d.id);
  }

  return (
    <Dialog title="도메인 추가" onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>도메인</span>
          <input value={fqdn} onChange={(e) => setFqdn(e.target.value)} placeholder="a.com" autoFocus required />
          <small>와일드카드 없이 입력합니다. 등록해도 ACME는 꺼져 있고, 상세 화면에서 켤 수 있습니다.</small>
        </label>
        <label className="field">
          <span>메모 (선택)</span>
          <input value={memo} onChange={(e) => setMemo(e.target.value)} maxLength={500} />
        </label>
        {error && <Notice kind="error">{error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>취소</button>
          <button type="submit" className="btn primary" disabled={busy || fqdn.trim() === ""}>추가</button>
        </div>
      </form>
    </Dialog>
  );
}
