import { api } from "../api/endpoints.ts";
import { usePageTitle } from "../app/usePageTitle.ts";
import { useResource } from "../app/useResource.ts";
import { ScriptBlock } from "../components/ScriptBlock.tsx";
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
      <p>
        인증서를 받을 서버에서 아래 순서로 한 번 설정하면, 이후 갱신은 자동으로 됩니다.
        스크립트에는 비밀값이 들어 있지 않습니다. 서버에서 실행하면 값을 물어보고, 그 값은 관리자가 도메인에서{" "}
        <strong>ACME를 켤 때</strong> 나오는 창에서 받습니다.
      </p>

      {loading && !data && <p className="muted">불러오는 중…</p>}
      {error && !data && <Notice kind="error">{error}</Notice>}

      {data && (
        <>
          <section className="dialog-section">
            <h2>1. 순서</h2>
            <ol className="steps">
              <li>관리자가 도메인을 등록하고 ACME를 켭니다. 화면에 CNAME 레코드와 자격 증명이 한 번만 나옵니다.</li>
              <li>도메인의 DNS에 그 CNAME 한 줄을 등록합니다.</li>
              <li>인증서를 받을 서버에 acme.sh를 설치합니다(서버마다 한 번).</li>
              <li>세팅 스크립트를 내려받아 실행하고, 물어보는 값을 입력합니다. 발급, 설치, 갱신 등록까지 됩니다.</li>
              <li>웹서버 설정에 인증서 경로를 넣습니다.</li>
            </ol>
          </section>

          <section className="dialog-section">
            <h2>2. acme.sh 설치</h2>
            <p>서버마다 한 번만 실행합니다. 이미 설치되어 있고 3.1.4 이상이면 건너뜁니다. 만료 알림 메일 주소를 물어봅니다.</p>
            <ScriptBlock script={data.install} />
          </section>

          <section className="dialog-section">
            <h2>3. 세팅 (발급과 설치)</h2>
            <p>웹서버를 고르세요. 아래 값을 차례로 물어봅니다.</p>
            <table className="table">
              <thead>
                <tr><th>물어보는 값</th><th>무엇을 넣나</th></tr>
              </thead>
              <tbody>
                <tr><td>웹서버</td><td>nginx, apache, caddy 중 하나. 아래 탭의 명령은 <code>WEB_SERVER=…</code>로 미리 정해 둡니다.</td></tr>
                <tr><td>ssl-checker 주소</td><td>스크립트를 내려받은 주소가 기본값입니다. 그대로 Enter.</td></tr>
                <tr><td>username, password, subdomain</td><td>ACME를 켤 때 나온 값. password는 입력해도 보이지 않습니다.</td></tr>
                <tr><td>도메인</td><td>인증서를 받을 도메인(예: <code>example.com</code>). <code>*.example.com</code>도 함께 받습니다.</td></tr>
                <tr><td>인증서 저장 경로</td><td>기본값 <code>/etc/ssl/&lt;도메인&gt;</code>. 웹서버 설정에 넣을 경로입니다.</td></tr>
                <tr><td>reload 명령</td><td>웹서버별 기본값이 채워져 있습니다. 다르면 바꿉니다.</td></tr>
              </tbody>
            </table>
            <p>
              실행하면 먼저 acme.sh가 설치되어 있는지(3.1.4 이상) 확인하고, 자격 증명을 입력하면 <strong>이 서버의 IP가 허용 IP 목록에 있는지</strong>도
              확인합니다. 설치가 안 되어 있거나 허용 IP가 아니면 발급 전에 이유를 알려 주고 멈춥니다.
            </p>
            <ServerScripts setup={data.setup} servers={data.servers} />
          </section>

          <section className="dialog-section">
            <h2>자동화와 에이전트</h2>
            <p>
              터미널이 없으면 값을 환경변수로 넘기면 묻지 않습니다. 예:{" "}
              <code>ACMEDNS_USERNAME=… ACMEDNS_PASSWORD=… ACMEDNS_SUBDOMAIN=… WEB_SERVER=nginx DOMAIN=example.com bash setup.sh</code>.
              키를 재발급한 뒤에는 <code>FORCE=1</code>을 붙여 다시 실행합니다.
              비밀번호는 셸 기록에 남지 않게 주의하세요.
            </p>
          </section>
        </>
      )}
    </>
  );
}
