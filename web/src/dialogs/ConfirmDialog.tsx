import type { ReactNode } from "react";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";

interface Props {
  title: string;
  children: ReactNode;
  confirmLabel: string;
  /** 되돌릴 수 없는 작업이면 확인 버튼을 빨갛게 한다. */
  danger?: boolean;
  /** 실패하면 던진다. 성공하면 창이 닫힌다. */
  onConfirm: () => Promise<unknown>;
  onClose: () => void;
}

export function ConfirmDialog({ title, children, confirmLabel, danger, onConfirm, onClose }: Props) {
  const { run, busy, error } = useAction();

  async function confirm() {
    const ok = await run(async () => {
      await onConfirm();
      return true;
    });
    if (ok) onClose();
  }

  return (
    <Dialog title={title} onClose={onClose}>
      <div className="dialog-body">{children}</div>
      {error && <Notice kind="error">{error}</Notice>}
      <div className="actions">
        <button type="button" className="btn" onClick={onClose}>취소</button>
        <button type="button" className={danger ? "btn danger" : "btn primary"} disabled={busy} onClick={() => void confirm()}>
          {confirmLabel}
        </button>
      </div>
    </Dialog>
  );
}
