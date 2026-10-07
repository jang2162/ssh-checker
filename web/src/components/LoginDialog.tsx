import { useState, type FormEvent } from "react";
import { ApiError } from "../api/client.ts";
import { useApp } from "../app/context.tsx";
import { errorMessage } from "../lib/errors.ts";
import { Dialog } from "./Dialog.tsx";
import { Notice } from "./Notice.tsx";

export function LoginDialog({ onClose }: { onClose: () => void }) {
  const { login } = useApp();
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await login(key);
      onClose();
    } catch (err) {
      // 로그인 화면의 401은 "세션 만료"가 아니라 "키가 틀림"이다.
      setError(err instanceof ApiError && err.status === 401 ? "관리자 키가 맞지 않습니다." : errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog title="관리자 로그인" onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>관리자 키 (ADMIN_KEY)</span>
          <input type="password" value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" autoFocus required />
        </label>
        <p className="muted small-text">로그인하면 12시간 동안 유지됩니다. 공용 PC에서는 사용 뒤 로그아웃하세요.</p>
        {error && <Notice kind="error">{error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>취소</button>
          <button type="submit" className="btn primary" disabled={busy || key === ""}>로그인</button>
        </div>
      </form>
    </Dialog>
  );
}
