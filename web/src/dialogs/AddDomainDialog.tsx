import { useState, type FormEvent } from "react";
import { api } from "../api/endpoints.ts";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";
import { t } from "../i18n/index.ts";

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
    <Dialog title={t("addDomain.title")} onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>{t("addDomain.domain")}</span>
          <input value={fqdn} onChange={(e) => setFqdn(e.target.value)} placeholder="a.com" autoFocus required />
          <small>{t("addDomain.hint")}</small>
        </label>
        <label className="field">
          <span>{t("common.memoOptional")}</span>
          <input value={memo} onChange={(e) => setMemo(e.target.value)} maxLength={500} />
        </label>
        {error && <Notice kind="error">{error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>{t("common.cancel")}</button>
          <button type="submit" className="btn primary" disabled={busy || fqdn.trim() === ""}>{t("addDomain.submit")}</button>
        </div>
      </form>
    </Dialog>
  );
}
