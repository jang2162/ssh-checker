import { useId, useState, type ReactNode } from "react";
import type { ServerSetup, SetupScript } from "../api/types.ts";
import { t } from "../i18n/index.ts";
import { withForce } from "../lib/command.ts";
import { CopyButton } from "./CopyButton.tsx";
import { ScriptBlock } from "./ScriptBlock.tsx";

interface Props {
  /** 설치·세팅 스크립트. 웹서버 감지, 경로 확인, 발급·설치를 모두 한다. */
  setup: SetupScript;
  /** 웹서버별 설정 예. 스크립트가 끝에 이 설정을 출력하므로 참고용이다. */
  servers: ServerSetup[];
  force?: boolean;
  /** 실행 명령 아래, 웹서버 설정 예 위에 넣을 안내. */
  children?: ReactNode;
}

/** 실행 명령은 하나이고, 웹서버(nginx, Apache, Caddy)별로는 서버 설정 예만 탭으로 보여 준다. */
export function ServerScripts({ setup, servers, force = false, children }: Props) {
  const [id, setId] = useState(servers[0]?.id ?? "");
  const panelId = useId();
  const current = servers.find((s) => s.id === id) ?? servers[0];
  const cmd = force ? withForce(setup.cmd) : setup.cmd;
  return (
    <>
      <ScriptBlock script={setup} cmd={cmd} />
      {children}
      {current && (
        <>
          <h3>{t("servers.heading")}</h3>
          <p className="muted small-text">{t("servers.hint")}</p>
          <div className="tabs" role="tablist" aria-label={t("servers.tabs")}>
            {servers.map((s) => (
              <button
                key={s.id}
                type="button"
                role="tab"
                className="btn"
                aria-selected={s.id === current.id}
                aria-controls={panelId}
                onClick={() => setId(s.id)}
              >
                {s.label}
              </button>
            ))}
          </div>
          <div id={panelId} role="tabpanel">
            <pre className="code">{current.config}</pre>
            <CopyButton text={current.config} label={t("copy.config")} />
          </div>
        </>
      )}
    </>
  );
}
