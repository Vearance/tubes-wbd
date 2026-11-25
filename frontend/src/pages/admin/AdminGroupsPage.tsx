import { useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router";
import { AnimatePresence, m } from "motion/react";
import {
  api,
  type AdminUser,
  type Group,
  type ManagedDatabase,
  type PermissionLevel,
} from "../../api";
import { UtilityHeader } from "../../components/AppShell";
import { Icon } from "../../components/Icon";
import {
  Button,
  ConfirmAction,
  EmptyState,
  ErrorNotice,
  Loading,
  Modal,
  PageSkeleton,
  Search,
  Status,
} from "../../components/ui";
import { useResource } from "../../hooks";
import { cn } from "../../lib/utils";
import {
  AssignedList,
  assignedRowClass,
  AssignmentEmpty,
  AssignmentHeading,
  avatarClass,
  detailSectionClass,
  glyphClass,
  PickerAction,
  PickerPopover,
  pickerOptionClass,
  removeButtonClass,
  StaggerRow,
  surfaceClass,
} from "./common";

function PermissionChoice({
  value,
  onChange,
}: {
  value?: PermissionLevel;
  onChange: (permission: PermissionLevel) => Promise<boolean>;
}) {
  const [pendingValue, setPendingValue] = useState<PermissionLevel | null>(null);
  const [busy, setBusy] = useState(false);

  const shown = pendingValue ?? value;

  async function choose(permission: PermissionLevel) {
    if (busy || permission === shown) return;
    setPendingValue(permission);
    setBusy(true);
    try {
      await onChange(permission);
    } finally {
      setPendingValue(null);
      setBusy(false);
    }
  }

  const choiceClass =
    "min-h-[34px] rounded-lg border px-2.5 text-[11px] font-bold disabled:cursor-not-allowed";

  return (
    <div
      className="inline-flex gap-1 max-[600px]:col-[2/4] max-[600px]:row-start-2"
      aria-label="Database permission"
    >
      <button
        type="button"
        disabled={busy}
        className={cn(
          choiceClass,
          shown === "view_only"
            ? "border-accent bg-accent-soft text-accent-fg"
            : "border-line bg-white text-muted hover:text-ink",
        )}
        onClick={() => void choose("view_only")}
      >
        Viewer
      </button>
      <button
        type="button"
        disabled={busy}
        className={cn(
          choiceClass,
          shown === "view_edit"
            ? "border-violet bg-violet-soft text-violet-fg"
            : "border-line bg-white text-muted hover:text-ink",
        )}
        onClick={() => void choose("view_edit")}
      >
        Editor
      </button>
    </div>
  );
}

function LeaderChoice({
  isLeader,
  disabled,
  onSetLeader,
  onClearLeader,
}: {
  isLeader: boolean;
  disabled?: boolean;
  onSetLeader: () => Promise<void>;
  onClearLeader: () => Promise<void>;
}) {
  const [busy, setBusy] = useState(false);

  async function choose(nextLeader: boolean) {
    if (busy || nextLeader === isLeader || (nextLeader && disabled)) return;
    setBusy(true);
    try {
      if (nextLeader) {
        await onSetLeader();
      } else {
        await onClearLeader();
      }
    } finally {
      setBusy(false);
    }
  }

  const pillClass =
    "min-h-[32px] rounded-lg border px-2.5 text-[11px] font-bold disabled:cursor-not-allowed disabled:opacity-60";

  return (
    <div className="inline-flex items-center gap-1" aria-label="Keyholder role">
      <button
        type="button"
        disabled={busy}
        className={cn(
          pillClass,
          !isLeader
            ? "border-line-strong bg-canvas text-ink"
            : "border-line bg-white text-muted hover:text-ink",
        )}
        onClick={() => void choose(false)}
      >
        Member
      </button>
      <button
        type="button"
        disabled={busy || (!isLeader && disabled)}
        title={
          disabled && !isLeader
            ? "Disabled users cannot be assigned as Keyholder"
            : "Assign as group Keyholder"
        }
        className={cn(
          pillClass,
          "inline-flex items-center gap-1",
          isLeader
            ? "border-violet bg-violet-soft text-violet-fg"
            : "border-line bg-white text-muted hover:text-ink",
        )}
        onClick={() => void choose(true)}
      >
        <Icon name="crown" className="size-3" />
        Keyholder
      </button>
    </div>
  );
}

function GroupDetail({
  group,
  users,
  databases,
  onGroupUpdated,
}: {
  group: Group;
  users: AdminUser[];
  databases: ManagedDatabase[];
  onGroupUpdated: () => Promise<void>;
}) {
  const members = useResource(() => api.groupMembers(group.id), [group.id]);
  const permissions = useResource(
    () => api.groupDatabases(group.id),
    [group.id],
  );
  const [memberPicker, setMemberPicker] = useState(false);
  const [databasePicker, setDatabasePicker] = useState(false);
  const [memberSearch, setMemberSearch] = useState("");
  const [databaseSearch, setDatabaseSearch] = useState("");
  const [memberError, setMemberError] = useState("");
  const [databaseError, setDatabaseError] = useState("");
  const [leaderBusy, setLeaderBusy] = useState(false);

  const availableMembers = users.filter(
    (user) =>
      !user.is_admin &&
      !members.data?.some((member) => member.id === user.id) &&
      user.username.toLowerCase().includes(memberSearch.toLowerCase()),
  );

  const availableDatabases = databases.filter(
    (database) =>
      !permissions.data?.some((item) => item.database_id === database.id) &&
      database.name.toLowerCase().includes(databaseSearch.toLowerCase()),
  );

  const leaderMember =
    group.leader_user_id != null
      ? members.data?.find((m) => m.id === group.leader_user_id)
      : undefined;
  const leaderName =
    group.leader_username ??
    leaderMember?.username ??
    (group.leader_user_id ? `User #${group.leader_user_id}` : null);
  const hasLeader = group.leader_user_id != null;
  const hasDatabases = (permissions.data?.length ?? 0) > 0;

  async function assignLeader(userId: number) {
    setMemberError("");
    setLeaderBusy(true);
    try {
      await api.setGroupLeader(group.id, userId);
      await onGroupUpdated();
    } catch (e) {
      setMemberError(
        e instanceof Error ? e.message : "Unable to assign group Keyholder",
      );
    } finally {
      setLeaderBusy(false);
    }
  }

  async function clearLeader() {
    setMemberError("");
    setLeaderBusy(true);
    try {
      await api.clearGroupLeader(group.id);
      await onGroupUpdated();
    } catch (e) {
      setMemberError(
        e instanceof Error ? e.message : "Unable to clear group Keyholder",
      );
    } finally {
      setLeaderBusy(false);
    }
  }

  async function addMember(userId: number) {
    setMemberError("");
    setMemberPicker(false);
    try {
      await api.addGroupMember(group.id, userId);
      await members.reload();
    } catch (e) {
      setMemberError(e instanceof Error ? e.message : "Unable to add member");
      setMemberPicker(true);
    }
  }

  async function removeMember(userId: number) {
    setMemberError("");
    try {
      await api.removeGroupMember(group.id, userId);
      await members.reload();
    } catch (e) {
      setMemberError(
        e instanceof Error ? e.message : "Unable to remove member",
      );
    }
  }

  async function setDatabasePermission(
    databaseId: number,
    permission: PermissionLevel,
    isNew = false,
  ) {
    setDatabaseError("");
    if (isNew) setDatabasePicker(false);
    try {
      await api.setGroupDatabase(group.id, databaseId, permission);
      await permissions.reload();
      return true;
    } catch (e) {
      setDatabaseError(
        e instanceof Error ? e.message : "Unable to update database access",
      );
      if (isNew) setDatabasePicker(true);
      return false;
    }
  }

  async function removeDatabase(databaseId: number) {
    setDatabaseError("");
    try {
      await api.removeGroupDatabase(group.id, databaseId);
      await permissions.reload();
    } catch (e) {
      setDatabaseError(
        e instanceof Error ? e.message : "Unable to remove database",
      );
    }
  }

  return (
    <>
      <header className="flex flex-wrap min-h-[84px] items-center justify-between gap-3 border-b border-line p-5">
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <button
            type="button"
            onClick={() => {
              window.scrollTo({ top: 0, behavior: "smooth" });
            }}
            className="min-[901px]:hidden flex items-center gap-1 text-xs font-bold text-violet-fg bg-violet-soft px-2.5 py-1.5 rounded-lg shrink-0 cursor-pointer"
            title="Back to groups list"
          >
            <Icon name="chevron-left" className="size-3.5" />
            <span>List</span>
          </button>
          <div className="min-w-0">
            <span className="text-[11px] font-bold uppercase tracking-wider text-muted">
              Group #{group.id}
            </span>
            <h2 className="mt-1 mb-0 truncate text-lg font-bold text-ink">{group.name}</h2>
          </div>
        </div>
        {hasLeader ? (
          <Status tone="violet" className="gap-1.5 px-3 py-1">
            <Icon name="crown" className="size-3.5" />
            Keyholder: @{leaderName}
          </Status>
        ) : (
          <Status tone="amber" className="gap-1.5 px-3 py-1">
            <Icon name="alert" className="size-3.5" />
            No Keyholder
          </Status>
        )}
      </header>

      <section
        className={cn(
          detailSectionClass,
          "py-3.5",
          hasLeader
            ? "bg-violet-soft/20"
            : hasDatabases
              ? "bg-warning-soft/45"
              : "bg-canvas/50",
        )}
      >
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <span
              className={cn(
                "grid size-8 shrink-0 place-items-center rounded-lg",
                hasLeader
                  ? "bg-violet text-white"
                  : hasDatabases
                    ? "bg-warning text-white"
                    : "bg-white border border-line text-muted",
              )}
            >
              <Icon name={hasLeader ? "crown" : "key"} className="size-4" />
            </span>
            <p className="m-0 text-xs font-medium text-ink">
              {hasLeader ? (
                <>
                  <strong>@{leaderName}</strong> holds the group approval key
                </>
              ) : hasDatabases ? (
                <span className="font-semibold text-warning-fg">
                  Assign a Keyholder to unblock migrations
                </span>
              ) : (
                <span className="text-muted">No Keyholder assigned</span>
              )}
            </p>
          </div>
          {hasLeader && (
            <ConfirmAction
              title={`Relinquish Keyholder from @${leaderName}?`}
              description={`New migrations on databases shared with ${group.name} will pause until a Keyholder is assigned.`}
              confirmLabel="Clear Keyholder"
              tone="danger"
              onConfirm={clearLeader}
              trigger={
                <Button variant="outline" size="sm" disabled={leaderBusy}>
                  <Icon name="unlock" className="size-3.5" />
                  Clear key
                </Button>
              }
            />
          )}
        </div>
      </section>

      <section className={detailSectionClass}>
        <AssignmentHeading>
          <h3 className="text-sm font-bold text-ink">
            Members ({members.data?.length ?? 0})
          </h3>
          <PickerAction
            open={memberPicker}
            label="Add member"
            className="max-[600px]:w-full"
            onClick={() => setMemberPicker(!memberPicker)}
          />
        </AssignmentHeading>
        {(members.error || memberError) && (
          <ErrorNotice message={members.error || memberError} />
        )}
        <AssignedList>
          {members.loading && !members.data ? (
            <Loading />
          ) : (
            members.data?.map((member) => {
              const isLeader = group.leader_user_id === member.id;
              return (
                <StaggerRow
                  className={cn(
                    assignedRowClass,
                    "grid-cols-[36px_minmax(0,1fr)_auto_32px] max-[600px]:grid-cols-[36px_minmax(0,1fr)_32px]",
                    isLeader && "bg-violet-soft/15",
                  )}
                  key={member.id}
                >
                  <span
                    className={cn(
                      avatarClass,
                      isLeader && "bg-violet text-white",
                      "max-[600px]:col-start-1 max-[600px]:row-start-1",
                    )}
                  >
                    {member.username.slice(0, 2).toUpperCase()}
                  </span>
                  <span className="max-[600px]:col-start-2 max-[600px]:row-start-1">
                    <b className="flex items-center gap-1.5">
                      <span className="truncate">{member.username}</span>
                      {member.disabled && (
                        <Status tone="red">
                          Disabled
                        </Status>
                      )}
                    </b>
                    <small>#{member.id}</small>
                  </span>
                  <div className="max-[600px]:col-[2/4] max-[600px]:row-start-2">
                    <LeaderChoice
                      isLeader={isLeader}
                      disabled={member.disabled}
                      onSetLeader={() => assignLeader(member.id)}
                      onClearLeader={clearLeader}
                    />
                  </div>
                  {isLeader ? (
                    <span
                      className="grid size-8 place-items-center rounded-lg text-violet-fg/80 max-[600px]:col-start-3 max-[600px]:row-start-1"
                      title="Reassign or clear Keyholder before removing"
                    >
                      <Icon name="lock" className="size-4" />
                    </span>
                  ) : (
                    <ConfirmAction
                      trigger={<Icon name="close" className="size-4" />}
                      triggerClassName={cn(
                        removeButtonClass,
                        "max-[600px]:col-start-3 max-[600px]:row-start-1",
                      )}
                      triggerAriaLabel={`Remove ${member.username}`}
                      title={`Remove ${member.username} from ${group.name}?`}
                      description="The user will lose access inherited through this group."
                      confirmLabel="Remove member"
                      onConfirm={() => removeMember(member.id)}
                    />
                  )}
                </StaggerRow>
              );
            })
          )}
        </AssignedList>
        {!members.loading && !members.data?.length && (
          <AssignmentEmpty>No members</AssignmentEmpty>
        )}
        {memberPicker && (
          <PickerPopover>
            <Search
              value={memberSearch}
              onChange={setMemberSearch}
              placeholder="Search users"
            />
            <div className="mt-2 max-h-[210px] overflow-auto">
              {availableMembers.map((user) => (
                <button
                  type="button"
                  className={pickerOptionClass}
                  key={user.id}
                  onClick={() => addMember(user.id)}
                >
                  <span className={cn(avatarClass, "size-8 rounded-lg")}>
                    {user.username.slice(0, 2).toUpperCase()}
                  </span>
                  <span>
                    <b>{user.username}</b>
                    <small>
                      #{user.id}
                      {user.disabled ? " · Disabled" : ""}
                    </small>
                  </span>
                  <em className="text-right text-xs font-bold not-italic text-violet-fg">
                    Add
                  </em>
                </button>
              ))}
              {!availableMembers.length && (
                <p className="m-0 p-4 text-center text-xs text-muted">
                  No users available
                </p>
              )}
            </div>
          </PickerPopover>
        )}
      </section>

      <section className={detailSectionClass}>
        <AssignmentHeading>
          <h3 className="text-sm font-bold text-ink">
            Database access ({permissions.data?.length ?? 0})
          </h3>
          <PickerAction
            open={databasePicker}
            label="Add database"
            className="max-[600px]:w-full"
            onClick={() => setDatabasePicker(!databasePicker)}
          />
        </AssignmentHeading>
        {(permissions.error || databaseError) && (
          <ErrorNotice message={permissions.error || databaseError} />
        )}
        <AssignedList>
          {permissions.loading && !permissions.data ? (
            <Loading />
          ) : (
            permissions.data?.map((item) => {
              const database = databases.find(
                (entry) => entry.id === item.database_id,
              );
              return (
                <StaggerRow
                  className={cn(
                    assignedRowClass,
                    "grid-cols-[36px_minmax(0,1fr)_auto_32px] max-[600px]:grid-cols-[36px_minmax(0,1fr)_32px]",
                  )}
                  key={item.database_id}
                >
                  <span
                    className={cn(
                      glyphClass,
                      "max-[600px]:col-start-1 max-[600px]:row-start-1",
                    )}
                  >
                    <Icon name="database" className="size-4" />
                  </span>
                  <span className="max-[600px]:col-start-2 max-[600px]:row-start-1">
                    <b>{database?.name ?? `Database #${item.database_id}`}</b>
                    <small>
                      {database
                        ? `${database.host}:${database.port}`
                        : `#${item.database_id}`}
                    </small>
                  </span>
                  <PermissionChoice
                    value={item.permission}
                    onChange={(permission) =>
                      setDatabasePermission(item.database_id, permission)
                    }
                  />
                  <ConfirmAction
                    trigger={<Icon name="close" className="size-4" />}
                    triggerClassName={cn(
                      removeButtonClass,
                      "max-[600px]:col-start-3 max-[600px]:row-start-1",
                    )}
                    triggerAriaLabel={`Remove ${database?.name ?? "database"}`}
                    title={`Remove ${database?.name ?? "this database"}?`}
                    description={`Members of ${group.name} will lose access to this database.`}
                    confirmLabel="Remove database"
                    onConfirm={() => removeDatabase(item.database_id)}
                  />
                </StaggerRow>
              );
            })
          )}
        </AssignedList>
        {!permissions.loading && !permissions.data?.length && (
          <AssignmentEmpty>No database access</AssignmentEmpty>
        )}
        {databasePicker && (
          <PickerPopover>
            <Search
              value={databaseSearch}
              onChange={setDatabaseSearch}
              placeholder="Search databases"
            />
            <div className="mt-2 max-h-[210px] overflow-auto">
              {availableDatabases.map((database) => (
                <StaggerRow
                  className="grid min-h-[52px] w-full grid-cols-[34px_minmax(0,1fr)_auto] items-center gap-3 rounded-lg p-2 text-left text-ink max-[600px]:grid-cols-[34px_minmax(0,1fr)] [&_b]:block [&_b]:text-xs [&_b]:font-bold [&_small]:mt-0.5 [&_small]:block [&_small]:text-xs [&_small]:text-muted"
                  key={database.id}
                >
                  <span className={cn(glyphClass, "size-8 rounded-lg")}>
                    <Icon name="database" className="size-4" />
                  </span>
                  <span>
                    <b>{database.name}</b>
                    <small>
                      {database.host}:{database.port}
                    </small>
                  </span>
                  <PermissionChoice
                    onChange={(permission) =>
                      setDatabasePermission(database.id, permission, true)
                    }
                  />
                </StaggerRow>
              ))}
              {!availableDatabases.length && (
                <p className="m-0 p-4 text-center text-xs text-muted">
                  No databases available
                </p>
              )}
            </div>
          </PickerPopover>
        )}
      </section>
    </>
  );
}

export function AdminGroupsPage() {
  const { groupId } = useParams<{ groupId?: string }>();
  const navigate = useNavigate();
  const groups = useResource(() => api.adminGroups(), []);
  const users = useResource(() => api.adminUsers(), []);
  const databases = useResource(() => api.adminDatabases(), []);
  const [selectedState, setSelectedState] = useState<Group | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState("");
  const [error, setError] = useState("");

  const selected =
    (groupId
      ? groups.data?.find((group) => String(group.id) === groupId)
      : null) ?? selectedState;

  function selectGroup(group: Group) {
    setSelectedState(group);
    navigate(`/admin/groups/${group.id}`);
    if (typeof window !== "undefined" && window.innerWidth <= 900) {
      setTimeout(() => {
        document.getElementById("group-detail-view")?.scrollIntoView({ behavior: "smooth" });
      }, 50);
    }
  }

  async function createGroup(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const created = await api.createGroup(name);
      setName("");
      setShowCreate(false);
      await groups.reload();
      selectGroup(created);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to create group");
    }
  }

  async function handleGroupUpdated() {
    const refreshed = await api.adminGroups();
    groups.reload();
    if (selected) {
      const next = refreshed.find((g) => g.id === selected.id);
      if (next) setSelectedState(next);
    }
  }

  const isLoading =
    (groups.loading && !groups.data) ||
    (users.loading && !users.data) ||
    (databases.loading && !databases.data);

  return (
    <>
      <UtilityHeader
        path="Administration"
        title="Groups"
        status={
          isLoading ? "Loading groups…" : `${groups.data?.length ?? 0} groups`
        }
        actions={
          <Button variant="dark" onClick={() => setShowCreate(true)}>
            <Icon name="plus" />
            Add group
          </Button>
        }
      />

      <Modal
        open={showCreate}
        onOpenChange={setShowCreate}
        title="Create Access Group"
        description="Create a new access control group to manage database permissions and Keyholders."
        size="sm"
      >
        <form onSubmit={createGroup} className="space-y-6">
          <label className="block text-sm font-bold text-ink">
            <span>Group name</span>
            <input
              className="mt-2 h-11 w-full rounded-xl border border-line-strong bg-white px-3.5 text-base sm:text-xs text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25"
              value={name}
              onChange={(event) => setName(event.target.value)}
              required
              autoFocus
              placeholder="e.g. Customer Editors"
            />
          </label>
          <div className="flex flex-wrap justify-end gap-3 pt-5 border-t border-line/80">
            <Button onClick={() => setShowCreate(false)}>Cancel</Button>
            <Button type="submit" variant="dark">
              Create group
            </Button>
          </div>
        </form>
      </Modal>

      <AnimatePresence mode="wait">
        {isLoading ? (
          <m.div
            key="groups-loading"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18, ease: "easeOut" }}
          >
            <PageSkeleton variant="admin-split" label="Loading access groups and governance…" />
          </m.div>
        ) : (
          <m.div
            key="groups-content"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.22, ease: "easeOut" }}
            className="space-y-4"
          >
            {(groups.error || users.error || databases.error || error) && (
              <ErrorNotice
                message={groups.error || users.error || databases.error || error}
              />
            )}
      <div className="grid min-w-0 grid-cols-1 gap-3 min-[901px]:min-h-[calc(100dvh-110px)] min-[901px]:grid-cols-[360px_minmax(560px,1fr)]">
        <section className={cn(surfaceClass, "min-w-0 overflow-hidden")}>
          <header className="flex min-h-14 items-center border-b border-line px-4 py-2.5">
            <h2 className="m-0 text-sm font-bold text-ink">Access groups</h2>
            <span className="ml-auto text-xs text-muted">
              {groups.data?.length ?? 0} total
            </span>
          </header>
          {groups.data?.map((group) => {
            const hasLeader = group.leader_user_id != null;
            return (
              <button
                type="button"
                className={cn(
                  "grid min-h-[72px] w-full grid-cols-[38px_minmax(0,1fr)_auto_18px] items-center gap-3 border-0 border-b border-line bg-white px-4 py-3 text-left hover:bg-violet-soft/20 cursor-pointer transition-colors duration-150",
                  selected?.id === group.id &&
                    "bg-violet-soft/60 shadow-[inset_3px_0_var(--color-violet)]",
                )}
                key={group.id}
                onClick={() => selectGroup(group)}
              >
                <span className={glyphClass}>
                  <Icon name="groups" className="size-4" />
                </span>
                <span className="min-w-0">
                  <b className="block truncate text-xs font-bold text-ink">
                    {group.name}
                  </b>
                  <span className="mt-1 flex items-center gap-1.5 text-xs">
                    <span
                      className={cn(
                        "size-1.5 rounded-full shrink-0",
                        hasLeader ? "bg-violet" : "bg-warning",
                      )}
                    />
                    <span
                      className={cn(
                        "truncate",
                        hasLeader ? "text-violet-fg font-semibold" : "text-warning-fg font-medium",
                      )}
                    >
                      {hasLeader
                        ? `@${group.leader_username ?? `user #${group.leader_user_id}`}`
                        : "No Keyholder"}
                    </span>
                  </span>
                </span>
                <em className="text-xs font-semibold not-italic text-violet-fg">
                  Manage
                </em>
                <Icon name="chevron" className="size-4 text-muted" />
              </button>
            );
          })}
        </section>
        <section id="group-detail-view" className={cn(surfaceClass, "relative min-w-0 overflow-auto")}>
          {selected ? (
            <GroupDetail
              key={selected.id}
              group={selected}
              users={users.data ?? []}
              databases={databases.data ?? []}
              onGroupUpdated={handleGroupUpdated}
            />
          ) : (
            <EmptyState
              icon="groups"
              title="Select a group"
              action={
                groups.data?.[0] ? (
                  <Button onClick={() => selectGroup(groups.data![0])}>
                    View first group
                  </Button>
                ) : undefined
              }
            >
              Choose a group to manage members, Keyholders, and database access.
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
