import { Link } from "react-router-dom";
import { usePageTitle } from "../app/usePageTitle.ts";
import { t } from "../i18n/index.ts";

export function NotFound() {
  usePageTitle(t("notFound.title"));
  return (
    <div className="empty">
      <h1>{t("notFound.heading")}</h1>
      <p><Link to="/">{t("notFound.home")}</Link></p>
    </div>
  );
}
