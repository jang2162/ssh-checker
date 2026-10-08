/**
 * 이미 인증서가 있어도 다시 발급하도록 내려받기 명령 뒤의 실행 부분 앞에 FORCE=1을 붙인다(키 재발급 직후).
 * "curl ... -o setup.sh && WEB_SERVER=nginx bash setup.sh" → "curl ... -o setup.sh && FORCE=1 WEB_SERVER=nginx bash setup.sh"
 */
export function withForce(cmd: string): string {
  return cmd.replace(/&& /, "&& FORCE=1 ");
}
