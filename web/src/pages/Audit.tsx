import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/endpoints.ts";
import type { AuditEntry } from "../api/types.ts";
import { useApp } from "../app/context.tsx";
import { useAction } from "../app/useAction.ts";
import { usePageTitle } from "../app/usePageTitle.ts";
import { useResource } from "../app/useResource.ts";
import { Notice } from "../components/Notice.tsx";
import { formatDateTime } from "../lib/format.ts";

const PAGE = 100;

export function Audit() {
  usePageTitle("감사 로그");
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

  if (!ready) return <p className="muted">불러오는 중…</p>;
  if (!admin) return <Notice kind="info">감사 로그는 관리자만 볼 수 있습니다. <Link to="/admin">관리자 로그인</Link></Notice>;

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
        <h1>감사 로그</h1>
      </div>
      {error && <Notice kind="error">{error}</Notice>}
      {loading && !data && <p className="muted">불러오는 중…</p>}
      {data && entries.length === 0 && <div className="empty"><p>기록이 없습니다.</p></div>}
      {entries.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>시각</th>
                <th>동작</th>
                <th>대상</th>
                <th>사용자</th>
                <th>IP</th>
                <th>상세</th>
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
          <button type="button" className="btn" disabled={more.busy} onClick={() => void loadMore()}>더 보기</button>
        </div>
      )}
    </>
  );
}
