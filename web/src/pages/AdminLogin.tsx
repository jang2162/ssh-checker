import { useState, type FormEvent } from "react";
import { Navigate } from "react-router-dom";
import { ApiError } from "../api/client.ts";
import { useApp } from "../app/context.tsx";
import { usePageTitle } from "../app/usePageTitle.ts";
import { Notice } from "../components/Notice.tsx";
import { t } from "../i18n/index.ts";
import { errorMessage } from "../lib/errors.ts";

/**
 * 관리자 로그인 화면(/admin). 상단 바에는 로그인 버튼을 두지 않아서, 관리자가 주소로 직접 들어온다.
 * 로그인하면(또는 이미 로그인돼 있으면) 대시보드로 보낸다.
 */
export function AdminLogin() {
  usePageTitle(t("login.title"));
  const { ready, admin, login } = useApp();
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!ready) return <p className="muted">{t("common.loading")}</p>;
  if (admin) return <Navigate to="/" replace />;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await login(key);
    } catch (err) {
      // 로그인 화면의 401은 "세션 만료"가 아니라 "키가 틀림"이다.
      setError(err instanceof ApiError && err.status === 401 ? t("login.wrongKey") : errorMessage(err));
      setBusy(false);
    }
  }

  return (
    <section className="card login-card">
      <h1>{t("login.title")}</h1>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>{t("login.keyLabel")}</span>
          <input type="password" value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" autoFocus required />
        </label>
        <p className="muted small-text">{t("login.hint")}</p>
        {error && <Notice kind="error">{error}</Notice>}
        <div className="actions">
          <button type="submit" className="btn primary" disabled={busy || key === ""}>{t("login.submit")}</button>
        </div>
      </form>
    </section>
  );
}
