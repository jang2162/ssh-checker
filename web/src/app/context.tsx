import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { api } from "../api/endpoints.ts";
import type { CodeInfo, Config } from "../api/types.ts";
import { parseGoDuration } from "../lib/status.ts";

interface AppState {
  /** 로그인 여부와 설정을 처음 불러오는 일이 끝났는지. */
  ready: boolean;
  admin: boolean;
  config: Config | null;
  checkIntervalMs: number | null;
  /** 판정 코드 → 설명(/api/codes). 배지의 툴팁에 쓴다. */
  codes: ReadonlyMap<string, CodeInfo>;
  login: (key: string) => Promise<void>;
  logout: () => Promise<void>;
  /** 쓰기 요청이 401을 받았을 때(세션 만료) 화면을 조회 전용으로 되돌린다. */
  markLoggedOut: () => void;
}

const Ctx = createContext<AppState | null>(null);

export function AppProvider({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false);
  const [admin, setAdmin] = useState(false);
  const [config, setConfig] = useState<Config | null>(null);
  const [codes, setCodes] = useState<ReadonlyMap<string, CodeInfo>>(new Map());

  useEffect(() => {
    let live = true;
    void Promise.allSettled([api.session(), api.config(), api.codes()]).then(([s, c, k]) => {
      if (!live) return;
      if (s.status === "fulfilled") setAdmin(s.value);
      if (c.status === "fulfilled") setConfig(c.value);
      if (k.status === "fulfilled") setCodes(new Map(k.value.map((x): [string, CodeInfo] => [x.code, x])));
      setReady(true);
    });
    return () => {
      live = false;
    };
  }, []);

  const login = useCallback(async (key: string) => {
    await api.login(key);
    setAdmin(true);
  }, []);
  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setAdmin(false);
    }
  }, []);
  const markLoggedOut = useCallback(() => setAdmin(false), []);

  const value = useMemo<AppState>(
    () => ({
      ready,
      admin,
      config,
      checkIntervalMs: config ? parseGoDuration(config.check_interval) : null,
      codes,
      login,
      logout,
      markLoggedOut,
    }),
    [ready, admin, config, codes, login, logout, markLoggedOut],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useApp(): AppState {
  const v = useContext(Ctx);
  if (!v) throw new Error("useApp must be used inside AppProvider");
  return v;
}
