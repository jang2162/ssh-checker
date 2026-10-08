import { useState, type FormEvent } from "react";
import { api } from "../api/endpoints.ts";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";
import { t } from "../i18n/index.ts";

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
    <Dialog title={t("memo.title")} onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>{t("common.memo")}</span>
          <input value={memo} onChange={(e) => setMemo(e.target.value)} maxLength={500} autoFocus />
        </label>
        {error && <Notice kind="error">{error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>{t("common.cancel")}</button>
          <button type="submit" className="btn primary" disabled={busy}>{t("common.save")}</button>
        </div>
      </form>
    </Dialog>
  );
}
