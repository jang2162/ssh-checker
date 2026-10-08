import { createClient, type Client } from "./client.ts";
import type {
  AcmeOn,
  AuditEntry,
  CheckResult,
  CodeInfo,
  Config,
  Credentials,
  DomainDetail,
  DomainSummary,
  Setup,
  Summary,
  Target,
  TargetInput,
} from "./types.ts";

/** /api 엔드포인트를 타입이 있는 함수로 감싼다. 경로와 본문 모양은 docs/guide/api.md와 같다. */
export function createApi(c: Client) {
  return {
    summary: () => c.get<Summary>("/api/summary"),
    config: () => c.get<Config>("/api/config"),
    setup: () => c.get<Setup>("/api/setup"),
    codes: () => c.get<{ codes: CodeInfo[] }>("/api/codes").then((r) => r.codes),
    domains: () => c.get<{ domains: DomainSummary[] }>("/api/domains").then((r) => r.domains),
    domain: (id: number) => c.get<DomainDetail>(`/api/domains/${id}`),
    targetResults: (targetId: number, limit = 50) =>
      c.get<{ results: CheckResult[] }>(`/api/targets/${targetId}/results?limit=${limit}`).then((r) => r.results),
    audit: (beforeId?: number, limit = 100) => {
      const q = new URLSearchParams({ limit: String(limit) });
      if (beforeId !== undefined) q.set("before_id", String(beforeId));
      return c.get<{ entries: AuditEntry[] }>(`/api/audit?${q}`).then((r) => r.entries);
    },

    session: () => c.get<{ admin: boolean }>("/api/session").then((r) => r.admin),
    login: (key: string) => c.send<{ admin: boolean }>("POST", "/api/session", { key }),
    logout: () => c.send<void>("DELETE", "/api/session"),

    createDomain: (fqdn: string, memo: string) => c.send<DomainDetail>("POST", "/api/domains", { fqdn, memo }),
    patchDomain: (id: number, memo: string) => c.send<DomainDetail>("PATCH", `/api/domains/${id}`, { memo }),
    deleteDomain: (id: number) => c.send<void>("DELETE", `/api/domains/${id}`),

    createTarget: (domainId: number, t: TargetInput) => c.send<Target>("POST", `/api/domains/${domainId}/targets`, t),
    patchTarget: (id: number, t: TargetInput) => c.send<Target>("PATCH", `/api/targets/${id}`, t),
    deleteTarget: (id: number) => c.send<void>("DELETE", `/api/targets/${id}`),
    checkTarget: (id: number) => c.send<{ results: CheckResult[] }>("POST", `/api/targets/${id}/check`),

    enableAcme: (domainId: number, allowfrom: string[]) =>
      c.send<Credentials>("POST", `/api/domains/${domainId}/acme`, { allowfrom }),
    patchAcme: (domainId: number, allowfrom: string[]) =>
      c.send<AcmeOn>("PATCH", `/api/domains/${domainId}/acme`, { allowfrom }),
    rotateAcme: (domainId: number) => c.send<Credentials>("POST", `/api/domains/${domainId}/acme/rotate`),
    checkAcme: (domainId: number) => c.send<AcmeOn>("POST", `/api/domains/${domainId}/acme/check`),
    disableAcme: (domainId: number) => c.send<void>("DELETE", `/api/domains/${domainId}/acme`),
  };
}

export const api = createApi(createClient());
