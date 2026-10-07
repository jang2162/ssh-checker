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
import { NotFound } from "./NotFound.tsx";

export function DomainDetail() {
  const params = useParams();
  const id = Number(params.id);
  const valid = Number.isInteger(id) && id > 0;
  const { admin } = useApp();
  const navigate = useNavigate();
  const [dialog, setDialog] = useState<"memo" | "target" | "delete" | null>(null);

  const { data, error, loading, reload } = useResource(() => api.domain(id), [id], REFRESH_MS);
  usePageTitle(data?.fqdn ?? "도메인");

  if (!valid) return <NotFound />;
  if (loading && !data) return <p className="muted">불러오는 중…</p>;
  if (!data) {
    return (
      <>
        <Notice kind="error">{error}</Notice>
        <p><Link to="/">← 대시보드로</Link></p>
      </>
    );
  }

  const now = Date.now();
  const onChanged = () => void reload();
  return (
    <>
      <p className="crumb"><Link to="/">← 대시보드</Link></p>
      <div className="page-head">
        <div>
          <h1>
            {data.fqdn} <StatusBadge status={data.status} />
          </h1>
          <div className="muted">
            {data.memo || "메모 없음"}
          </div>
        </div>
        {admin && (
          <div className="actions">
            <button type="button" className="btn" onClick={() => setDialog("memo")}>메모 수정</button>
            <button type="button" className="btn danger-text" onClick={() => setDialog("delete")}>도메인 삭제</button>
          </div>
        )}
      </div>

      {error && <Notice kind="error">새로 고치지 못했습니다: {error}</Notice>}
      <ReasonList reasons={data.reasons} />

      <div className="section-head">
        <h2>모니터링 대상</h2>
        {admin && (
          <button type="button" className="btn primary small" onClick={() => setDialog("target")}>대상 추가</button>
        )}
      </div>
      {data.targets.length === 0 ? (
        <div className="empty">
          <p>모니터링 대상이 없습니다.{admin ? " \"대상 추가\"로 점검할 호스트를 등록하세요." : ""}</p>
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
          title="도메인 삭제"
          confirmLabel="삭제"
          danger
          onClose={() => setDialog(null)}
          onConfirm={async () => {
            await api.deleteDomain(data.id);
            navigate("/");
          }}
        >
          <p><strong>{data.fqdn}</strong>과 모든 대상, 점검 이력을 삭제합니다.</p>
          {data.acme.enabled && <p>ACME가 켜져 있어서 먼저 끄고 챌린지 TXT를 지웁니다. 서버의 acme.sh 갱신은 더 이상 되지 않습니다.</p>}
        </ConfirmDialog>
      )}
    </>
  );
}
