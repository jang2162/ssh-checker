import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../api/endpoints.ts";
import { useApp } from "../app/context.tsx";
import { usePageTitle } from "../app/usePageTitle.ts";
import { REFRESH_MS, useResource } from "../app/useResource.ts";
import { Notice } from "../components/Notice.tsx";
import { ReasonTags, StatusBadge } from "../components/StatusBadge.tsx";
import { AddDomainDialog } from "../dialogs/AddDomainDialog.tsx";
import { t } from "../i18n/index.ts";
import { daysLeftText, formatDate, formatDateTime, relativeTime } from "../lib/format.ts";
import { cnameLabel, isStale, parseStatus, sortByUrgency, statusLabel, STATUS_ORDER } from "../lib/status.ts";

export function Dashboard() {
  usePageTitle(t("nav.dashboard"));
  const { admin, checkIntervalMs } = useApp();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const filter = parseStatus(params.get("status"));
  const [adding, setAdding] = useState(false);

  const { data, error, loading } = useResource(
    async () => {
      const [summary, domains] = await Promise.all([api.summary(), api.domains()]);
      return { summary, domains };
    },
    [],
    REFRESH_MS,
  );

  function toggle(status: (typeof STATUS_ORDER)[number]) {
    setParams(filter === status ? {} : { status });
  }

  const now = Date.now();
  const rows = sortByUrgency((data?.domains ?? []).filter((d) => filter === null || d.status === filter));
  const anyStale = (data?.domains ?? []).some((d) => isStale(d.last_checked_at, checkIntervalMs, now));

  return (
    <>
      <div className="page-head">
        <h1>{data ? t("dashboard.headingCount", { count: data.summary.domains }) : t("dashboard.heading")}</h1>
        {admin && (
          <button type="button" className="btn primary" onClick={() => setAdding(true)}>{t("addDomain.title")}</button>
        )}
      </div>

      {error && <Notice kind="error">{data ? t("common.refreshFailed", { error }) : error}</Notice>}
      {anyStale && (
        <Notice kind="warning">
          {t("dashboard.stale")}
        </Notice>
      )}

      {data && (
        <div className="summary" role="group" aria-label={t("dashboard.summaryLabel")}>
          {STATUS_ORDER.map((s) => (
            <button
              key={s}
              type="button"
              className={`summary-card summary-${s}`}
              aria-pressed={filter === s}
              onClick={() => toggle(s)}
            >
              <span className="summary-count">{data.summary[s]}</span>
              <span className="summary-label">{statusLabel(s)}</span>
            </button>
          ))}
        </div>
      )}

      {loading && !data && <p className="muted">{t("common.loading")}</p>}

      {data && rows.length === 0 && (
        <div className="empty">
          {filter ? (
            <p>{t("dashboard.noneWithStatus", { status: statusLabel(filter) })}</p>
          ) : (
            <p>
              {admin ? t("dashboard.emptyAdmin") : t("dashboard.emptyViewer")}
            </p>
          )}
        </div>
      )}

      {rows.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>{t("col.status")}</th>
                <th>{t("col.domain")}</th>
                <th>{t("col.earliestExpiry")}</th>
                <th>{t("col.targets")}</th>
                <th>ACME</th>
                <th>{t("col.lastChecked")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((d) => (
                <tr key={d.id}>
                  <td>
                    <StatusBadge status={d.status} />
                    <ReasonTags reasons={d.reasons} />
                  </td>
                  <td>
                    <Link to={`/domains/${d.id}`}>{d.fqdn}</Link>
                    {d.memo && <div className="muted small-text">{d.memo}</div>}
                  </td>
                  <td>
                    {formatDate(d.earliest_not_after)}
                    {d.days_left !== null && <div className="muted small-text">{daysLeftText(d.days_left)}</div>}
                  </td>
                  <td className="num">{d.targets}</td>
                  <td>
                    {d.acme.enabled ? (
                      <span>
                        {t("dashboard.acmeOn")} <span className="muted small-text">{d.acme.cname_status ? cnameLabel(d.acme.cname_status) : ""}</span>
                      </span>
                    ) : (
                      <span className="muted">{t("dashboard.acmeOff")}</span>
                    )}
                  </td>
                  <td className="nowrap" title={formatDateTime(d.last_checked_at)}>
                    {relativeTime(d.last_checked_at, now)}
                    {isStale(d.last_checked_at, checkIntervalMs, now) && (
                      <span className="tag tag-warning" title={t("dashboard.staleTitle")}>{t("dashboard.staleTag")}</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {adding && <AddDomainDialog onClose={() => setAdding(false)} onCreated={(id) => navigate(`/domains/${id}`)} />}
    </>
  );
}
