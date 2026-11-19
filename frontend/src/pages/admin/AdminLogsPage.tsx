import { Link } from "react-router";
import { UtilityHeader } from "../../components/AppShell";
import { EmptyState } from "../../components/ui";
import { cn } from "../../lib/utils";
import { buttonClass, surfaceClass } from "./common";

export function AdminLogsPage() {
  return (
    <>
      <UtilityHeader path="Administration" title="Logs" />
      <section
        className={cn(
          surfaceClass,
          "grid min-h-[calc(100dvh-112px)] place-items-center",
        )}
      >
        <EmptyState
          className="h-auto min-h-0"
          icon="logs"
          title="Audit logs are not available"
          action={
            <Link className={buttonClass} to="/admin/users">
              Manage users
            </Link>
          }
        >
          The current OpenAPI contract does not expose an administrator
          audit-log endpoint. This screen is ready to consume one when it is
          added.
        </EmptyState>
      </section>
    </>
  );
}
