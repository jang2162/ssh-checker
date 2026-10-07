import { useEffect, useRef, type ReactNode } from "react";

interface Props {
  title: string;
  /** Esc로 닫혔을 때 부른다. 부모가 창을 렌더링하지 않으면 닫힌다. */
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
  /** true면 Esc로 닫히지 않는다. 한 번만 보여 주는 정보를 담은 창에 쓴다. */
  persistent?: boolean;
}

/** 브라우저 <dialog>를 모달로 연다. 포커스 가둠과 Esc 처리는 브라우저가 한다. */
export function Dialog({ title, onClose, children, wide, persistent }: Props) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (d && !d.open) d.showModal();
  }, []);
  return (
    <dialog
      ref={ref}
      className={wide ? "dialog wide" : "dialog"}
      aria-labelledby="dialog-title"
      onClose={onClose}
      onCancel={persistent ? (e) => e.preventDefault() : undefined}
    >
      <h2 id="dialog-title">{title}</h2>
      {children}
    </dialog>
  );
}
