import { useCallback, useEffect, useRef, useState } from "react";
import { errorMessage } from "../lib/errors.ts";

/** 화면을 열 때 한 번, 그 뒤 refreshMs마다(0이면 안 함) 데이터를 불러온다. 서버 부하를 줄이려고 60초를 쓴다. */
export const REFRESH_MS = 60_000;

/**
 * 읽기 데이터를 불러오는 훅. 새로 고침이 실패해도 이전 데이터는 남기고 error만 채운다.
 * deps가 바뀌면(다른 도메인으로 이동 등) 이전 데이터를 버리고 다시 불러온다.
 * 늦게 도착한 옛 요청의 응답은 무시한다.
 */
export function useResource<T>(load: () => Promise<T>, deps: readonly unknown[], refreshMs = 0) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const seq = useRef(0);
  const loadRef = useRef(load);
  loadRef.current = load;

  const reload = useCallback(async () => {
    const mine = ++seq.current;
    try {
      const next = await loadRef.current();
      if (mine !== seq.current) return;
      setData(next);
      setError(null);
    } catch (e) {
      if (mine !== seq.current) return;
      setError(errorMessage(e));
    } finally {
      if (mine === seq.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    setData(null);
    setLoading(true);
    void reload();
    if (refreshMs <= 0) return;
    const id = setInterval(() => void reload(), refreshMs);
    return () => clearInterval(id);
  }, [reload, refreshMs, ...deps]);

  return { data, error, loading, reload };
}
