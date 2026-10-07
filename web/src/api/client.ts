/** 서버가 {"error":"<코드>"}로 돌려준 실패. status가 0이면 서버에 닿지 못한 것이다. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string) {
    super(`${status} ${code}`);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

export type FetchFn = (input: string, init?: RequestInit) => Promise<Response>;

export interface Client {
  get<T>(path: string): Promise<T>;
  send<T>(method: "POST" | "PATCH" | "DELETE", path: string, body?: unknown): Promise<T>;
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null;
}

/**
 * fetch 위의 얇은 JSON 클라이언트다. 서버는 GET·HEAD가 아닌 요청에 Content-Type: application/json을
 * 요구하므로(다른 사이트의 폼 위조를 막는다) 본문이 없는 DELETE에도 헤더를 붙인다.
 */
export function createClient(fetchFn: FetchFn = (input, init) => fetch(input, init)): Client {
  async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Accept: "application/json" };
    const init: RequestInit = { method, headers, credentials: "same-origin" };
    if (method !== "GET") {
      headers["Content-Type"] = "application/json";
      if (body !== undefined) init.body = JSON.stringify(body);
    }
    let res: Response;
    try {
      res = await fetchFn(path, init);
    } catch {
      throw new ApiError(0, "network_error");
    }
    if (res.status === 204) return undefined as T;
    let data: unknown = null;
    try {
      data = await res.json();
    } catch {
      // 본문이 JSON이 아니다(프록시 오류 페이지 등). 아래에서 상태 코드만으로 처리한다.
    }
    if (!res.ok) {
      const code = isRecord(data) && typeof data.error === "string" ? data.error : "unknown";
      throw new ApiError(res.status, code);
    }
    return data as T;
  }

  return {
    get: (path) => request("GET", path),
    send: (method, path, body) => request(method, path, body),
  };
}
