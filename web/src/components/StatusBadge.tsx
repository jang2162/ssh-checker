import type { Status } from "../api/types.ts";
import { useApp } from "../app/context.tsx";
import { statusLabel } from "../lib/status.ts";

/** 색만으로 구분하지 않도록 글자를 함께 쓴다. */
export function StatusBadge({ status }: { status: Status }) {
  return <span className={`badge badge-${status}`}>{statusLabel(status)}</span>;
}

/** 판정 코드 태그. 마우스를 올리면 서버가 알려 준 설명이 보인다. */
export function ReasonTags({ reasons }: { reasons: readonly string[] }) {
  const { codes } = useApp();
  if (reasons.length === 0) return null;
  return (
    <span className="reasons">
      {reasons.map((code) => (
        <span key={code} className={`tag tag-${codes.get(code)?.status ?? "unknown"}`} title={codes.get(code)?.meaning}>
          {code}
        </span>
      ))}
    </span>
  );
}

/** 판정 코드와 그 뜻을 줄마다 보여 준다(도메인 상세 상단). */
export function ReasonList({ reasons }: { reasons: readonly string[] }) {
  const { codes } = useApp();
  if (reasons.length === 0) return null;
  return (
    <ul className="reason-list">
      {reasons.map((code) => (
        <li key={code}>
          <span className={`tag tag-${codes.get(code)?.status ?? "unknown"}`}>{code}</span>
          <span>{codes.get(code)?.meaning ?? ""}</span>
        </li>
      ))}
    </ul>
  );
}
