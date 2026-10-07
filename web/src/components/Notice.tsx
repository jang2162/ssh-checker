import type { ReactNode } from "react";

export function Notice({ kind = "info", children }: { kind?: "info" | "error" | "warning"; children: ReactNode }) {
  return (
    <div className={`notice notice-${kind}`} role={kind === "error" ? "alert" : "status"}>
      {children}
    </div>
  );
}
