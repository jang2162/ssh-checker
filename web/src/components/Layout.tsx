import { useState } from "react";
import { Link, NavLink, Outlet } from "react-router-dom";
import { useApp } from "../app/context.tsx";
import { LoginDialog } from "./LoginDialog.tsx";

export function Layout() {
  const { ready, admin, logout } = useApp();
  const [loginOpen, setLoginOpen] = useState(false);
  return (
    <>
      <header className="topbar">
        <div className="topbar-inner">
          <Link to="/" className="brand">ssl-checker</Link>
          <nav aria-label="주 메뉴">
            <NavLink to="/" end>대시보드</NavLink>
            {admin && <NavLink to="/audit">감사 로그</NavLink>}
          </nav>
          <div className="spacer" />
          {ready &&
            (admin ? (
              <button type="button" className="btn" onClick={() => void logout()}>로그아웃</button>
            ) : (
              <button type="button" className="btn primary" onClick={() => setLoginOpen(true)}>관리자 로그인</button>
            ))}
        </div>
      </header>
      <main className="page">
        <Outlet />
      </main>
      {loginOpen && <LoginDialog onClose={() => setLoginOpen(false)} />}
    </>
  );
}
