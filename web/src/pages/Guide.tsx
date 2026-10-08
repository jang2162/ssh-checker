import { api } from "../api/endpoints.ts";
import { usePageTitle } from "../app/usePageTitle.ts";
import { useResource } from "../app/useResource.ts";
import { Notice } from "../components/Notice.tsx";
import { ServerScripts } from "../components/ServerScripts.tsx";

/** 인증서를 받을 서버에서 실행하는 스크립트를 값을 비운 채로 보여 주는 안내 페이지다. 로그인 없이 볼 수 있다. */
export function Guide() {
  usePageTitle("이용 방법");
  const { data, error, loading } = useResource(() => api.setup(), []);

  return (
    <>
      <div className="page-head">
        <h1>이용 방법</h1>
      </div>
      <p className="lead">인증서를 받을 서버에서 명령 한 줄을 실행하면 설정이 끝납니다. 이후 갱신은 자동입니다.</p>

      {loading && !data && <p className="muted">불러오는 중…</p>}
      {error && !data && <Notice kind="error">{error}</Notice>}

      {data && (
        <>
          <section className="dialog-section">
            <h2>1. 준비 (관리자)</h2>
            <ol className="steps">
              <li>도메인을 등록하고 <strong>ACME를 켭니다.</strong> CNAME 레코드와 username, password가 한 번만 나옵니다.</li>
              <li>도메인의 DNS에 그 <strong>CNAME 한 줄</strong>을 등록합니다.</li>
            </ol>
          </section>

          <section className="dialog-section">
            <h2>2. 서버에서 실행</h2>
            <ServerScripts setup={data.setup} servers={data.servers}>
              <h3>입력하는 값</h3>
              <ul className="plain">
                <li><strong>username, password</strong>: ACME를 켤 때 나온 값입니다. password는 입력해도 보이지 않습니다.</li>
                <li><strong>도메인</strong>: 인증서를 받을 도메인(예: <code>example.com</code>). <code>*.example.com</code>도 함께 받습니다.</li>
                <li>나머지는 기본값이 채워져 있어 <strong>Enter</strong>만 누르면 됩니다.</li>
              </ul>

              <h3>스크립트가 확인하는 것</h3>
              <p className="muted small-text">문제가 있으면 이유를 알려 주고 발급 전에 멈춥니다.</p>
              <ul className="checklist">
                <li><strong>허용 IP</strong>: 이 서버의 IP가 허용 목록에 있는지</li>
                <li><strong>acme.sh</strong>: 3.1.4 이상이 설치되어 있는지. 없으면 설치합니다.</li>
                <li><strong>웹서버</strong>: nginx, apache, caddy를 감지하고 설치 위치를 확인합니다. 감지되지 않으면 <code>manual</code>을 골라 reload 스크립트를 입력합니다.</li>
                <li><strong>CNAME</strong>: <code>_acme-challenge.&lt;도메인&gt;</code>이 맞게 등록되었는지. 없으면 등록할 레코드를 보여 주고, 등록한 뒤 Enter로 다시 확인합니다.</li>
                <li><strong>저장 경로</strong>: 기본 <code>/etc/ssl/&lt;도메인&gt;</code>. 발급, 설치, 갱신 시 reload까지 합니다.</li>
                <li><strong>웹서버 설정</strong>: 이 인증서를 이미 쓰는지. 아니면 넣을 설정을 보여 줍니다.</li>
              </ul>
            </ServerScripts>
          </section>

          <section className="dialog-section">
            <h2>3. 자동화와 에이전트</h2>
            <p>터미널이 없으면 값을 환경변수로 넘깁니다. 넘긴 값은 묻지 않습니다.</p>
            <pre className="code">ACMEDNS_USERNAME=… ACMEDNS_PASSWORD=… DOMAIN=example.com bash setup.sh</pre>
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr><th>환경변수</th><th>뜻</th></tr>
                </thead>
                <tbody>
                  <tr><td><code>WEB_SERVER</code></td><td><code>nginx</code>, <code>apache</code>, <code>caddy</code>, <code>manual</code>. 기본은 감지된 웹서버입니다.</td></tr>
                  <tr><td><code>RELOAD_CMD</code></td><td>설치 뒤 실행할 reload 스크립트나 명령. <code>manual</code>이면 필수입니다.</td></tr>
                  <tr><td><code>CERT_DIR</code></td><td>인증서 저장 경로. 기본 <code>/etc/ssl/&lt;도메인&gt;</code></td></tr>
                  <tr><td><code>WEB_CONF_DIR</code></td><td>웹서버 설정 디렉터리(설정 반영 확인용)</td></tr>
                  <tr><td><code>FORCE=1</code></td><td>키를 재발급한 뒤 다시 실행할 때</td></tr>
                  <tr><td><code>SKIP_CNAME_CHECK=1</code></td><td>CNAME 확인을 건너뜁니다(사내 DNS만 쓸 때 등)</td></tr>
                </tbody>
              </table>
            </div>
            <p className="muted small-text">비밀번호는 셸 기록에 남지 않게 주의하세요. 터미널에서 실행하면 입력으로 받는 편이 안전합니다.</p>
          </section>
        </>
      )}
    </>
  );
}
