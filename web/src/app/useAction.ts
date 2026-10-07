import { useCallback, useState } from "react";
import { ApiError } from "../api/client.ts";
import { errorMessage } from "../lib/errors.ts";
import { useApp } from "./context.tsx";

/**
 * 쓰기 요청을 실행하고 진행 중 여부와 오류 문구를 관리한다.
 * 실패하면 undefined를 돌려준다. 401이면 세션이 만료된 것이므로 화면을 조회 전용으로 되돌린다.
 */
export function useAction() {
  const { markLoggedOut } = useApp();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const run = useCallback(
    async <T,>(fn: () => Promise<T>): Promise<T | undefined> => {
      setBusy(true);
      setError(null);
      try {
        return await fn();
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) markLoggedOut();
        setError(errorMessage(e));
        return undefined;
      } finally {
        setBusy(false);
      }
    },
    [markLoggedOut],
  );
  const clearError = useCallback(() => setError(null), []);
  return { run, busy, error, clearError };
}
