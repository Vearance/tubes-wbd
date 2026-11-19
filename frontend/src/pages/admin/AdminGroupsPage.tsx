import { useId, useState, type FormEvent } from "react";
import { AnimatePresence, LayoutGroup, m, useReducedMotion } from "motion/react";
import { useNavigate, useParams } from "react-router";
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
  Search,
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
  const groupId = useId();
  const reduceMotion = useReducedMotion();
  const [optimisticValue, setOptimisticValue] = useState(value);
  const [busy, setBusy] = useState(false);

  async function choose(permission: PermissionLevel) {
    if (busy || permission === optimisticValue) return;
    const previous = optimisticValue;
    setOptimisticValue(permission);
    setBusy(true);
    if (!(await onChange(permission))) setOptimisticValue(previous);
    setBusy(false);
  }

  const choiceClass =
    "relative min-h-[34px] overflow-hidden rounded-lg border border-line bg-white px-[9px] text-[8px] font-extrabold text-muted disabled:cursor-not-allowed";

  return (
    <LayoutGroup id={groupId}>
      <div
        className="inline-flex gap-1 max-[600px]:col-[2/4] max-[600px]:row-start-2"
        aria-label="Database permission"
      >
        <button
          disabled={busy}
          className={cn(
            choiceClass,
            optimisticValue === "view_only" && "border-accent text-accent",
          )}
          onClick={() => void choose("view_only")}
        >
          {optimisticValue === "view_only" && (
            <m.span
              className="absolute inset-0 rounded-[7px]"
              layoutId="permission-knob"
              initial={false}
              transition={{ layout: { duration: 0.16, ease: "easeInOut" } }}
            >
              <m.span
                className="block size-full rounded-[inherit] bg-accent-soft"
                animate={reduceMotion ? undefined : { scaleX: [1, 1.12, 1] }}
                transition={{ duration: 0.16, ease: "easeInOut" }}
              />
            </m.span>
          )}
          <span className="relative z-[1]">Viewer</span>
        </button>
        <button
          disabled={busy}
          className={cn(
            choiceClass,
            optimisticValue === "view_edit" && "border-violet text-violet",
          )}
          onClick={() => void choose("view_edit")}
        >
          {optimisticValue === "view_edit" && (
            <m.span
              className="absolute inset-0 rounded-[7px]"
              layoutId="permission-knob"
              initial={false}
              transition={{ layout: { duration: 0.16, ease: "easeInOut" } }}
            >
              <m.span
                className="block size-full rounded-[inherit] bg-violet-soft"
                animate={reduceMotion ? undefined : { scaleX: [1, 1.12, 1] }}
                transition={{ duration: 0.16, ease: "easeInOut" }}
              />
            </m.span>
          )}
          <span className="relative z-[1]">Editor</span>
        </button>
      </div>
    </LayoutGroup>
  );
}

function GroupDetail({
  group,
  users,
  databases,
}: {
  group: Group;
  users: AdminUser[];
  databases: ManagedDatabase[];
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
      <header className="flex min-h-[94px] items-center gap-3 border-b border-line p-4">
        <div>
          <span className="text-muted">Group</span>
          <h2 className="mt-1 text-[17px] font-bold">{group.name}</h2>
        </div>
      </header>
      <section className={detailSectionClass}>
        <AssignmentHeading>
          <h3 className="text-xs font-bold">Members</h3>
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
          {members.loading ? (
            <Loading />
          ) : (
            members.data?.map((member, index) => (
              <StaggerRow
                index={index}
                className={assignedRowClass}
                key={member.id}
              >
                <span className={avatarClass}>
                  {member.username.slice(0, 2).toUpperCase()}
                </span>
                <span>
                  <b>{member.username}</b>
                  <small>User #{member.id}</small>
                </span>
                <ConfirmAction
                  trigger={<Icon name="close" />}
                  triggerClassName={removeButtonClass}
                  triggerAriaLabel={`Remove ${member.username}`}
                  title={`Remove ${member.username} from ${group.name}?`}
                  description="The user will lose access inherited through this group."
                  confirmLabel="Remove member"
                  onConfirm={() => removeMember(member.id)}
                />
              </StaggerRow>
            ))
          )}
        </AssignedList>
        {!members.loading && !members.data?.length && (
          <AssignmentEmpty>No members</AssignmentEmpty>
        )}
        <AnimatePresence>
          {memberPicker && (
            <PickerPopover>
              <Search
                value={memberSearch}
                onChange={setMemberSearch}
                placeholder="Search users"
              />
              <div className="mt-[7px] max-h-[190px] overflow-auto">
                {availableMembers.map((user) => (
                  <button
                    className={pickerOptionClass}
                    key={user.id}
                    onClick={() => addMember(user.id)}
                  >
                    <span className={cn(avatarClass, "size-8 rounded-[9px]")}>
                      {user.username.slice(0, 2).toUpperCase()}
                    </span>
                    <span>
                      <b>{user.username}</b>
                      <small>User #{user.id}</small>
                    </span>
                    <em className="text-right text-[8px] font-bold not-italic text-violet">
                      Add
                    </em>
                  </button>
                ))}
                {!availableMembers.length && (
                  <p className="m-0 p-[18px] text-center text-[9px] text-muted">
                    No users available
                  </p>
                )}
              </div>
            </PickerPopover>
          )}
        </AnimatePresence>
      </section>

      <section className={detailSectionClass}>
        <AssignmentHeading>
          <h3 className="text-xs font-bold">Database access</h3>
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
          {permissions.loading ? (
            <Loading />
          ) : (
            permissions.data?.map((item, index) => {
              const database = databases.find(
                (entry) => entry.id === item.database_id,
              );
              return (
                <StaggerRow
                  index={index}
                  className={cn(
                    assignedRowClass,
                    "grid-cols-[38px_minmax(0,1fr)_auto_32px] max-[600px]:grid-cols-[38px_minmax(0,1fr)_32px]",
                  )}
                  key={item.database_id}
                >
                  <span
                    className={cn(
                      glyphClass,
                      "max-[600px]:col-start-1 max-[600px]:row-start-1",
                    )}
                  >
                    <Icon name="database" />
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
                    trigger={<Icon name="close" />}
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
        <AnimatePresence>
          {databasePicker && (
            <PickerPopover>
              <Search
                value={databaseSearch}
                onChange={setDatabaseSearch}
                placeholder="Search databases"
              />
              <div className="mt-[7px] max-h-[190px] overflow-auto">
                {availableDatabases.map((database, index) => (
                  <StaggerRow
                    index={index}
                    className="grid min-h-[50px] w-full grid-cols-[34px_minmax(0,1fr)_auto] items-center gap-[9px] rounded-lg p-[7px] text-left text-ink max-[600px]:grid-cols-[34px_minmax(0,1fr)] [&_b]:block [&_b]:text-[9px] [&_small]:mt-[3px] [&_small]:block [&_small]:text-[8px] [&_small]:text-muted"
                    key={database.id}
                  >
                    <span className={cn(glyphClass, "size-8 rounded-[9px]")}>
                      <Icon name="database" />
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
                  <p className="m-0 p-[18px] text-center text-[9px] text-muted">
                    No databases available
                  </p>
                )}
              </div>
            </PickerPopover>
          )}
        </AnimatePresence>
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

  if (groups.loading || users.loading || databases.loading)
    return <Loading page />;

  return (
    <>
      <UtilityHeader
        path="Administration"
        title="Groups"
        status={`${groups.data?.length ?? 0} groups`}
        actions={
          <Button variant="dark" onClick={() => setShowCreate((prev) => !prev)}>
            <Icon name="plus" />
            {showCreate ? "Close" : "Add group"}
          </Button>
        }
      />
      {(groups.error || users.error || databases.error || error) && (
        <ErrorNotice
          message={groups.error || users.error || databases.error || error}
        />
      )}
      {showCreate && (
        <form
          className={cn(
            surfaceClass,
            "mb-3 flex items-end gap-3 px-4 py-[14px] max-[900px]:flex-wrap max-[900px]:items-start max-[600px]:[&>div]:w-full max-[600px]:[&_button]:w-full",
          )}
          onSubmit={createGroup}
        >
          <label className="grid flex-1 gap-[7px] text-[11px] font-semibold max-[900px]:min-w-[min(100%,260px)]">
            <span>Group name</span>
            <input
              className="h-11 w-full rounded-[10px] border border-line-strong px-3"
              value={name}
              onChange={(event) => setName(event.target.value)}
              required
              autoFocus
              placeholder="Customer editors"
            />
          </label>
          <div className="flex gap-2">
            <Button onClick={() => setShowCreate(false)}>Cancel</Button>
            <Button type="submit" variant="dark">
              Create group
            </Button>
          </div>
        </form>
      )}
      <div className="grid min-w-0 grid-cols-1 gap-3 min-[901px]:min-h-[calc(100dvh-84px)] min-[901px]:grid-cols-[360px_minmax(560px,1fr)]">
        <section className={cn(surfaceClass, "min-w-0 overflow-hidden")}>
          <header className="flex min-h-16 items-center border-b border-line px-3 py-[10px]">
            <h2 className="font-bold">Access groups</h2>
            <span className="ml-auto text-[9px] text-muted">
              {groups.data?.length ?? 0} groups
            </span>
          </header>
          {groups.data?.map((group) => (
            <button
              className={cn(
                "grid min-h-[68px] w-full grid-cols-[40px_minmax(0,1fr)_48px_18px] items-center gap-[10px] border-0 border-b border-line bg-white px-3 py-[9px] text-left transition-colors cursor-pointer",
                selected?.id === group.id &&
                  "bg-violet-soft shadow-[inset_3px_0_var(--violet)]",
              )}
              key={group.id}
              onClick={() => selectGroup(group)}
            >
              <span className={glyphClass}>
                <Icon name="groups" />
              </span>
              <span>
                <b className="block">{group.name}</b>
                <small className="mt-1 block font-mono text-[9px] text-muted">
                  Group #{group.id}
                </small>
              </span>
              <em className="text-[8px] not-italic text-violet">Manage</em>
              <Icon name="chevron" />
            </button>
          ))}
        </section>
        <section className={cn(surfaceClass, "relative min-w-0 overflow-auto")}>
          <AnimatePresence initial={false} mode="popLayout">
            {selected ? (
              <m.div
                className="h-full"
                key={`group-${selected.id}`}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{
                  opacity: 0,
                  transition: { duration: 0.12, ease: "easeOut" },
                }}
                transition={{ duration: 0.15, ease: "easeOut" }}
              >
                <GroupDetail
                  group={selected}
                  users={users.data ?? []}
                  databases={databases.data ?? []}
                />
              </m.div>
            ) : (
              <m.div
                className="h-full"
                key="group-empty"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{
                  opacity: 0,
                  transition: { duration: 0.14, ease: "easeOut" },
                }}
              >
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
                  Choose a group to manage members and database access.
                </EmptyState>
              </m.div>
            )}
          </AnimatePresence>
        </section>
      </div>
    </>
  );
}
