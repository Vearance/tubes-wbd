import { useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { AnimatePresence, m } from "motion/react";
import {
  api,
  type DatabasePermission,
  type Group,
  type ManagedDatabase,
  type ManagedDatabaseInput,
  type PermissionLevel,
} from "../../api";
import { UtilityHeader } from "../../components/AppShell";
import { Icon } from "../../components/Icon";
import {
  Button,
  ConfirmAction,
  EmptyState,
  ErrorNotice,
  Modal,
  PageSkeleton,
  Status,
  baseButtonClasses,
  buttonSizes,
  buttonVariants,
} from "../../components/ui";
import { useResource } from "../../hooks";
import { cn } from "../../lib/utils";
import { glyphClass, surfaceClass } from "./common";

type GroupWithDatabasePermissions = {
  group: Group;
  permissions: DatabasePermission[];
};

function ConnectionForm({
  initialData,
  onCancel,
  onCreated,
}: {
  initialData?: ManagedDatabase | null;
  onCancel: () => void;
  onCreated: (db: ManagedDatabase) => Promise<void>;
}) {
  const [value, setValue] = useState<ManagedDatabaseInput>({
    name: initialData?.name ?? "",
    host: initialData?.host ?? "",
    port: initialData?.port ?? 5432,
    username: initialData?.username ?? "",
    password: "",
    db_name: initialData?.db_name ?? "",
    ssl_mode: initialData?.ssl_mode ?? "disable",
  });
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);

  async function test() {
    setBusy(true);
    setMessage("Testing connection…");
    try {
      await api.testDatabase(value);
      setMessage("Connection succeeded. You can save it now.");
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Connection failed");
    } finally {
      setBusy(false);
    }
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setMessage("");
    try {
      const payload: ManagedDatabaseInput = {
        name: value.name,
        host: value.host,
        port: value.port,
        username: value.username,
        db_name: value.db_name,
        ssl_mode: value.ssl_mode,
        ...(value.password ? { password: value.password } : {}),
      };

      const result = initialData
        ? await api.updateDatabase(initialData.id, payload)
        : await api.createDatabase(payload);

      await onCreated(result);
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Unable to save connection");
      setBusy(false);
    }
  }

  const fieldClass =
    "grid gap-2 text-sm font-bold text-ink [&_input]:h-11 [&_input]:w-full [&_input]:rounded-xl [&_input]:border [&_input]:border-line-strong [&_input]:bg-white [&_input]:px-3.5 [&_input]:text-base sm:[&_input]:text-xs [&_input]:font-normal [&_input]:text-ink [&_input]:outline-none [&_input]:focus:border-violet [&_input]:focus:ring-2 [&_input]:focus:ring-violet/25 [&_select]:h-11 [&_select]:w-full [&_select]:rounded-xl [&_select]:border [&_select]:border-line-strong [&_select]:bg-white [&_select]:px-3.5 [&_select]:text-base sm:[&_select]:text-xs [&_select]:font-normal [&_select]:text-ink [&_select]:outline-none [&_select]:focus:border-violet [&_select]:focus:ring-2 [&_select]:focus:ring-violet/25";

  return (
    <form className="space-y-6" onSubmit={save}>
      <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
        <label className={fieldClass}>
          <span>Display name</span>
          <input
            value={value.name}
            onChange={(event) =>
              setValue({ ...value, name: event.target.value })
            }
            required
            placeholder="Customer DB"
          />
        </label>
        <label className={fieldClass}>
          <span>Host</span>
          <input
            value={value.host}
            onChange={(event) =>
              setValue({ ...value, host: event.target.value })
            }
            required
            placeholder="test-db"
          />
        </label>
        <label className={fieldClass}>
          <span>Port</span>
          <input
            type="number"
            min="1"
            max="65535"
            value={value.port}
            onChange={(event) =>
              setValue({ ...value, port: Number(event.target.value) })
            }
            required
          />
        </label>
        <label className={fieldClass}>
          <span>Database name</span>
          <input
            value={value.db_name}
            onChange={(event) =>
              setValue({ ...value, db_name: event.target.value })
            }
            required
            placeholder="targetdb"
          />
        </label>
        <label className={fieldClass}>
          <span>Username</span>
          <input
            value={value.username}
            onChange={(event) =>
              setValue({ ...value, username: event.target.value })
            }
            required
            autoComplete="username"
            placeholder="target"
          />
        </label>
        <label className={fieldClass}>
          <span>Password</span>
          <input
            type="password"
            value={value.password}
            onChange={(event) =>
              setValue({ ...value, password: event.target.value })
            }
            required={!initialData}
            autoComplete={initialData ? "current-password" : "new-password"}
            placeholder={
              initialData ? "•••••••• (Leave blank to keep unchanged)" : ""
            }
          />
        </label>
        <label className={cn(fieldClass, "sm:col-span-2")}>
          <span>SSL mode</span>
          <select
            value={value.ssl_mode}
            onChange={(event) =>
              setValue({ ...value, ssl_mode: event.target.value })
            }
          >
            <option value="disable">Disable</option>
            <option value="prefer">Prefer</option>
            <option value="require">Require</option>
            <option value="verify-ca">Verify CA</option>
            <option value="verify-full">Verify full</option>
          </select>
        </label>
      </div>
      {message && (
        <p
          className="rounded-xl bg-violet-soft px-4 py-3 text-xs font-semibold text-violet-fg"
          role="status"
        >
          {message}
        </p>
      )}
      <footer className="flex flex-wrap justify-end gap-3 border-t border-line/80 pt-5">
        <Button onClick={onCancel}>Cancel</Button>
        <Button disabled={busy} onClick={test}>
          <Icon name="refresh" />
          Test connection
        </Button>
        <Button type="submit" variant="dark" disabled={busy}>
          <Icon name="save" />
          {initialData ? "Update database" : "Save database"}
        </Button>
      </footer>
    </form>
  );
}

export function AdminDatabasesPage() {
  const { databaseId } = useParams<{ databaseId?: string }>();
  const navigate = useNavigate();
  const databases = useResource(() => api.adminDatabases(), []);
  const groupMatrix = useResource(async () => {
    const groups = await api.adminGroups();
    const entries = await Promise.all(
      groups.map(async (group) => ({
        group,
        permissions: await api.groupDatabases(group.id),
      })),
    );
    return entries satisfies GroupWithDatabasePermissions[];
  }, []);

  const [selectedState, setSelectedState] = useState<ManagedDatabase | null>(
    null,
  );
  const [showForm, setShowForm] = useState(false);
  const [editingDb, setEditingDb] = useState<ManagedDatabase | null>(null);
  const [message, setMessage] = useState("");
  const [grantGroupId, setGrantGroupId] = useState("");
  const [grantPermission, setGrantPermission] =
    useState<PermissionLevel>("view_only");
  const [accessBusy, setAccessBusy] = useState(false);
  const [accessError, setAccessError] = useState("");

  const selected =
    (databaseId
      ? databases.data?.find((db) => String(db.id) === databaseId)
      : null) ?? selectedState;

  function selectDatabase(db: ManagedDatabase) {
    setSelectedState(db);
    setMessage("");
    setAccessError("");
    navigate(`/admin/databases/${db.id}`);
    if (typeof window !== "undefined" && window.innerWidth <= 900) {
      setTimeout(() => {
        document.getElementById("db-detail-view")?.scrollIntoView({ behavior: "smooth" });
      }, 50);
    }
  }

  async function test(id: number) {
    setMessage("Testing connection…");
    try {
      await api.testStoredDatabase(id);
      setMessage("Connection is ready.");
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Connection failed");
    }
  }

  async function grantAccess(e: FormEvent) {
    e.preventDefault();
    if (!selected || !grantGroupId) return;
    setAccessBusy(true);
    setAccessError("");
    try {
      await api.setGroupDatabase(
        Number(grantGroupId),
        selected.id,
        grantPermission,
      );
      setGrantGroupId("");
      await groupMatrix.reload();
    } catch (err) {
      setAccessError(
        err instanceof Error ? err.message : "Unable to grant group access",
      );
    } finally {
      setAccessBusy(false);
    }
  }

  async function updateAccess(groupId: number, permission: PermissionLevel) {
    if (!selected) return;
    setAccessError("");
    try {
      await api.setGroupDatabase(groupId, selected.id, permission);
      await groupMatrix.reload();
    } catch (err) {
      setAccessError(
        err instanceof Error ? err.message : "Unable to update permission",
      );
    }
  }

  async function revokeAccess(groupId: number) {
    if (!selected) return;
    setAccessError("");
    try {
      await api.removeGroupDatabase(groupId, selected.id);
      await groupMatrix.reload();
    } catch (err) {
      setAccessError(
        err instanceof Error ? err.message : "Unable to revoke group access",
      );
    }
  }

  const isLoading = databases.loading && !databases.data;

  const attachedGroups = selected
    ? (groupMatrix.data ?? [])
        .map((entry) => ({
          group: entry.group,
          perm: entry.permissions.find((p) => p.database_id === selected.id),
        }))
        .filter(
          (
            item,
          ): item is { group: Group; perm: DatabasePermission } =>
            item.perm !== undefined,
        )
    : [];

  const unattachedGroups = selected
    ? (groupMatrix.data ?? [])
        .map((entry) => entry.group)
        .filter((g) => !attachedGroups.some((a) => a.group.id === g.id))
    : [];

  const missingLeaders = attachedGroups.filter(
    (item) => item.group.leader_user_id == null,
  );
  const hasEditor = attachedGroups.some(
    (item) => item.perm.permission === "view_edit",
  );

  return (
    <>
      <UtilityHeader
        path="Administration"
        title="Databases"
        status={
          isLoading
            ? "Loading databases…"
            : `${databases.data?.length ?? 0} connections`
        }
        actions={
          <Button
            variant="dark"
            onClick={() => {
              setEditingDb(null);
              setShowForm(true);
            }}
          >
            <Icon name="plus" />
            Add database
          </Button>
        }
      />

      <Modal
        open={showForm}
        onOpenChange={(open) => {
          if (!open) {
            setShowForm(false);
            setEditingDb(null);
          }
        }}
        title={
          editingDb
            ? "Edit PostgreSQL Database Connection"
            : "Add PostgreSQL Database"
        }
        description={
          editingDb
            ? "Modify credentials or network endpoints for this target database."
            : "Connect a new PostgreSQL target database to the consensus pipeline."
        }
        size="lg"
      >
        <ConnectionForm
          initialData={editingDb}
          onCancel={() => {
            setShowForm(false);
            setEditingDb(null);
          }}
          onCreated={async (saved) => {
            setShowForm(false);
            setEditingDb(null);
            await databases.reload();
            selectDatabase(saved);
          }}
        />
      </Modal>

      <AnimatePresence mode="wait">
        {isLoading ? (
          <m.div
            key="db-loading"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18, ease: "easeOut" }}
          >
            <PageSkeleton variant="admin-split" label="Loading managed databases…" />
          </m.div>
        ) : (
          <m.div
            key="db-content"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.22, ease: "easeOut" }}
            className="space-y-4"
          >
      {(databases.error || groupMatrix.error) && (
        <ErrorNotice message={databases.error || groupMatrix.error} />
      )}
      <div className="grid min-w-0 grid-cols-1 gap-3 min-[901px]:min-h-[calc(100dvh-110px)] min-[901px]:grid-cols-[360px_minmax(560px,1fr)]">
        <section className={cn(surfaceClass, "min-w-0 overflow-hidden")}>
          <header className="flex min-h-16 items-center justify-between gap-2 border-b border-line px-4 py-3">
            <h2 className="m-0 text-xs font-bold uppercase tracking-wider text-muted">
              Databases
            </h2>
            <span className="font-mono text-xs text-muted">
              {databases.data?.length ?? 0} total
            </span>
          </header>
          {databases.data?.map((database) => {
            const attached = (groupMatrix.data ?? []).filter((entry) =>
              entry.permissions.some((p) => p.database_id === database.id),
            );
            const blocked = attached.some((a) => a.group.leader_user_id == null);
            const isSelected = selected?.id === database.id;
            return (
              <button
                type="button"
                className={cn(
                  "grid min-h-[72px] w-full grid-cols-[38px_minmax(0,1fr)_auto_18px] items-center gap-3 border-0 border-b border-line bg-white px-4 py-3 text-left hover:bg-violet-soft/20 cursor-pointer transition-colors duration-150",
                  isSelected &&
                    "bg-violet-soft/60 shadow-[inset_3px_0_var(--color-violet)]",
                )}
                key={database.id}
                onClick={() => selectDatabase(database)}
              >
                <span className={glyphClass}>
                  <Icon name="database" className="size-4" />
                </span>
                <span className="min-w-0">
                  <strong className="block truncate text-xs font-bold text-ink">
                    {database.name}
                  </strong>
                  <span className="mt-0.5 block truncate text-[11px] text-muted font-mono">
                    {database.host}:{database.port}/{database.db_name}
                  </span>
                  <span className="mt-0.5 block text-[11px] text-muted">
                    {attached.length} team{attached.length === 1 ? "" : "s"}
                    {blocked && (
                      <span className="ml-1.5 font-semibold text-warning-fg">
                        · Leader missing
                      </span>
                    )}
                  </span>
                </span>
                <Status tone="green">{database.ssl_mode}</Status>
                <Icon name="chevron" className="size-4 text-muted/60" />
              </button>
            );
          })}
          {!databases.data?.length && (
            <div className="py-12 text-center text-xs text-muted">
              No databases registered
            </div>
          )}
        </section>

        <section id="db-detail-view" className={cn(surfaceClass, "relative min-w-0 overflow-auto shadow-xs")}>
          {selected ? (
            <div>
              <div className="flex flex-wrap items-center gap-4 border-b border-line p-5">
                <div className="flex min-w-0 flex-1 items-center gap-3">
                  <button
                    type="button"
                    onClick={() => {
                      window.scrollTo({ top: 0, behavior: "smooth" });
                    }}
                    className="min-[901px]:hidden flex items-center gap-1 text-xs font-bold text-violet-fg bg-violet-soft px-2.5 py-1.5 rounded-lg shrink-0 cursor-pointer"
                    title="Back to databases list"
                  >
                    <Icon name="chevron-left" className="size-3.5" />
                    <span>List</span>
                  </button>
                  <div className="min-w-0 flex-1">
                    <h2 className="my-1 truncate text-xl font-bold">{selected.name}</h2>
                    <p className="m-0 truncate text-xs text-muted">
                      {selected.host}:{selected.port} · {selected.db_name} (SSL:{" "}
                      {selected.ssl_mode})
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-2 flex-wrap">
                  <Button size="sm" onClick={() => test(selected.id)}>
                    <Icon name="refresh" className="size-3.5" />
                    Test connection
                  </Button>
                  <Button
                    size="sm"
                    onClick={() => {
                      setEditingDb(selected);
                      setShowForm(true);
                    }}
                  >
                    <Icon name="edit" className="size-3.5" />
                    Edit connection
                  </Button>
                  <ConfirmAction
                    title={`Delete database "${selected.name}"?`}
                    description={`Are you sure you want to delete "${selected.name}"? This removes connection credentials and associated group access permissions.`}
                    confirmLabel="Delete Database"
                    tone="danger"
                    onConfirm={async () => {
                      await api.deleteDatabase(selected.id);
                      setSelectedState(null);
                      navigate("/admin/databases");
                      await databases.reload();
                    }}
                    trigger={
                      <Button
                        size="sm"
                        className="border-danger/40 text-danger-fg hover:bg-danger-soft"
                      >
                        <Icon name="trash" className="size-3.5" />
                        Delete
                      </Button>
                    }
                  />
                </div>
                {message && (
                  <p
                    className="w-full text-xs text-violet-fg font-semibold m-0 bg-violet-soft px-3 py-2 rounded-lg"
                    role="status"
                  >
                    {message}
                  </p>
                )}
              </div>

              <div className="p-5 space-y-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h3 className="m-0 text-sm font-bold text-ink">
                    Attached Teams &amp; Consensus Readiness
                  </h3>
                  {attachedGroups.length === 0 ? (
                    <Status tone="gray">No groups assigned</Status>
                  ) : missingLeaders.length > 0 ? (
                    <Status tone="amber">
                      {missingLeaders.length} missing Keyholder
                    </Status>
                  ) : !hasEditor ? (
                    <Status tone="amber">Read-only (no Editor team)</Status>
                  ) : (
                    <Status tone="green">
                      Consensus ready ({attachedGroups.length})
                    </Status>
                  )}
                </div>

                {accessError && (
                  <ErrorNotice
                    message={accessError}
                    onDismiss={() => setAccessError("")}
                  />
                )}

                {missingLeaders.length > 0 && (
                  <div className="rounded-xl border border-warning/30 bg-warning-soft/50 px-4 py-3 text-xs text-ink flex flex-wrap items-center justify-between gap-2">
                    <span>
                      <strong className="text-warning-fg">
                        Missing Keyholder:
                      </strong>{" "}
                      {missingLeaders.map((m) => m.group.name).join(", ")}
                    </span>
                    <Link
                      to={`/admin/groups/${missingLeaders[0].group.id}`}
                      className={cn(
                        baseButtonClasses,
                        buttonVariants.dark,
                        buttonSizes.sm,
                        "no-underline",
                      )}
                    >
                      <Icon name="crown" className="size-3.5" />
                      Assign Keyholder
                    </Link>
                  </div>
                )}

                {attachedGroups.length > 0 ? (
                  <div className="overflow-hidden rounded-xl border border-line divide-y divide-line">
                    {attachedGroups.map(({ group, perm }) => {
                      const hasLeader = group.leader_user_id != null;
                      return (
                        <div
                          key={group.id}
                          className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 bg-white"
                        >
                          <div className="min-w-0 flex-1 flex flex-wrap items-center gap-2">
                            <Link
                              to={`/admin/groups/${group.id}`}
                              className="text-xs font-bold text-ink no-underline hover:text-violet-fg hover:underline"
                            >
                              {group.name}
                            </Link>
                            {hasLeader ? (
                              <Status tone="violet" className="gap-1">
                                <Icon name="crown" className="size-3" />
                                @{group.leader_username ?? `user #${group.leader_user_id}`}
                              </Status>
                            ) : (
                              <Link
                                to={`/admin/groups/${group.id}`}
                                className="no-underline"
                              >
                                <Status tone="amber" className="gap-1">
                                  <Icon name="alert" className="size-3" />
                                  Assign Keyholder
                                </Status>
                              </Link>
                            )}
                          </div>

                          <div className="flex items-center gap-2">
                            <select
                              aria-label={`Permission for ${group.name}`}
                              value={perm.permission}
                              onChange={(e) =>
                                void updateAccess(
                                  group.id,
                                  e.target.value as PermissionLevel,
                                )
                              }
                              className="h-9 sm:h-8 rounded-lg border border-line-strong bg-white px-2.5 text-base sm:text-xs font-semibold text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25"
                            >
                              <option value="view_only">Viewer</option>
                              <option value="view_edit">Editor</option>
                            </select>
                            <ConfirmAction
                              title={`Revoke ${group.name}'s access to ${selected.name}?`}
                              description="Group members will immediately lose access to this database."
                              confirmLabel="Revoke access"
                              onConfirm={() => revokeAccess(group.id)}
                              trigger={<Icon name="close" className="size-4" />}
                              triggerClassName="grid size-8 place-items-center rounded-lg border-0 bg-transparent text-muted hover:bg-danger-soft hover:text-danger-fg"
                              triggerAriaLabel={`Revoke ${group.name} access`}
                            />
                          </div>
                        </div>
                      );
                    })}
                  </div>
                ) : (
                  <p className="m-0 rounded-xl border border-dashed border-line p-4 text-center text-xs text-muted">
                    No teams assigned.
                  </p>
                )}

                {unattachedGroups.length > 0 && (
                  <form
                    onSubmit={grantAccess}
                    className="flex flex-wrap items-center gap-2 rounded-xl border border-line bg-canvas/50 p-3"
                  >
                    <span className="text-xs font-semibold text-ink mr-1">
                      Grant team access:
                    </span>
                    <select
                      aria-label="Select group to grant access"
                      value={grantGroupId}
                      onChange={(e) => setGrantGroupId(e.target.value)}
                      required
                      className="h-10 sm:h-9 w-full min-w-0 flex-1 rounded-lg border border-line-strong bg-white px-3 text-base sm:text-xs text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25 sm:w-auto sm:min-w-44"
                    >
                      <option value="">Choose a group…</option>
                      {unattachedGroups.map((g) => (
                        <option key={g.id} value={g.id}>
                          {g.name}
                          {g.leader_username
                            ? ` (Keyholder: @${g.leader_username})`
                            : " (No Keyholder)"}
                        </option>
                      ))}
                    </select>
                    <select
                      aria-label="Select permission level"
                      value={grantPermission}
                      onChange={(e) =>
                        setGrantPermission(e.target.value as PermissionLevel)
                      }
                      className="h-10 sm:h-9 rounded-lg border border-line-strong bg-white px-3 text-base sm:text-xs text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25"
                    >
                      <option value="view_only">Viewer</option>
                      <option value="view_edit">Editor</option>
                    </select>
                    <Button
                      type="submit"
                      variant="dark"
                      size="sm"
                      loading={accessBusy}
                      disabled={!grantGroupId}
                    >
                      <Icon name="plus" className="size-3.5" />
                      Grant access
                    </Button>
                  </form>
                )}
              </div>
            </div>
          ) : (
            <EmptyState
              icon="database"
              title="Select a database"
              action={
                databases.data?.[0] ? (
                  <Button onClick={() => selectDatabase(databases.data![0])}>
                    View first database
                  </Button>
                ) : undefined
              }
            >
              Choose a database from the list to inspect credentials, test connection, and configure team access.
            </EmptyState>
          )}
        </section>
      </div>
          </m.div>
        )}
      </AnimatePresence>
    </>
  );
}
