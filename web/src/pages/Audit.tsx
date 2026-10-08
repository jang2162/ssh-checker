import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/endpoints.ts";
import type { AuditEntry } from "../api/types.ts";
import { useApp } from "../app/context.tsx";
import { useAction } from "../app/useAction.ts";
import { usePageTitle } from "../app/usePageTitle.ts";
import { useResource } from "../app/useResource.ts";
import { Notice } from "../components/Notice.tsx";
import { T, t } from "../i18n/index.ts";
import { formatDateTime } from "../lib/format.ts";

const PAGE = 100;

export function Audit() {
  usePageTitle(t("nav.audit"));
  const { admin, ready } = useApp();
  const { data, error, loading } = useResource(() => (admin ? api.audit(undefined, PAGE) : Promise.resolve([])), [admin]);
  const [older, setOlder] = useState<AuditEntry[]>([]);
  const [exhausted, setExhausted] = useState(false);
  const more = useAction();

  // 첫 페이지를 다시 불러오면 "더 보기"로 붙인 것은 버린다.
  useEffect(() => {
    setOlder([]);
    setExhausted(false);
  }, [data]);

  if (!ready) return <p className="muted">{t("common.loading")}</p>;
  if (!admin) return <Notice kind="info"><T k="audit.adminOnly" vars={{ link: <Link to="/admin">{t("login.title")}</Link> }} /></Notice>;

  const entries = [...(data ?? []), ...older];
  const last = entries[entries.length - 1];

  async function loadMore() {
    if (!last) return;
    const page = await more.run(() => api.audit(last.id, PAGE));
    if (!page) return;
    setOlder((o) => [...o, ...page]);
    if (page.length < PAGE) setExhausted(true);
  }

  return (
    <>
      <div className="page-head">
        <h1>{t("nav.audit")}</h1>
      </div>
      {error && <Notice kind="error">{error}</Notice>}
      {loading && !data && <p className="muted">{t("common.loading")}</p>}
      {data && entries.length === 0 && <div className="empty"><p>{t("audit.empty")}</p></div>}
      {entries.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>{t("audit.colTime")}</th>
                <th>{t("audit.colAction")}</th>
                <th>{t("audit.colTarget")}</th>
                <th>{t("audit.colActor")}</th>
                <th>IP</th>
                <th>{t("audit.colDetail")}</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <tr key={e.id}>
                  <td className="nowrap">{formatDateTime(e.at)}</td>
                  <td><code>{e.action}</code></td>
                  <td>{e.target}</td>
                  <td>{e.actor}</td>
                  <td><code>{e.source_ip}</code></td>
                  <td className="muted">{e.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {more.error && <Notice kind="error">{more.error}</Notice>}
      {entries.length >= PAGE && !exhausted && (
        <div className="actions center">
          <button type="button" className="btn" disabled={more.busy} onClick={() => void loadMore()}>{t("audit.more")}</button>
        </div>
      )}
    </>
  );
}
