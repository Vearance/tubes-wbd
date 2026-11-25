import { Link } from "react-router";
import { AnimatePresence, m } from "motion/react";
import {
  api,
  type AdminUser,
  type DatabasePermission,
  type Group,
  type ManagedDatabase,
} from "../../api";
import { UtilityHeader } from "../../components/AppShell";
import { InteractiveLinkCard } from "../../components/aceternity/InteractiveLinkCard";
import { Icon } from "../../components/Icon";
import {
  Button,
  EmptyState,
  ErrorNotice,
  PageSkeleton,
  Status,
  baseButtonClasses,
  buttonSizes,
  buttonVariants,
} from "../../components/ui";
import { useResource } from "../../hooks";
import { cn } from "../../lib/utils";
import { surfaceClass } from "./common";

type GroupGovernanceDetail = {
  group: Group;
  members: AdminUser[];
  permissions: DatabasePermission[];
};

export function AdminLogsPage() {
  const governance = useResource(async () => {
    const [users, groups, databases] = await Promise.all([
      api.adminUsers(),
      api.adminGroups(),
      api.adminDatabases(),
    ]);
    const groupDetails: GroupGovernanceDetail[] = await Promise.all(
      groups.map(async (group) => {
        const [members, permissions] = await Promise.all([
          api.groupMembers(group.id),
          api.groupDatabases(group.id),
        ]);
        return { group, members, permissions };
      }),
    );
    return { users, groups, databases, groupDetails };
  }, []);

  const isLoading = governance.loading && !governance.data;

  const users = governance.data?.users ?? [];
  const groups = governance.data?.groups ?? [];
  const databases: ManagedDatabase[] = governance.data?.databases ?? [];
  const groupDetails = governance.data?.groupDetails ?? [];

  const activeUsers = users.filter((u) => !u.disabled).length;
  const groupsWithLeaders = groups.filter((g) => g.leader_user_id != null).length;
  const leaderCoverage =
    groups.length > 0 ? Math.round((groupsWithLeaders / groups.length) * 100) : 0;

  const dbReadiness = databases.map((db) => {
    const teams = groupDetails
      .map((detail) => ({
        group: detail.group,
        membersCount: detail.members.length,
        perm: detail.permissions.find((p) => p.database_id === db.id),
      }))
      .filter(
        (
          item,
        ): item is {
          group: Group;
          membersCount: number;
          perm: DatabasePermission;
        } => item.perm !== undefined,
      );

    const missingLeaderTeams = teams.filter(
      (t) => t.group.leader_user_id == null,
    );
    const hasEditor = teams.some((t) => t.perm.permission === "view_edit");
    const ready =
      teams.length > 0 && missingLeaderTeams.length === 0 && hasEditor;

    return { db, teams, missingLeaderTeams, hasEditor, ready };
  });

  const readyDatabasesCount = dbReadiness.filter((d) => d.ready).length;

  return (
    <>
      <UtilityHeader
        path="Administration"
        title="Governance & Readiness Matrix"
        status={
          isLoading
            ? "Loading matrix…"
            : `${readyDatabasesCount}/${databases.length} consensus ready`
        }
        actions={
          <Button variant="outline" size="sm" onClick={governance.reload}>
            <Icon name="refresh" className="size-3.5" />
            Refresh matrix
          </Button>
        }
      />

      <AnimatePresence mode="wait">
        {isLoading ? (
          <m.div
            key="matrix-loading"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18, ease: "easeOut" }}
          >
            <PageSkeleton variant="metrics" label="Loading governance and consensus matrix…" />
          </m.div>
        ) : (
          <m.div
            key="matrix-content"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.22, ease: "easeOut" }}
            className="space-y-5"
          >
            {governance.error && <ErrorNotice message={governance.error} />}

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <div className={cn(surfaceClass, "p-4")}>
          <span className="text-xs font-bold uppercase tracking-wider text-muted">
            Active Users
          </span>
          <div className="mt-2 flex items-baseline justify-between">
            <strong className="text-2xl font-black text-ink tabular-nums">
              {activeUsers}
            </strong>
            <span className="text-xs text-muted">of {users.length} total</span>
          </div>
        </div>

        <div className={cn(surfaceClass, "p-4")}>
          <span className="text-xs font-bold uppercase tracking-wider text-muted">
            Keyholder Coverage
          </span>
          <div className="mt-2 flex items-baseline justify-between">
            <strong className="text-2xl font-black text-violet-fg tabular-nums">
              {leaderCoverage}%
            </strong>
            <span className="text-xs text-muted">
              {groupsWithLeaders}/{groups.length} groups
            </span>
          </div>
        </div>

        <div className={cn(surfaceClass, "p-4")}>
          <span className="text-xs font-bold uppercase tracking-wider text-muted">
            Managed Databases
          </span>
          <div className="mt-2 flex items-baseline justify-between">
            <strong className="text-2xl font-black text-ink tabular-nums">
              {databases.length}
            </strong>
            <span className="text-xs text-muted">PostgreSQL targets</span>
          </div>
        </div>

        <div className={cn(surfaceClass, "p-4")}>
          <span className="text-xs font-bold uppercase tracking-wider text-muted">
            Consensus Readiness
          </span>
          <div className="mt-2 flex items-baseline justify-between">
            <strong
              className={cn(
                "text-2xl font-black tabular-nums",
                readyDatabasesCount === databases.length && databases.length > 0
                  ? "text-success-fg"
                  : "text-warning-fg",
              )}
            >
              {readyDatabasesCount}/{databases.length}
            </strong>
            <span className="text-xs text-muted">ready for migrations</span>
          </div>
        </div>
      </div>

      <section className={cn(surfaceClass, "overflow-hidden shadow-xs")}>
        <header className="flex flex-wrap items-center justify-between gap-3 border-b border-line px-5 py-4">
          <h2 className="m-0 text-sm font-bold text-ink">
            Database Consensus Matrix
          </h2>
          <Link
            to="/admin/groups"
            className={cn(
              baseButtonClasses,
              buttonVariants.dark,
              buttonSizes.sm,
              "no-underline",
            )}
          >
            <Icon name="groups" className="size-3.5" />
            Configure Groups &amp; Keyholders
          </Link>
        </header>

        {dbReadiness.length === 0 ? (
          <div className="py-10">
            <EmptyState
              icon="database"
              title="No managed databases registered"
              action={
                <Link
                  to="/admin/databases"
                  className={cn(
                    baseButtonClasses,
                    buttonVariants.dark,
                    buttonSizes.sm,
                    "no-underline",
                  )}
                >
                  Register first database
                </Link>
              }
            >
              Register a target database and grant group permissions to verify
              multi-team consensus readiness.
            </EmptyState>
          </div>
        ) : (
          <div className="divide-y divide-line">
            {dbReadiness.map(
              ({ db, teams, missingLeaderTeams, hasEditor, ready }) => (
                <article key={db.id} className="p-5 space-y-3">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div className="flex items-center gap-3 min-w-0">
                      <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-violet-soft text-violet-fg">
                        <Icon name="database" className="size-4" />
                      </span>
                      <div className="min-w-0">
                        <Link
                          to={`/admin/databases/${db.id}`}
                          className="text-sm font-bold text-ink no-underline hover:text-violet-fg hover:underline"
                        >
                          {db.name}
                        </Link>
                        <p className="mt-0.5 mb-0 font-mono text-[11px] text-muted truncate">
                          {db.host}:{db.port}/{db.db_name}
                        </p>
                      </div>
                    </div>

                    <div className="flex items-center gap-2">
                      {ready ? (
                        <Status tone="green" className="gap-1">
                          <Icon name="shield" className="size-3.5" />
                          Consensus Ready ({teams.length} Keyholder
                          {teams.length === 1 ? "" : "s"})
                        </Status>
                      ) : teams.length === 0 ? (
                        <Status tone="gray">No Teams Assigned</Status>
                      ) : missingLeaderTeams.length > 0 ? (
                        <Status tone="amber" className="gap-1">
                          <Icon name="alert" className="size-3.5" />
                          Blocked: Missing Keyholder
                        </Status>
                      ) : !hasEditor ? (
                        <Status tone="amber">Read-Only (No Editor Group)</Status>
                      ) : null}
                    </div>
                  </div>

                  {teams.length > 0 ? (
                    <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
                      {teams.map(({ group, membersCount, perm }) => {
                        const hasLeader = group.leader_user_id != null;
                        return (
                          <InteractiveLinkCard
                            key={group.id}
                            to={`/admin/groups/${group.id}`}
                            className={cn(
                              "flex items-center justify-between gap-2 p-3 no-underline",
                              hasLeader
                                ? "border-line/80 bg-white"
                                : "border-warning/40 bg-warning-soft/30 hover:border-warning",
                            )}
                          >
                            <div className="min-w-0">
                              <strong className="block truncate text-xs font-bold text-ink">
                                {group.name}
                              </strong>
                              <span className="mt-0.5 block text-xs text-muted">
                                {membersCount} member
                                {membersCount === 1 ? "" : "s"} ·{" "}
                                {hasLeader ? (
                                  <span className="font-semibold text-violet-fg">
                                    @{group.leader_username ?? `user #${group.leader_user_id}`}
                                  </span>
                                ) : (
                                  <span className="font-semibold text-warning-fg">
                                    Needs Keyholder →
                                  </span>
                                )}
                              </span>
                            </div>
                            <Status
                              tone={
                                perm.permission === "view_edit"
                                  ? "violet"
                                  : "accent"
                              }
                            >
                              {perm.permission === "view_edit"
                                ? "Editor"
                                : "Viewer"}
                            </Status>
                          </InteractiveLinkCard>
                        );
                      })}
                    </div>
                  ) : (
                    <div className="flex items-center justify-between rounded-xl border border-dashed border-line px-4 py-3 text-xs text-muted">
                      <span>No access groups assigned to this database.</span>
                      <Link
                        to={`/admin/databases/${db.id}`}
                        className="font-bold text-violet-fg no-underline hover:underline"
                      >
                        Grant access →
                      </Link>
                    </div>
                  )}
                </article>
              ),
            )}
          </div>
        )}
      </section>
          </m.div>
        )}
      </AnimatePresence>
    </>
  );
}
