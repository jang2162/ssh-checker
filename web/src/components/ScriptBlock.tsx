import type { SetupScript } from "../api/types.ts";
import { withForce } from "../lib/command.ts";
import { CopyButton } from "./CopyButton.tsx";

/**
 * 서버에서 실행할 명령과, 내려받는 스크립트 내용(펼쳐서 미리 읽어 볼 수 있다)을 보여 준다.
 * force이면 이미 인증서가 있어도 다시 발급하도록 FORCE=1을 붙인다(키 재발급 직후).
 */
export function ScriptBlock({ script, force = false }: { script: SetupScript; force?: boolean }) {
  const cmd = force ? withForce(script.cmd) : script.cmd;
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
