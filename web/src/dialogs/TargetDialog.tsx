import { useState, type FormEvent } from "react";
import { api } from "../api/endpoints.ts";
import type { Target } from "../api/types.ts";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";
import { parsePort, splitList } from "../lib/input.ts";

interface Props {
  domainId: number;
  /** 있으면 수정, 없으면 추가. */
  target?: Target;
  onClose: () => void;
  onSaved: () => void;
}

export function TargetDialog({ domainId, target, onClose, onSaved }: Props) {
  const [hostname, setHostname] = useState(target?.hostname ?? "");
  const [port, setPort] = useState(String(target?.port ?? 443));
  const [sni, setSni] = useState(target?.sni ?? "");
  const [ips, setIps] = useState((target?.ips ?? []).join("\n"));
  const [memo, setMemo] = useState(target?.memo ?? "");
  const [localError, setLocalError] = useState<string | null>(null);
  const { run, busy, error } = useAction();

  async function submit(e: FormEvent) {
    e.preventDefault();
    const p = parsePort(port);
    if (p === null) {
      setLocalError("포트는 1~65535 사이의 정수여야 합니다.");
      return;
    }
    setLocalError(null);
    const input = { hostname: hostname.trim(), port: p, sni: sni.trim(), ips: splitList(ips), memo };
    const ok = await run(async () => {
      if (target) await api.patchTarget(target.id, input);
      else await api.createTarget(domainId, input);
      return true;
    });
    if (ok) {
      onSaved();
      onClose();
    }
  }

  return (
    <Dialog title={target ? "대상 수정" : "모니터링 대상 추가"} onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>호스트명</span>
          <input value={hostname} onChange={(e) => setHostname(e.target.value)} placeholder="www.a.com" autoFocus required />
        </label>
        <label className="field">
          <span>포트</span>
          <input value={port} onChange={(e) => setPort(e.target.value)} inputMode="numeric" required />
        </label>
        <label className="field">
          <span>SNI (선택)</span>
          <input value={sni} onChange={(e) => setSni(e.target.value)} placeholder="비우면 호스트명" />
        </label>
        <label className="field">
          <span>서버 IP (선택, 한 줄에 하나)</span>
          <textarea value={ips} onChange={(e) => setIps(e.target.value)} rows={3} placeholder={"10.0.1.10\n10.0.1.11"} />
          <small>
            비우면 호스트명으로 한 번만 접속합니다(DNS가 여러 주소를 줘도 하나만 점검). 서버마다 같은 인증서가 깔렸는지 보려면 IP를 모두 적습니다(최대 16개).
          </small>
        </label>
        <label className="field">
          <span>메모 (선택)</span>
          <input value={memo} onChange={(e) => setMemo(e.target.value)} maxLength={500} />
        </label>
        <p className="muted small-text">저장하면 바로 한 번 점검합니다. 접속 설정을 바꾸면 옛 주소의 결과는 더 이상 보이지 않습니다.</p>
        {(localError ?? error) && <Notice kind="error">{localError ?? error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>취소</button>
          <button type="submit" className="btn primary" disabled={busy || hostname.trim() === ""}>
            {busy ? "점검 중…" : "저장"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
