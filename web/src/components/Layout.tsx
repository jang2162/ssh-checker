import { Link, NavLink, Outlet } from "react-router-dom";
import { useApp } from "../app/context.tsx";

export function Layout() {
  const { ready, admin, logout } = useApp();
  return (
    <>
      <header className="topbar">
        <div className="topbar-inner">
          <Link to="/" className="brand">ssl-checker</Link>
          <nav aria-label="주 메뉴">
            <NavLink to="/" end>대시보드</NavLink>
            <NavLink to="/guide">이용 방법</NavLink>
            {admin && <NavLink to="/audit">감사 로그</NavLink>}
          </nav>
          <div className="spacer" />
          {/* 로그인 버튼은 두지 않는다. 관리자는 /admin으로 직접 들어온다. */}
          {ready && admin && (
            <button type="button" className="btn" onClick={() => void logout()}>로그아웃</button>
          )}
        </div>
      </header>
      <main className="page">
        <Outlet />
      </main>
    </>
  );
}
