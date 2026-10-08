import { useId, useState } from "react";
import type { ServerSetup, SetupScript } from "../api/types.ts";
import { CopyButton } from "./CopyButton.tsx";
import { ScriptBlock } from "./ScriptBlock.tsx";

interface Props {
  /** 세팅 스크립트. 웹서버와 상관없이 같은 파일이다. */
  setup: SetupScript;
  servers: ServerSetup[];
  force?: boolean;
}

/** 웹서버(nginx, Apache, Caddy)를 고르면 그 서버용 실행 명령과 웹서버 설정 예를 보여 준다. */
export function ServerScripts({ setup, servers, force = false }: Props) {
  const [id, setId] = useState(servers[0]?.id ?? "");
  const panelId = useId();
  const current = servers.find((s) => s.id === id) ?? servers[0];
  if (!current) return null;
  return (
    <>
      <div className="tabs" role="tablist" aria-label="웹서버">
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
        <ScriptBlock script={{ ...setup, cmd: current.cmd }} force={force} />
        <h3>웹서버 설정 예</h3>
        <pre className="code">{current.config}</pre>
        <CopyButton text={current.config} label="설정 복사" />
      </div>
    </>
  );
}
