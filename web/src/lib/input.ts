/** 줄바꿈·쉼표·공백으로 나눈 목록. 빈 항목과 중복은 버리고 순서는 유지한다. */
export function splitList(text: string): string[] {
  const out: string[] = [];
  for (const item of text.split(/[\s,]+/)) {
    if (item !== "" && !out.includes(item)) out.push(item);
  }
  return out;
}

/** 1~65535인 정수만 포트로 받는다. */
export function parsePort(text: string): number | null {
  const t = text.trim();
  if (!/^\d{1,5}$/.test(t)) return null;
  const n = Number(t);
  return n >= 1 && n <= 65535 ? n : null;
}
