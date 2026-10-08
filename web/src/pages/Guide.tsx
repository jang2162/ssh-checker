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
              <li>인증서를 받을 서버에서 스크립트를 내려받아 실행하고, 물어보는 값을 입력합니다. 허용 IP와 CNAME 확인, acme.sh 설치(없으면), 발급, 설치, 갱신 등록까지 됩니다.</li>
              <li>웹서버 설정에 인증서 경로를 넣습니다.</li>
            </ol>
          </section>

          <section className="dialog-section">
            <h2>2. 서버에서 실행</h2>
            <p>아래 명령 하나로 내려받아 실행합니다. 값은 실행하면 차례로 물어봅니다.</p>
            <table className="table">
              <thead>
                <tr><th>하는 일</th><th>물어보는 값 / 확인</th></tr>
              </thead>
              <tbody>
                <tr><td>자격 증명 입력</td><td>ssl-checker 주소(내려받은 주소가 기본값), username, password(입력해도 보이지 않음). ACME를 켤 때 나온 값입니다. subdomain은 서버가 알려 주므로 입력하지 않습니다.</td></tr>
                <tr><td>허용 IP 확인</td><td>입력한 자격 증명이 맞고 <strong>이 서버의 IP가 허용 목록에 있는지</strong> 확인합니다. 아니면 이 서버의 IP를 알려 주고 멈춥니다.</td></tr>
                <tr><td>acme.sh 확인</td><td>3.1.4 이상이 설치되어 있는지 확인합니다. 없으면 만료 알림 메일 주소를 물어보고 설치합니다.</td></tr>
                <tr><td>웹서버 지정</td><td>설치된 nginx, apache, caddy를 감지해 기본값으로 제안합니다. 실행 파일과 설정 디렉터리를 확인합니다. 감지되지 않거나 직접 reload하려면 <code>manual</code>을 골라 <strong>reload 스크립트</strong>(또는 명령)를 입력합니다.</td></tr>
                <tr><td>CNAME 확인</td><td>도메인을 입력하면 <code>_acme-challenge.&lt;도메인&gt;</code>의 CNAME이 ACME를 켤 때 안내한 주소를 가리키는지 공개 DNS로 확인합니다. 없거나 다르면 등록할 레코드를 보여 주고, 등록한 뒤 Enter로 다시 확인합니다.</td></tr>
                <tr><td>인증서 저장 경로</td><td>도메인(예: <code>example.com</code>, <code>*.example.com</code>도 함께 받음)과 저장 디렉터리(기본 <code>/etc/ssl/&lt;도메인&gt;</code>), reload 명령을 정합니다.</td></tr>
                <tr><td>발급과 설치</td><td>발급해 저장하고 갱신될 때마다 reload합니다. 끝에 웹서버 설정이 이 인증서를 쓰는지 확인하고, 아니면 넣을 설정을 보여 줍니다.</td></tr>
              </tbody>
            </table>
            <ServerScripts setup={data.setup} servers={data.servers} />
          </section>

          <section className="dialog-section">
            <h2>자동화와 에이전트</h2>
            <p>
              터미널이 없으면 값을 환경변수로 넘기면 묻지 않습니다. 예:{" "}
              <code>ACMEDNS_USERNAME=… ACMEDNS_PASSWORD=… WEB_SERVER=nginx DOMAIN=example.com bash setup.sh</code>. 웹서버를 직접 정하려면 <code>WEB_SERVER=manual RELOAD_CMD=/usr/local/bin/reload-web.sh</code>, 저장 경로는 <code>CERT_DIR</code>, 웹서버 설정 위치는 <code>WEB_CONF_DIR</code>로 넘깁니다.
              키를 재발급한 뒤에는 <code>FORCE=1</code>을 붙여 다시 실행합니다.
              비밀번호는 셸 기록에 남지 않게 주의하세요.
            </p>
          </section>
        </>
      )}
    </>
  );
}
