import { Link } from "react-router-dom";
import { usePageTitle } from "../app/usePageTitle.ts";

export function NotFound() {
  usePageTitle("찾을 수 없음");
  return (
    <div className="empty">
      <h1>페이지를 찾을 수 없습니다</h1>
      <p><Link to="/">대시보드로 가기</Link></p>
    </div>
  );
}
