import { useState } from "react";
import { api } from "../api/endpoints.ts";
import type { Target } from "../api/types.ts";
import { useResource } from "../app/useResource.ts";
import { useAction } from "../app/useAction.ts";
import { ConfirmDialog } from "../dialogs/ConfirmDialog.tsx";
import { T, t } from "../i18n/index.ts";
import { TargetDialog } from "../dialogs/TargetDialog.tsx";
import { Notice } from "./Notice.tsx";
import { HistoryTable, ResultsTable } from "./ResultsTable.tsx";
import { ReasonTags, StatusBadge } from "./StatusBadge.tsx";

interface Props {
  domainId: number;
  target: Target;
  admin: boolean;
  now: number;
  onChanged: () => void;
}

function TargetHistory({ targetId }: { targetId: number }) {
  const { data, error, loading } = useResource(() => api.targetResults(targetId, 50), [targetId]);
  if (loading && !data) return <p className="muted">{t("common.loading")}</p>;
  if (error && !data) return <Notice kind="error">{error}</Notice>;
  return <HistoryTable results={data ?? []} />;
}

export function TargetCard({ domainId, target, admin, now, onChanged }: Props) {
  const [dialog, setDialog] = useState<"edit" | "delete" | null>(null);
  const [history, setHistory] = useState(false);
  const check = useAction();

  async function checkNow() {
    const r = await check.run(() => api.checkTarget(target.id));
    if (r) onChanged();
  }

  const where =
    target.ips.length > 0 ? t("target.ips", { ips: target.ips.join(", ") }) : t("target.noIps");
  return (
    <section className="card target">
      <div className="target-head">
        <div>
          <h3>
            {target.hostname}:{target.port} <StatusBadge status={target.status} />
          </h3>
          <div className="muted small-text">
            {target.sni && `SNI ${target.sni} · `}
            {where}
            {target.memo && ` · ${target.memo}`}
          </div>
          <ReasonTags reasons={target.reasons} />
        </div>
        <div className="actions">
          <button type="button" className="btn small" aria-expanded={history} onClick={() => setHistory((h) => !h)}>
            {t("target.history")}
          </button>
          {admin && (
            <>
              <button type="button" className="btn small" disabled={check.busy} onClick={() => void checkNow()}>
                {check.busy ? t("common.checking") : t("target.checkNow")}
              </button>
              <button type="button" className="btn small" onClick={() => setDialog("edit")}>{t("common.edit")}</button>
              <button type="button" className="btn small danger-text" onClick={() => setDialog("delete")}>{t("common.delete")}</button>
            </>
          )}
        </div>
      </div>
      {check.error && <Notice kind="error">{check.error}</Notice>}
      <ResultsTable results={target.results} now={now} />
      {history && <TargetHistory targetId={target.id} />}

      {dialog === "edit" && (
        <TargetDialog domainId={domainId} target={target} onClose={() => setDialog(null)} onSaved={onChanged} />
      )}
      {dialog === "delete" && (
        <ConfirmDialog
          title={t("target.deleteTitle")}
          confirmLabel={t("common.delete")}
          danger
          onClose={() => setDialog(null)}
          onConfirm={async () => {
            await api.deleteTarget(target.id);
            onChanged();
          }}
        >
          <p><T k="target.deleteBody" vars={{ target: `${target.hostname}:${target.port}` }} /></p>
        </ConfirmDialog>
      )}
    </section>
  );
}
