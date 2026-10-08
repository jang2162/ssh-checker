import { useState, type FormEvent } from "react";
import { api } from "../api/endpoints.ts";
import type { Target } from "../api/types.ts";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";
import { t } from "../i18n/index.ts";
import { parsePort, splitList } from "../lib/input.ts";

interface Props {
  domainId: number;
  /** 있으면 수정, 없으면 추가. */
  target?: Target;
  onClose: () => void;
  onSaved: () => void;
}

export function TargetDialog({ domainId, target, onClose, onSaved }: Props) {
  const [hostname, setHostname] = useState(target?.hostname ?? "");
  const [port, setPort] = useState(String(target?.port ?? 443));
  const [sni, setSni] = useState(target?.sni ?? "");
  const [ips, setIps] = useState((target?.ips ?? []).join("\n"));
  const [memo, setMemo] = useState(target?.memo ?? "");
  const [localError, setLocalError] = useState<string | null>(null);
  const { run, busy, error } = useAction();

  async function submit(e: FormEvent) {
    e.preventDefault();
    const p = parsePort(port);
    if (p === null) {
      setLocalError(t("targetDialog.portError"));
      return;
    }
    setLocalError(null);
    const input = { hostname: hostname.trim(), port: p, sni: sni.trim(), ips: splitList(ips), memo };
    const ok = await run(async () => {
      if (target) await api.patchTarget(target.id, input);
      else await api.createTarget(domainId, input);
      return true;
    });
    if (ok) {
      onSaved();
      onClose();
    }
  }

  return (
    <Dialog title={target ? t("targetDialog.editTitle") : t("targetDialog.addTitle")} onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>{t("targetDialog.hostname")}</span>
          <input value={hostname} onChange={(e) => setHostname(e.target.value)} placeholder="www.a.com" autoFocus required />
        </label>
        <label className="field">
          <span>{t("targetDialog.port")}</span>
          <input value={port} onChange={(e) => setPort(e.target.value)} inputMode="numeric" required />
        </label>
        <label className="field">
          <span>{t("targetDialog.sni")}</span>
          <input value={sni} onChange={(e) => setSni(e.target.value)} placeholder={t("targetDialog.sniPlaceholder")} />
        </label>
        <label className="field">
          <span>{t("targetDialog.ips")}</span>
          <textarea value={ips} onChange={(e) => setIps(e.target.value)} rows={3} placeholder={"10.0.1.10\n10.0.1.11"} />
          <small>
            {t("targetDialog.ipsHint")}
          </small>
        </label>
        <label className="field">
          <span>{t("common.memoOptional")}</span>
          <input value={memo} onChange={(e) => setMemo(e.target.value)} maxLength={500} />
        </label>
        <p className="muted small-text">{t("targetDialog.saveNote")}</p>
        {(localError ?? error) && <Notice kind="error">{localError ?? error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>{t("common.cancel")}</button>
          <button type="submit" className="btn primary" disabled={busy || hostname.trim() === ""}>
            {busy ? t("common.checking") : t("common.save")}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
