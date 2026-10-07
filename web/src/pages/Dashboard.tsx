import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../api/endpoints.ts";
import { useApp } from "../app/context.tsx";
import { usePageTitle } from "../app/usePageTitle.ts";
import { REFRESH_MS, useResource } from "../app/useResource.ts";
import { Notice } from "../components/Notice.tsx";
import { ReasonTags, StatusBadge } from "../components/StatusBadge.tsx";
import { AddDomainDialog } from "../dialogs/AddDomainDialog.tsx";
import { daysLeftText, formatDate, formatDateTime, relativeTime } from "../lib/format.ts";
import { CNAME_LABEL, isStale, parseStatus, sortByUrgency, STATUS_LABEL, STATUS_ORDER } from "../lib/status.ts";

export function Dashboard() {
  usePageTitle("대시보드");
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
        <h1>도메인 {data ? `${data.summary.domains}개` : ""}</h1>
        {admin && (
          <button type="button" className="btn primary" onClick={() => setAdding(true)}>도메인 추가</button>
        )}
      </div>

      {error && <Notice kind="error">{data ? `새로 고치지 못했습니다: ${error}` : error}</Notice>}
      {anyStale && (
        <Notice kind="warning">
          일부 점검 결과가 오래됐습니다(점검 주기의 2배 넘게 지남). 상태는 점검할 때 저장되므로, worker가 멈췄다면 아래 "정상"도 최신이 아닐 수 있습니다.
        </Notice>
      )}

      {data && (
        <div className="summary" role="group" aria-label="상태별 도메인 수">
          {STATUS_ORDER.map((s) => (
            <button
              key={s}
              type="button"
              className={`summary-card summary-${s}`}
              aria-pressed={filter === s}
              onClick={() => toggle(s)}
            >
              <span className="summary-count">{data.summary[s]}</span>
              <span className="summary-label">{STATUS_LABEL[s]}</span>
            </button>
          ))}
        </div>
      )}

      {loading && !data && <p className="muted">불러오는 중…</p>}

      {data && rows.length === 0 && (
        <div className="empty">
          {filter ? (
            <p>"{STATUS_LABEL[filter]}" 상태인 도메인이 없습니다.</p>
          ) : (
            <p>
              등록된 도메인이 없습니다.
              {admin ? " 위의 \"도메인 추가\"로 시작하세요." : " 관리자로 로그인하면 도메인을 추가할 수 있습니다."}
            </p>
          )}
        </div>
      )}

      {rows.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>상태</th>
                <th>도메인</th>
                <th>가장 이른 만료</th>
                <th>대상</th>
                <th>ACME</th>
                <th>마지막 점검</th>
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
                        켜짐 <span className="muted small-text">{d.acme.cname_status ? CNAME_LABEL[d.acme.cname_status] : ""}</span>
                      </span>
                    ) : (
                      <span className="muted">꺼짐</span>
                    )}
                  </td>
                  <td className="nowrap" title={formatDateTime(d.last_checked_at)}>
                    {relativeTime(d.last_checked_at, now)}
                    {isStale(d.last_checked_at, checkIntervalMs, now) && (
                      <span className="tag tag-warning" title="점검이 멈춘 것 같습니다">오래됨</span>
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
