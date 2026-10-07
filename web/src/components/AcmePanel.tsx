import { useState } from "react";
import { api } from "../api/endpoints.ts";
import type { Credentials, DomainDetail } from "../api/types.ts";
import { useAction } from "../app/useAction.ts";
import { AllowFromDialog } from "../dialogs/AllowFromDialog.tsx";
import { ConfirmDialog } from "../dialogs/ConfirmDialog.tsx";
import { CredentialsDialog } from "../dialogs/CredentialsDialog.tsx";
import { formatDateTime, relativeTime } from "../lib/format.ts";
import { CNAME_LABEL } from "../lib/status.ts";
import { CopyButton } from "./CopyButton.tsx";
import { Notice } from "./Notice.tsx";

interface Props {
  domain: DomainDetail;
  admin: boolean;
  now: number;
  onChanged: () => void;
}

type DialogKind = "enable" | "allow" | "rotate" | "disable";

function when(iso: string | null, now: number): string {
  return iso ? `${formatDateTime(iso)} (${relativeTime(iso, now)})` : "";
}

export function AcmePanel({ domain, admin, now, onChanged }: Props) {
  const acme = domain.acme;
  const [dialog, setDialog] = useState<DialogKind | null>(null);
  const [creds, setCreds] = useState<{ kind: "enable" | "rotate"; value: Credentials } | null>(null);
  const check = useAction();

  async function checkCname() {
    const r = await check.run(() => api.checkAcme(domain.id));
    if (r) onChanged();
  }

  return (
    <section className="card">
      <div className="target-head">
        <h2>ACME (DNS-01 위임)</h2>
        {admin && !acme.enabled && (
          <button type="button" className="btn primary small" onClick={() => setDialog("enable")}>ACME 켜기</button>
        )}
        {admin && acme.enabled && (
          <div className="actions">
            <button type="button" className="btn small" disabled={check.busy} onClick={() => void checkCname()}>
              {check.busy ? "확인 중…" : "CNAME 지금 확인"}
            </button>
            <button type="button" className="btn small" onClick={() => setDialog("allow")}>허용 IP 수정</button>
            <button type="button" className="btn small" onClick={() => setDialog("rotate")}>키 재발급</button>
            <button type="button" className="btn small danger-text" onClick={() => setDialog("disable")}>ACME 끄기</button>
          </div>
        )}
      </div>
      {check.error && <Notice kind="error">{check.error}</Notice>}

      {!acme.enabled ? (
        <p className="muted">
          ACME 위임이 꺼져 있습니다. 켜면 이 도메인의 인증서를 DNS API 없이 acme.sh로 발급·갱신할 수 있습니다.
        </p>
      ) : (
        <dl className="kv">
          <dt>CNAME 레코드</dt>
          <dd>
            <code>{acme.cname_record}</code> <CopyButton text={acme.cname_record} />
          </dd>
          <dt>CNAME 확인</dt>
          <dd>
            {CNAME_LABEL[acme.cname_status]}
            {acme.cname_checked_at && <span className="muted"> · {when(acme.cname_checked_at, now)}</span>}
            {acme.cname_status === "mismatch" && acme.cname_value && (
              <div className="small-text">
                현재 값: <code>{acme.cname_value}</code>. 위 레코드의 값으로 고쳐야 합니다.
              </div>
            )}
          </dd>
          <dt>마지막 TXT 갱신</dt>
          <dd>{acme.last_update_at ? when(acme.last_update_at, now) : <span className="muted">아직 없음</span>}</dd>
          <dt>마지막 갱신 실패</dt>
          <dd>
            {acme.last_failure_at ? when(acme.last_failure_at, now) : <span className="muted">없음</span>}
            {acme.consecutive_failures > 0 && <span className="error-text"> · 연속 {acme.consecutive_failures}회 실패</span>}
          </dd>
          <dt>허용 IP</dt>
          <dd>
            {acme.allowfrom.length > 0 ? acme.allowfrom.join(", ") : <span className="warning-text">제한 없음</span>}
          </dd>
          <dt>username</dt>
          <dd><code>{acme.username}</code></dd>
        </dl>
      )}

      {dialog === "enable" && (
        <AllowFromDialog
          title="ACME 켜기"
          submitLabel="켜기"
          initial={[]}
          onClose={() => setDialog(null)}
          onSubmit={async (allowfrom) => {
            const value = await api.enableAcme(domain.id, allowfrom);
            setCreds({ kind: "enable", value });
            onChanged();
          }}
        />
      )}
      {dialog === "allow" && acme.enabled && (
        <AllowFromDialog
          title="허용 IP 수정"
          submitLabel="저장"
          initial={acme.allowfrom}
          onClose={() => setDialog(null)}
          onSubmit={async (allowfrom) => {
            await api.patchAcme(domain.id, allowfrom);
            onChanged();
          }}
        />
      )}
      {dialog === "rotate" && (
        <ConfirmDialog
          title="키 재발급"
          confirmLabel="재발급"
          onClose={() => setDialog(null)}
          onConfirm={async () => {
            const value = await api.rotateAcme(domain.id);
            setCreds({ kind: "rotate", value });
            onChanged();
          }}
        >
          <p>새 키를 만듭니다. <strong>옛 키는 즉시 무효</strong>가 되어, 서버의 acme.sh에 새 키를 설정하기 전까지 갱신이 실패합니다.</p>
          <p>서브도메인과 CNAME은 그대로라 DNS는 바꾸지 않아도 됩니다.</p>
        </ConfirmDialog>
      )}
      {dialog === "disable" && (
        <ConfirmDialog
          title="ACME 끄기"
          confirmLabel="끄기"
          danger
          onClose={() => setDialog(null)}
          onConfirm={async () => {
            await api.disableAcme(domain.id);
            onChanged();
          }}
        >
          <p>계정을 폐기하고 챌린지 TXT를 지웁니다. 서버의 acme.sh 갱신이 더 이상 되지 않습니다.</p>
          <p>다시 켜면 서브도메인이 새로 생겨 <strong>CNAME을 다시 걸어야</strong> 합니다.</p>
        </ConfirmDialog>
      )}
      {creds && <CredentialsDialog kind={creds.kind} creds={creds.value} onClose={() => setCreds(null)} />}
    </section>
  );
}
