import { Link, NavLink, Outlet } from "react-router-dom";
import { useApp } from "../app/context.tsx";
import { t } from "../i18n/index.ts";

export function Layout() {
  const { ready, admin, logout } = useApp();
  return (
    <>
      <header className="topbar">
        <div className="topbar-inner">
          <Link to="/" className="brand">ssl-checker</Link>
          <nav aria-label={t("nav.main")}>
            <NavLink to="/" end>{t("nav.dashboard")}</NavLink>
            <NavLink to="/guide">{t("nav.guide")}</NavLink>
            {admin && <NavLink to="/audit">{t("nav.audit")}</NavLink>}
          </nav>
          <div className="spacer" />
          {/* 로그인 버튼은 두지 않는다. 관리자는 /admin으로 직접 들어온다. */}
          {ready && admin && (
            <button type="button" className="btn" onClick={() => void logout()}>{t("nav.logout")}</button>
          )}
        </div>
      </header>
      <main className="page">
        <Outlet />
      </main>
    </>
  );
}
