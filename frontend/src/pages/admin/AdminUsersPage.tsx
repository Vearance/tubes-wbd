import { useState, type FormEvent } from "react";
import { AnimatePresence, m } from "motion/react";
import { useNavigate, useParams } from "react-router";
import {
  api,
  type AdminUser,
  type DatabasePermission,
  type Group,
  type ManagedDatabase,
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
  buttonClass,
  detailSectionClass,
  glyphClass,
  PickerAction,
  PickerPopover,
  pickerOptionClass,
  removeButtonClass,
  StaggerRow,
  surfaceClass,
} from "./common";

type UserGroupAccess = { group: Group; permissions: DatabasePermission[] };

function UserAccessDetail({
  user,
  groups,
  databases,
  onToggle,
}: {
  user: AdminUser;
  groups: Group[];
  databases: ManagedDatabase[];
  onToggle: () => Promise<void>;
}) {
  const access = useResource(async () => {
    if (user.is_admin) return [];
    const memberships = await Promise.all(
      groups.map(async (group) => {
        const members = await api.groupMembers(group.id);
        return members.some((member) => member.id === user.id)
          ? { group, permissions: await api.groupDatabases(group.id) }
          : null;
      }),
    );
    return memberships.filter(
      (entry): entry is UserGroupAccess => entry !== null,
    );
  }, [user.id, user.is_admin, groups]);

  const [groupPicker, setGroupPicker] = useState(false);
  const [membershipError, setMembershipError] = useState("");
  const [accountError, setAccountError] = useState("");

  const availableGroups = groups.filter(
    (group) => !access.data?.some((entry) => entry.group.id === group.id),
  );

  const databaseAccess = (access.data ?? []).flatMap((entry) =>
    entry.permissions.map((permission) => ({
      group: entry.group,
      permission,
      database: databases.find(
        (database) => database.id === permission.database_id,
      ),
    })),
  );

  async function addMembership(groupId: number) {
    setMembershipError("");
    setGroupPicker(false);
    try {
      await api.addGroupMember(groupId, user.id);
      await access.reload();
    } catch (e) {
      setMembershipError(
        e instanceof Error ? e.message : "Unable to add group membership",
      );
      setGroupPicker(true);
    }
  }

  async function removeMembership(groupId: number) {
    setMembershipError("");
    try {
      await api.removeGroupMember(groupId, user.id);
      await access.reload();
    } catch (e) {
      setMembershipError(
        e instanceof Error ? e.message : "Unable to remove group membership",
      );
    }
  }

  async function toggleAccount() {
    setAccountError("");
    try {
      await onToggle();
    } catch (e) {
      setAccountError(
        e instanceof Error ? e.message : "Unable to update account",
      );
    }
  }

  return (
    <>
      <header className="flex min-h-[94px] items-center gap-3 border-b border-line p-4">
        <span className={avatarClass}>
          {user.username.slice(0, 2).toUpperCase()}
        </span>
        <span className="flex-1">
          <small className="text-[8px] text-muted">
            {user.is_admin ? "Administrator" : "Database user"}
          </small>
          <h2 className="mt-1 text-[17px] font-bold">{user.username}</h2>
        </span>
        <Status tone={user.disabled ? "red" : "green"}>
          {user.disabled ? "Disabled" : "Active"}
        </Status>
      </header>

      {!user.is_admin && (
        <>
          <section className={detailSectionClass}>
            <AssignmentHeading>
              <h3 className="text-xs font-bold">Group membership</h3>
              <PickerAction
                open={groupPicker}
                label="Add group"
                className="min-w-[132px] max-[600px]:w-full max-[600px]:min-w-0"
                onClick={() => setGroupPicker(!groupPicker)}
              />
            </AssignmentHeading>
            {(access.error || membershipError) && (
              <ErrorNotice message={access.error || membershipError} />
            )}
            {access.loading ? (
              <Loading />
            ) : (
              <AssignedList>
                {access.data?.map((entry, index) => (
                  <StaggerRow
                    index={index}
                    className={assignedRowClass}
                    key={entry.group.id}
                  >
                    <span className={glyphClass}>
                      <Icon name="groups" />
                    </span>
                    <span>
                      <b>{entry.group.name}</b>
                      <small>
                        {entry.permissions.length} database
                        {entry.permissions.length === 1 ? "" : "s"}
                      </small>
                    </span>
                    <ConfirmAction
                      trigger={<Icon name="close" />}
                      triggerClassName={removeButtonClass}
                      triggerAriaLabel={`Remove ${user.username} from ${entry.group.name}`}
                      title={`Remove ${user.username} from ${entry.group.name}?`}
                      description="The user will lose access inherited through this group."
                      confirmLabel="Remove membership"
                      onConfirm={() => removeMembership(entry.group.id)}
                    />
                  </StaggerRow>
                ))}
              </AssignedList>
            )}
            {!access.loading && !access.data?.length && (
              <AssignmentEmpty>No groups</AssignmentEmpty>
            )}
            <AnimatePresence>
              {groupPicker && (
                <PickerPopover>
                  <div className="mt-[7px] max-h-[190px] overflow-auto">
                    {availableGroups.map((group) => (
                      <button
                        className={pickerOptionClass}
                        key={group.id}
                        onClick={() => addMembership(group.id)}
                      >
                        <span
                          className={cn(glyphClass, "size-8 rounded-[9px]")}
                        >
                          <Icon name="groups" />
                        </span>
                        <span>
                          <b>{group.name}</b>
                          <small>Group #{group.id}</small>
                        </span>
                        <em className="text-right text-[8px] font-bold not-italic text-violet">
                          Add
                        </em>
                      </button>
                    ))}
                    {!availableGroups.length && (
                      <p className="m-0 p-[18px] text-center text-[9px] text-muted">
                        No groups available
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
            </AssignmentHeading>
            <AssignedList>
              {databaseAccess.map(({ group, permission, database }, index) => (
                <StaggerRow
                  index={index}
                  className={cn(
                    assignedRowClass,
                    "grid-cols-[38px_minmax(0,1fr)_auto]",
                  )}
                  key={`${group.id}-${permission.database_id}`}
                >
                  <span className={glyphClass}>
                    <Icon name="database" />
                  </span>
                  <span>
                    <b>
                      {database?.name ?? `Database #${permission.database_id}`}
                    </b>
                    <small>{group.name}</small>
                  </span>
                  <span
                    className={cn(
                      "mt-1 inline-flex w-fit rounded-full px-[7px] py-1 text-[8px] font-extrabold",
                      permission.permission === "view_edit"
                        ? "bg-violet-soft text-violet"
                        : "bg-accent-soft text-accent",
                    )}
                  >
                    {permission.permission === "view_edit"
                      ? "Editor"
                      : "Viewer"}
                  </span>
                </StaggerRow>
              ))}
            </AssignedList>
            {!access.loading && !databaseAccess.length && (
              <AssignmentEmpty>No database access</AssignmentEmpty>
            )}
          </section>
        </>
      )}

      <section className={detailSectionClass}>
        <AssignmentHeading>
          <h3 className="text-xs font-bold">Account</h3>
          {user.disabled ? (
            <Button
              variant="dark"
              className="min-w-[132px] max-[600px]:w-full max-[600px]:min-w-0"
              onClick={toggleAccount}
            >
              Enable account
            </Button>
          ) : (
            <ConfirmAction
              trigger="Disable account"
              triggerClassName={cn(
                buttonClass,
                "min-w-[132px] max-[600px]:w-full max-[600px]:min-w-0",
              )}
              title={`Disable ${user.username}?`}
              description="This user will be signed out until the account is enabled again."
              confirmLabel="Disable account"
              onConfirm={toggleAccount}
            />
          )}
        </AssignmentHeading>
        {accountError && (
          <ErrorNotice className="my-[10px]" message={accountError} />
        )}
      </section>
    </>
  );
}

export function AdminUsersPage() {
  const { userId } = useParams<{ userId?: string }>();
  const navigate = useNavigate();
  const users = useResource(() => api.adminUsers(), []);
  const groups = useResource(() => api.adminGroups(), []);
  const databases = useResource(() => api.adminDatabases(), []);
  const [search, setSearch] = useState("");
  const [selectedState, setSelectedState] = useState<AdminUser | null>(null);

  const [showCreate, setShowCreate] = useState(false);
  const [newUsername, setNewUsername] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [createError, setCreateError] = useState("");
  const [creating, setCreating] = useState(false);

  if (users.loading || groups.loading || databases.loading)
    return <Loading page />;

  const selected =
    (userId
      ? users.data?.find((user) => String(user.id) === userId)
      : null) ?? selectedState;

  function selectUser(user: AdminUser) {
    setSelectedState(user);
    navigate(`/admin/users/${user.id}`);
  }

  const shown =
    users.data?.filter((user) =>
      user.username.toLowerCase().includes(search.toLowerCase()),
    ) ?? [];

  async function toggle(user: AdminUser) {
    const updated = await api.updateUser(user.id, !user.disabled);
    setSelectedState(updated);
    await users.reload();
  }

  async function handleCreateUser(e: FormEvent) {
    e.preventDefault();
    setCreating(true);
    setCreateError("");
    try {
      const created = await api.createUser(newUsername, newPassword);
      setNewUsername("");
      setNewPassword("");
      setShowCreate(false);
      await users.reload();
      selectUser(created);
    } catch (err) {
      setCreateError(
        err instanceof Error ? err.message : "Unable to create user",
      );
    } finally {
      setCreating(false);
    }
  }

  return (
    <>
      <UtilityHeader
        path="Administration"
        title="Users"
        status={`${users.data?.length ?? 0} users`}
        actions={
          <Button variant="dark" onClick={() => setShowCreate((prev) => !prev)}>
            <Icon name="plus" />
            {showCreate ? "Close" : "Add user"}
          </Button>
        }
      />
      {(users.error || groups.error || databases.error || createError) && (
        <ErrorNotice
          message={
            users.error || groups.error || databases.error || createError
          }
        />
      )}

      {showCreate && (
        <form
          onSubmit={handleCreateUser}
          className={cn(surfaceClass, "mb-4 p-5 space-y-4 shadow-xs")}
        >
          <div className="flex items-center justify-between border-b border-line pb-3">
            <h3 className="m-0 text-base font-bold text-ink">
              Create New User
            </h3>
            <span className="text-xs text-muted">
              Add regular database user
            </span>
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <label className="block text-xs font-bold text-ink">
              <span>Username</span>
              <input
                type="text"
                required
                autoComplete="off"
                placeholder="e.g. alex"
                value={newUsername}
                onChange={(e) => setNewUsername(e.target.value)}
                className="mt-1.5 h-11 w-full rounded-[10px] border border-line-strong bg-white px-3 text-xs outline-none focus:border-violet"
              />
            </label>
            <label className="block text-xs font-bold text-ink">
              <span>Password</span>
              <input
                type="password"
                required
                autoComplete="new-password"
                placeholder="••••••••"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                className="mt-1.5 h-11 w-full rounded-[10px] border border-line-strong bg-white px-3 text-xs outline-none focus:border-violet"
              />
            </label>
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <Button onClick={() => setShowCreate(false)}>Cancel</Button>
            <Button type="submit" variant="dark" disabled={creating}>
              <Icon name="save" />
              {creating ? "Creating…" : "Save user"}
            </Button>
          </div>
        </form>
      )}

      <div className="grid min-w-0 grid-cols-1 gap-3 min-[901px]:min-h-[calc(100dvh-84px)] min-[901px]:grid-cols-[360px_minmax(560px,1fr)]">
        <section className={cn(surfaceClass, "min-w-0 overflow-hidden")}>
          <header className="flex min-h-16 items-center border-b border-line px-3 py-[10px]">
            <Search
              value={search}
              onChange={setSearch}
              placeholder="Search users"
            />
          </header>
          {shown.map((user) => (
            <button
              className={cn(
                "grid min-h-[68px] w-full grid-cols-[40px_minmax(0,1fr)_18px] items-center gap-[10px] border-0 border-b border-line bg-white px-3 py-[9px] text-left transition-colors cursor-pointer",
                selected?.id === user.id &&
                  "bg-violet-soft shadow-[inset_3px_0_var(--violet)]",
              )}
              key={user.id}
              onClick={() => selectUser(user)}
            >
              <span className={avatarClass}>
                {user.username.slice(0, 2).toUpperCase()}
              </span>
              <span>
                <b className="block">{user.username}</b>
                <code className="mt-1 block font-mono text-[9px] text-muted">
                  #{user.id} {user.disabled ? "· Disabled" : ""}
                </code>
              </span>
              <Icon name="chevron" />
            </button>
          ))}
          {shown.length === 0 && (
            <div className="p-8 text-center text-xs text-muted">
              No users found matching &ldquo;{search}&rdquo;.
            </div>
          )}
        </section>
        <section className={cn(surfaceClass, "relative min-w-0")}>
          <AnimatePresence initial={false} mode="popLayout">
            {selected ? (
              <m.div
                className="h-full"
                key={`user-${selected.id}`}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{
                  opacity: 0,
                  transition: { duration: 0.12, ease: "easeOut" },
                }}
                transition={{ duration: 0.15, ease: "easeOut" }}
              >
                <UserAccessDetail
                  user={selected}
                  groups={groups.data ?? []}
                  databases={databases.data ?? []}
                  onToggle={() => toggle(selected)}
                />
              </m.div>
            ) : (
              <m.div
                className="h-full"
                key="user-empty"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{
                  opacity: 0,
                  transition: { duration: 0.14, ease: "easeOut" },
                }}
              >
                <EmptyState
                  icon="users"
                  title="Select a user"
                  action={
                    shown[0] ? (
                      <Button onClick={() => selectUser(shown[0])}>
                        View first user
                      </Button>
                    ) : undefined
                  }
                >
                  Choose an account to inspect status and access.
                </EmptyState>
              </m.div>
            )}
          </AnimatePresence>
        </section>
      </div>
    </>
  );
}
