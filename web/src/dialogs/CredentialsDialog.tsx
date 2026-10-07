import type { Credentials } from "../api/types.ts";
import { CopyButton } from "../components/CopyButton.tsx";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";

interface Props {
  kind: "enable" | "rotate";
  creds: Credentials;
  onClose: () => void;
}

/**
 * ACME를 켜거나 키를 재발급한 직후 한 번만 보여 주는 창이다. 비밀번호는 이 응답에만 있어서
 * Esc로 실수로 닫히지 않게 하고, 버튼으로만 닫는다.
 */
export function CredentialsDialog({ kind, creds, onClose }: Props) {
  return (
    <Dialog title={kind === "enable" ? "ACME를 켰습니다" : "키를 재발급했습니다"} onClose={onClose} wide persistent>
      <Notice kind="warning">
        <strong>이 창을 닫으면 비밀번호를 다시 볼 수 없습니다.</strong> 지금 서버에 설정하세요. 잃어버리면 키를 재발급해야 합니다.
      </Notice>

      {kind === "enable" ? (
        <section className="dialog-section">
          <h3>1. CNAME 레코드 등록</h3>
          <p>도메인의 DNS에 아래 한 줄을 등록합니다. 한 번만 하면 됩니다.</p>
          <div className="copy-row">
            <code>{creds.cname_record}</code>
            <CopyButton text={creds.cname_record} />
          </div>
        </section>
      ) : (
        <section className="dialog-section">
          <h3>CNAME은 그대로입니다</h3>
          <p>서브도메인이 같아서 DNS는 바꾸지 않아도 됩니다.</p>
        </section>
      )}

      <section className="dialog-section">
        <h3>{kind === "enable" ? "2. " : ""}자격 증명</h3>
        <dl className="kv">
          <dt>username</dt>
          <dd><code>{creds.username}</code> <CopyButton text={creds.username} /></dd>
          <dt>password</dt>
          <dd><code>{creds.password}</code> <CopyButton text={creds.password} /></dd>
          <dt>subdomain</dt>
          <dd><code>{creds.subdomain}</code></dd>
          <dt>허용 IP</dt>
          <dd>{creds.allowfrom.length > 0 ? creds.allowfrom.join(", ") : "제한 없음"}</dd>
        </dl>
      </section>

      <section className="dialog-section">
        <h3>{kind === "enable" ? "3. " : ""}인증서를 받을 서버에서 실행</h3>
        {kind === "rotate" && (
          <Notice kind="warning">
            새 키를 acme.sh 설정에 저장하려면 아래 <code>--issue ... --force</code>를 서버에서 다시 실행해야 합니다. 그렇지 않으면 다음 자동 갱신이 401로 실패합니다.
          </Notice>
        )}
        <pre className="code">{creds.acme_sh}</pre>
        <CopyButton text={creds.acme_sh} label="명령 복사" />
        <p className="muted small-text">
          비밀번호가 셸 기록에 남지 않게 하려면 <code>export ACMEDNS_PASSWORD=...</code> 줄 대신{" "}
          <code>read -rs ACMEDNS_PASSWORD &amp;&amp; export ACMEDNS_PASSWORD</code>를 실행하고 비밀번호를 붙여 넣으세요.
        </p>
      </section>

      <div className="actions">
        <button type="button" className="btn primary" onClick={onClose}>비밀번호를 저장했습니다. 닫기</button>
      </div>
    </Dialog>
  );
}
