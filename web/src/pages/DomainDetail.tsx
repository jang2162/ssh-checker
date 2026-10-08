import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api } from "../api/endpoints.ts";
import { useApp } from "../app/context.tsx";
import { usePageTitle } from "../app/usePageTitle.ts";
import { REFRESH_MS, useResource } from "../app/useResource.ts";
import { AcmePanel } from "../components/AcmePanel.tsx";
import { Notice } from "../components/Notice.tsx";
import { ReasonList, StatusBadge } from "../components/StatusBadge.tsx";
import { TargetCard } from "../components/TargetCard.tsx";
import { ConfirmDialog } from "../dialogs/ConfirmDialog.tsx";
import { MemoDialog } from "../dialogs/MemoDialog.tsx";
import { TargetDialog } from "../dialogs/TargetDialog.tsx";
import { T, t } from "../i18n/index.ts";
import { isStale, latestCheckedAt } from "../lib/status.ts";
import { NotFound } from "./NotFound.tsx";

export function DomainDetail() {
  const params = useParams();
  const id = Number(params.id);
  const valid = Number.isInteger(id) && id > 0;
  const { admin, checkIntervalMs } = useApp();
  const navigate = useNavigate();
  const [dialog, setDialog] = useState<"memo" | "target" | "delete" | null>(null);

  const { data, error, loading, reload } = useResource(() => api.domain(id), [id], REFRESH_MS);
  usePageTitle(data?.fqdn ?? t("detail.titleFallback"));

  if (!valid) return <NotFound />;
  if (loading && !data) return <p className="muted">{t("common.loading")}</p>;
  if (!data) {
    return (
      <>
        <Notice kind="error">{error}</Notice>
        <p><Link to="/">{t("detail.backToDashboard")}</Link></p>
      </>
    );
  }

  const now = Date.now();
  const newest = latestCheckedAt(data.targets.flatMap((t) => t.results));
  const stale = isStale(newest, checkIntervalMs, now);
  const onChanged = () => void reload();
  return (
    <>
      <p className="crumb"><Link to="/">{t("detail.crumb")}</Link></p>
      <div className="page-head">
        <div>
          <h1>
            {data.fqdn} <StatusBadge status={data.status} />
          </h1>
          <div className="muted">
            {data.memo || t("detail.noMemo")}
          </div>
        </div>
        {admin && (
          <div className="actions">
            <button type="button" className="btn" onClick={() => setDialog("memo")}>{t("memo.title")}</button>
            <button type="button" className="btn danger-text" onClick={() => setDialog("delete")}>{t("detail.delete")}</button>
          </div>
        )}
      </div>

      {error && <Notice kind="error">{t("common.refreshFailed", { error })}</Notice>}
      {stale && (
        <Notice kind="warning">
          {t("detail.stale")}
        </Notice>
      )}
      <ReasonList reasons={data.reasons} />

      <div className="section-head">
        <h2>{t("detail.targets")}</h2>
        {admin && (
          <button type="button" className="btn primary small" onClick={() => setDialog("target")}>{t("detail.addTarget")}</button>
        )}
      </div>
      {data.targets.length === 0 ? (
        <div className="empty">
          <p>{admin ? t("detail.noTargetsAdmin") : t("detail.noTargets")}</p>
        </div>
      ) : (
        data.targets.map((t) => (
          <TargetCard key={t.id} domainId={data.id} target={t} admin={admin} now={now} onChanged={onChanged} />
        ))
      )}

      <AcmePanel domain={data} admin={admin} now={now} onChanged={onChanged} />

      {dialog === "memo" && (
        <MemoDialog domainId={data.id} initial={data.memo} onClose={() => setDialog(null)} onSaved={onChanged} />
      )}
      {dialog === "target" && (
        <TargetDialog domainId={data.id} onClose={() => setDialog(null)} onSaved={onChanged} />
      )}
      {dialog === "delete" && (
        <ConfirmDialog
          title={t("detail.delete")}
          confirmLabel={t("common.delete")}
          danger
          onClose={() => setDialog(null)}
          onConfirm={async () => {
            await api.deleteDomain(data.id);
            navigate("/");
          }}
        >
          <p><T k="detail.deleteBody" vars={{ fqdn: data.fqdn }} /></p>
          {data.acme.enabled && <p>{t("detail.deleteAcme")}</p>}
        </ConfirmDialog>
      )}
    </>
  );
}
