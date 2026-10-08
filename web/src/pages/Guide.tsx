import { api } from "../api/endpoints.ts";
import { usePageTitle } from "../app/usePageTitle.ts";
import { useResource } from "../app/useResource.ts";
import { Notice } from "../components/Notice.tsx";
import { ServerScripts } from "../components/ServerScripts.tsx";
import { T, t } from "../i18n/index.ts";

/** 인증서를 받을 서버에서 실행하는 스크립트를 값을 비운 채로 보여 주는 안내 페이지다. 로그인 없이 볼 수 있다. */
export function Guide() {
  usePageTitle(t("nav.guide"));
  const { data, error, loading } = useResource(() => api.setup(), []);

  return (
    <>
      <div className="page-head">
        <h1>{t("nav.guide")}</h1>
      </div>
      <p className="lead">{t("guide.lead")}</p>

      {loading && !data && <p className="muted">{t("common.loading")}</p>}
      {error && !data && <Notice kind="error">{error}</Notice>}

      {data && (
        <>
          <section className="dialog-section">
            <h2>{t("guide.prepHeading")}</h2>
            <ol className="steps">
              <li><T k="guide.prep1" /></li>
              <li><T k="guide.prep2" /></li>
            </ol>
          </section>

          <section className="dialog-section">
            <h2>{t("guide.runHeading")}</h2>
            <ServerScripts setup={data.setup} servers={data.servers}>
              <h3>{t("guide.inputsHeading")}</h3>
              <ul className="plain">
                <li><T k="guide.inputCreds" /></li>
                <li><T k="guide.inputDomain" /></li>
                <li><T k="guide.inputRest" /></li>
              </ul>

              <h3>{t("guide.checksHeading")}</h3>
              <p className="muted small-text">{t("guide.checksHint")}</p>
              <ul className="checklist">
                <li><T k="guide.checkAllow" /></li>
                <li><T k="guide.checkAcmeSh" /></li>
                <li><T k="guide.checkWebServer" /></li>
                <li><T k="guide.checkCname" /></li>
                <li><T k="guide.checkCertDir" /></li>
                <li><T k="guide.checkWebConfig" /></li>
                <li><T k="guide.checkRenewal" /></li>
              </ul>
            </ServerScripts>
          </section>

          <section className="dialog-section">
            <h2>{t("guide.autoHeading")}</h2>
            <p>{t("guide.autoBody")}</p>
            <pre className="code">ACMEDNS_USERNAME=… ACMEDNS_PASSWORD=… DOMAIN=example.com bash setup.sh</pre>
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr><th>{t("guide.colEnv")}</th><th>{t("guide.colMeaning")}</th></tr>
                </thead>
                <tbody>
                  <tr><td><code>WEB_SERVER</code></td><td><T k="guide.envWebServer" /></td></tr>
                  <tr><td><code>RELOAD_CMD</code></td><td><T k="guide.envReloadCmd" /></td></tr>
                  <tr><td><code>CERT_DIR</code></td><td><T k="guide.envCertDir" /></td></tr>
                  <tr><td><code>WEB_CONF_DIR</code></td><td>{t("guide.envWebConfDir")}</td></tr>
                  <tr><td><code>FORCE=1</code></td><td>{t("guide.envForce")}</td></tr>
                  <tr><td><code>SKIP_CNAME_CHECK=1</code></td><td>{t("guide.envSkipCname")}</td></tr>
                </tbody>
              </table>
            </div>
            <p><T k="guide.sudo" /></p>
            <p className="muted small-text">{t("guide.secretHint")}</p>
          </section>
        </>
      )}
    </>
  );
}
