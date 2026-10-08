import { Fragment } from "react";
import type { CheckResult } from "../api/types.ts";
import { t } from "../i18n/index.ts";
import { daysLeftText, daysUntil, formatDate, formatDateTime, relativeTime, shortSerial } from "../lib/format.ts";
import { ReasonTags, StatusBadge } from "./StatusBadge.tsx";

/** 대상의 접속 주소별 최신 점검 결과. */
export function ResultsTable({ results, now }: { results: readonly CheckResult[]; now: number }) {
  if (results.length === 0) return <p className="muted">{t("results.empty")}</p>;
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th>{t("col.status")}</th>
            <th>{t("col.address")}</th>
            <th>{t("col.expires")}</th>
            <th>{t("col.issuer")}</th>
            <th>serial</th>
            <th>TLS</th>
            <th>{t("col.checked")}</th>
          </tr>
        </thead>
        <tbody>
          {results.map((r) => (
            <Fragment key={r.address}>
              <tr>
                <td><StatusBadge status={r.status} /></td>
                <td><code>{r.address}</code></td>
                <td>
                  {formatDate(r.not_after)}
                  {r.not_after && <div className="muted small-text">{daysLeftText(daysUntil(r.not_after, now))}</div>}
                </td>
                <td>{r.issuer || "-"}</td>
                <td><code title={r.serial}>{shortSerial(r.serial) || "-"}</code></td>
                <td>{r.tls_version || "-"}</td>
                <td className="nowrap" title={formatDateTime(r.checked_at)}>{relativeTime(r.checked_at, now)}</td>
              </tr>
              {(r.reasons.length > 0 || r.error) && (
                <tr className="detail-row">
                  <td colSpan={7}>
                    <ReasonTags reasons={r.reasons} />
                    {r.error && <span className="error-text">{r.error}</span>}
                  </td>
                </tr>
              )}
            </Fragment>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** 대상의 점검 이력(최신순). */
export function HistoryTable({ results }: { results: readonly CheckResult[] }) {
  if (results.length === 0) return <p className="muted">{t("history.empty")}</p>;
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th>{t("col.checkedAt")}</th>
            <th>{t("col.address")}</th>
            <th>{t("col.status")}</th>
            <th>{t("col.expires")}</th>
            <th>serial</th>
            <th>{t("col.reasons")}</th>
          </tr>
        </thead>
        <tbody>
          {results.map((r) => (
            <tr key={`${r.address}-${r.checked_at}`}>
              <td className="nowrap">{formatDateTime(r.checked_at)}</td>
              <td><code>{r.address}</code></td>
              <td><StatusBadge status={r.status} /></td>
              <td>{formatDate(r.not_after)}</td>
              <td><code title={r.serial}>{shortSerial(r.serial) || "-"}</code></td>
              <td>
                <ReasonTags reasons={r.reasons} />
                {r.error && <span className="error-text">{r.error}</span>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
