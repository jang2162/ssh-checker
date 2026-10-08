import type { Credentials } from "../api/types.ts";
import { CopyButton } from "../components/CopyButton.tsx";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";
import { ServerScripts } from "../components/ServerScripts.tsx";
import { T, t } from "../i18n/index.ts";

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
    <Dialog title={kind === "enable" ? t("creds.titleEnable") : t("creds.titleRotate")} onClose={onClose} wide persistent>
      <Notice kind="warning">
        <T k="creds.warning" />
      </Notice>

      {kind === "enable" ? (
        <section className="dialog-section">
          <h3>{t("creds.cnameHeading")}</h3>
          <p>{t("creds.cnameBody")}</p>
          <div className="copy-row">
            <code>{creds.cname_record}</code>
            <CopyButton text={creds.cname_record} />
          </div>
        </section>
      ) : (
        <section className="dialog-section">
          <h3>{t("creds.cnameSameHeading")}</h3>
          <p>{t("creds.cnameSameBody")}</p>
        </section>
      )}

      <section className="dialog-section">
        <h3>{kind === "enable" ? "2. " : ""}{t("creds.credentials")}</h3>
        <dl className="kv">
          <dt>username</dt>
          <dd><code>{creds.username}</code> <CopyButton text={creds.username} /></dd>
          <dt>password</dt>
          <dd><code>{creds.password}</code> <CopyButton text={creds.password} /></dd>
          <dt>subdomain</dt>
          <dd><code>{creds.subdomain}</code></dd>
          <dt>{t("common.allowedIps")}</dt>
          <dd>{creds.allowfrom.length > 0 ? creds.allowfrom.join(", ") : t("common.unrestricted")}</dd>
        </dl>
      </section>

      <section className="dialog-section">
        <h3>{kind === "enable" ? "3. " : ""}{t("creds.runHeading")}</h3>
        {creds.rotated && (
          <Notice kind="warning">
            <T k="creds.rotatedNotice" />
          </Notice>
        )}
        <p>
          <T k="creds.prompts" />
        </p>
        <p className="muted small-text"><T k="creds.manual" /></p>
        <ServerScripts setup={creds.setup.setup} servers={creds.setup.servers} force={creds.rotated} />
      </section>

      <div className="actions">
        <button type="button" className="btn primary" onClick={onClose}>{t("creds.close")}</button>
      </div>
    </Dialog>
  );
}
