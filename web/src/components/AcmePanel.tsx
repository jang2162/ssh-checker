import { useState } from "react";
import { api } from "../api/endpoints.ts";
import type { Credentials, DomainDetail } from "../api/types.ts";
import { useAction } from "../app/useAction.ts";
import { AllowFromDialog } from "../dialogs/AllowFromDialog.tsx";
import { ConfirmDialog } from "../dialogs/ConfirmDialog.tsx";
import { CredentialsDialog } from "../dialogs/CredentialsDialog.tsx";
import { T, t, tn } from "../i18n/index.ts";
import { formatDateTime, relativeTime } from "../lib/format.ts";
import { cnameLabel } from "../lib/status.ts";
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
        <h2>{t("acme.title")}</h2>
        {admin && !acme.enabled && (
          <button type="button" className="btn primary small" onClick={() => setDialog("enable")}>{t("acme.enable")}</button>
        )}
        {admin && acme.enabled && (
          <div className="actions">
            <button type="button" className="btn small" disabled={check.busy} onClick={() => void checkCname()}>
              {check.busy ? t("acme.checking") : t("acme.checkCname")}
            </button>
            <button type="button" className="btn small" onClick={() => setDialog("allow")}>{t("acme.editAllow")}</button>
            <button type="button" className="btn small" onClick={() => setDialog("rotate")}>{t("acme.rotate")}</button>
            <button type="button" className="btn small danger-text" onClick={() => setDialog("disable")}>{t("acme.disable")}</button>
          </div>
        )}
      </div>
      {check.error && <Notice kind="error">{check.error}</Notice>}

      {!acme.enabled ? (
        <p className="muted">
          {t("acme.off")}
        </p>
      ) : (
        <dl className="kv">
          <dt>{t("acme.cnameRecord")}</dt>
          <dd>
            <code>{acme.cname_record}</code> <CopyButton text={acme.cname_record} />
          </dd>
          <dt>{t("acme.cnameCheck")}</dt>
          <dd>
            {cnameLabel(acme.cname_status)}
            {acme.cname_checked_at && <span className="muted"> · {when(acme.cname_checked_at, now)}</span>}
            {acme.cname_status === "mismatch" && acme.cname_value && (
              <div className="small-text">
                <T k="acme.cnameMismatch" vars={{ value: acme.cname_value }} />
              </div>
            )}
          </dd>
          <dt>{t("acme.lastUpdate")}</dt>
          <dd>{acme.last_update_at ? when(acme.last_update_at, now) : <span className="muted">{t("acme.notYet")}</span>}</dd>
          <dt>{t("acme.lastFailure")}</dt>
          <dd>
            {acme.last_failure_at ? when(acme.last_failure_at, now) : <span className="muted">{t("common.none")}</span>}
            {acme.consecutive_failures > 0 && <span className="error-text"> · {tn("acme.consecutiveFailures", acme.consecutive_failures)}</span>}
          </dd>
          <dt>{t("common.allowedIps")}</dt>
          <dd>
            {acme.allowfrom.length > 0 ? acme.allowfrom.join(", ") : <span className="warning-text">{t("common.unrestricted")}</span>}
          </dd>
          <dt>username</dt>
          <dd><code>{acme.username}</code></dd>
        </dl>
      )}

      {dialog === "enable" && (
        <AllowFromDialog
          title={t("acme.enable")}
          submitLabel={t("acme.enableSubmit")}
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
          title={t("acme.editAllow")}
          submitLabel={t("common.save")}
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
          title={t("acme.rotate")}
          confirmLabel={t("acme.rotateSubmit")}
          onClose={() => setDialog(null)}
          onConfirm={async () => {
            const value = await api.rotateAcme(domain.id);
            setCreds({ kind: "rotate", value });
            onChanged();
          }}
        >
          <p><T k="acme.rotateBody1" /></p>
          <p>{t("acme.rotateBody2")}</p>
        </ConfirmDialog>
      )}
      {dialog === "disable" && (
        <ConfirmDialog
          title={t("acme.disable")}
          confirmLabel={t("acme.disableSubmit")}
          danger
          onClose={() => setDialog(null)}
          onConfirm={async () => {
            await api.disableAcme(domain.id);
            onChanged();
          }}
        >
          <p>{t("acme.disableBody1")}</p>
          <p><T k="acme.disableBody2" /></p>
        </ConfirmDialog>
      )}
      {creds && <CredentialsDialog kind={creds.kind} creds={creds.value} onClose={() => setCreds(null)} />}
    </section>
  );
}
