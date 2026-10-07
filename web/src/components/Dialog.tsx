import { useEffect, useId, useRef, type ReactNode } from "react";

interface Props {
  title: string;
  /** Esc로 닫혔을 때 부른다. 부모가 창을 렌더링하지 않으면 닫힌다. */
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
  /**
   * true면 Esc로 닫히지 않는다. 브라우저가 어떻게든 닫으면(Chromium의 연속 Esc) 다시 연다.
   * 닫는 것은 부모가 컴포넌트를 언마운트해서 한다(그때는 close 핸들러가 불리지 않는다).
   * 한 번만 보여 주는 정보를 담은 창에 쓴다.
   */
  persistent?: boolean;
}

/** 브라우저 <dialog>를 모달로 연다. 포커스 가둠과 Esc 처리는 브라우저가 한다. */
export function Dialog({ title, onClose, children, wide, persistent }: Props) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const d = ref.current;
    if (d && !d.open) d.showModal();
  }, []);
  return (
    <dialog
      ref={ref}
      className={wide ? "dialog wide" : "dialog"}
      aria-labelledby={titleId}
      onClose={() => {
        if (persistent) {
          // 브라우저가 cancel을 막지 못하고 닫았으면(Chromium의 연속 Esc) 다시 연다.
          const d = ref.current;
          if (d && !d.open) d.showModal();
          return;
        }
        onClose();
      }}
      onCancel={persistent ? (e) => e.preventDefault() : undefined}
    >
      <h2 id={titleId}>{title}</h2>
      {children}
    </dialog>
  );
}
