/** 셸에서 그대로 쓸 수 있게 값을 인용한다. 안전한 문자만 있으면 그대로 둔다. */
export function shellQuote(v: string): string {
  if (/^[A-Za-z0-9_@%+=:,./-]+$/.test(v)) return v;
  return `'${v.replace(/'/g, `'\\''`)}'`;
}

/**
 * "curl ... -o setup.sh && WEB_SERVER=nginx bash setup.sh"의 실행 부분 앞에 환경변수를 붙인다.
 * 예: withEnv(cmd, {CERT_DIR: "/srv/tls"}) → "curl ... && CERT_DIR=/srv/tls WEB_SERVER=nginx bash setup.sh"
 */
export function withEnv(cmd: string, env: Record<string, string>): string {
  const prefix = Object.entries(env)
    .map(([k, v]) => `${k}=${shellQuote(v)}`)
    .join(" ");
  if (prefix === "") return cmd;
  return cmd.replace(/&& /, `&& ${prefix} `);
}

/** 이미 인증서가 있어도 다시 발급하도록 FORCE=1을 붙인다(키 재발급 직후). */
export function withForce(cmd: string): string {
  return withEnv(cmd, { FORCE: "1" });
}
