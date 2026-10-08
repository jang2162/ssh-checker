// /api 응답 모양. 서버의 internal/adminapi와 internal/monitor가 만드는 JSON과 같아야 한다.

export type Status = "critical" | "warning" | "info" | "ok" | "unknown";
export type CnameStatus = "unknown" | "ok" | "missing" | "mismatch" | "error";

export interface Summary {
  domains: number;
  critical: number;
  warning: number;
  info: number;
  ok: number;
  unknown: number;
  checked_at: string | null;
}

export interface Config {
  challenge_zone: string;
  check_interval: string; // Go 시간 문자열. 예: "6h0m0s"
  public_url: string;
}

export interface CodeInfo {
  code: string;
  status: Status;
  meaning: string;
}

export interface DomainSummary {
  id: number;
  fqdn: string;
  memo: string;
  status: Status;
  reasons: string[];
  earliest_not_after: string | null;
  days_left: number | null;
  targets: number;
  acme: { enabled: boolean; cname_status?: CnameStatus };
  last_checked_at: string | null;
}

/** 접속 주소 하나의 점검 한 건. 인증서를 받지 못한 결과에는 not_before/not_after가 아예 없다. */
export interface CheckResult {
  address: string;
  checked_at: string;
  status: Status;
  reasons: string[];
  tls_version: string;
  cipher: string;
  subject_cn: string;
  sans: string[] | null; // 접속에 실패한 즉시 점검 응답에는 null이 올 수 있다
  issuer: string;
  serial: string;
  not_before?: string;
  not_after?: string;
  key_alg: string;
  key_bits: number;
  chain_ok: boolean;
  hostname_ok: boolean;
  error: string;
}

export interface Target {
  id: number;
  hostname: string;
  port: number;
  sni: string;
  ips: string[];
  memo: string;
  status: Status;
  reasons: string[];
  results: CheckResult[];
}

export interface AcmeOff {
  enabled: false;
}

export interface AcmeOn {
  enabled: true;
  username: string;
  subdomain: string;
  fulldomain: string;
  cname_record: string;
  allowfrom: string[];
  cname_status: CnameStatus;
  cname_value: string;
  cname_checked_at: string | null;
  last_update_at: string | null;
  last_failure_at: string | null;
  consecutive_failures: number;
}

export type Acme = AcmeOff | AcmeOn;

export interface DomainDetail {
  id: number;
  fqdn: string;
  memo: string;
  created_at: string;
  status: Status;
  reasons: string[];
  targets: Target[];
  acme: Acme;
}

/** ACME를 켜거나 키를 재발급할 때 한 번만 받는 자격 증명. password는 이 응답에만 있다. */
export interface Credentials {
  username: string;
  password: string;
  subdomain: string;
  fulldomain: string;
  cname_record: string;
  allowfrom: string[];
  /** acme.sh 설치와 웹서버별 세팅 스크립트. 비밀값은 들어 있지 않고, 서버에서 실행할 때 위 값을 입력한다. */
  setup: Setup;
  /** true이면 키 재발급 응답이다. 세팅 스크립트를 FORCE=1로 다시 실행해야 새 키가 저장된다. */
  rotated: boolean;
}

/** 서버에서 내려받아 실행하는 스크립트 하나. */
export interface SetupScript {
  id: string;
  label: string;
  /** 내려받는 주소(/acme/ 아래). */
  url: string;
  /** 서버에서 실행할 명령(내려받고 실행). */
  cmd: string;
  /** 스크립트 내용. 미리 읽어 볼 수 있다. */
  script: string;
}

/** 웹서버(nginx, apache, caddy) 하나의 세팅 명령과 웹서버 설정 예. 스크립트는 같고 WEB_SERVER만 다르다. */
export interface ServerSetup {
  id: string;
  label: string;
  cmd: string;
  config: string;
}

/** GET /api/setup. acme.sh 설치 스크립트, 세팅 스크립트, 웹서버별 실행 명령. */
export interface Setup {
  install: SetupScript;
  setup: SetupScript;
  servers: ServerSetup[];
}

export interface AuditEntry {
  id: number;
  at: string;
  actor: string;
  action: string;
  target: string;
  source_ip: string;
  detail: string;
}

export interface TargetInput {
  hostname: string;
  port: number;
  sni: string;
  ips: string[];
  memo: string;
}
