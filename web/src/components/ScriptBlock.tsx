import type { SetupScript } from "../api/types.ts";
import { CopyButton } from "./CopyButton.tsx";

/**
 * 서버에서 실행할 명령과, 내려받는 스크립트 내용(펼쳐서 미리 읽어 볼 수 있다)을 보여 준다.
 * cmd를 주면 script.cmd 대신 그 명령을 보여 준다(경로나 FORCE=1을 붙인 명령).
 */
export function ScriptBlock({ script, cmd = script.cmd }: { script: SetupScript; cmd?: string }) {
  return (
    <>
      <pre className="code">{cmd}</pre>
      <CopyButton text={cmd} label="명령 복사" />
      <details className="script-view">
        <summary>스크립트 내용 보기</summary>
        <pre className="code">{script.script}</pre>
        <CopyButton text={script.script} label="스크립트 복사" />
      </details>
    </>
  );
}
