import { useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { AnimatePresence, m } from "motion/react";
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
  Modal,
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
      <header className="flex min-h-[84px] items-center gap-3.5 border-b border-line p-5">
        <span className={cn(avatarClass, "size-11 text-sm")}>
          {user.username.slice(0, 2).toUpperCase()}
        </span>
        <span className="flex-1 min-w-0">
          <small className="block text-[11px] font-bold uppercase tracking-wider text-muted">
            {user.is_admin ? "Administrator" : `User #${user.id}`}
          </small>
          <h2 className="mt-1 mb-0 truncate text-lg font-bold text-ink">
            {user.username}
          </h2>
        </span>
        <Status
          tone={user.disabled ? "red" : "green"}
          className="px-3 py-1 text-[11px]"
        >
          {user.disabled ? "Disabled" : "Active"}
        </Status>
      </header>

      {!user.is_admin && (
        <>
          <section className={detailSectionClass}>
            <AssignmentHeading>
              <h3 className="text-sm font-bold text-ink">
                Group membership ({access.data?.length ?? 0})
              </h3>
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
            {access.loading && !access.data ? (
              <Loading />
            ) : (
              <AssignedList>
                {access.data?.map((entry) => {
                  const isLeader = entry.group.leader_user_id === user.id;
                  return (
                    <StaggerRow
                      className={cn(
                        assignedRowClass,
                        "grid-cols-[36px_minmax(0,1fr)_auto_32px]",
                      )}
                      key={entry.group.id}
                    >
                      <span className={glyphClass}>
                        <Icon name="groups" className="size-4" />
                      </span>
                      <span>
                        <b className="flex items-center gap-1.5">
                          <Link
                            to={`/admin/groups/${entry.group.id}`}
                            className="truncate text-ink no-underline hover:text-violet hover:underline"
                          >
                            {entry.group.name}
                          </Link>
                        </b>
                        <small>
                          {entry.permissions.length} database
                          {entry.permissions.length === 1 ? "" : "s"}
                        </small>
                      </span>
                      {isLeader ? (
                        <Status tone="violet" className="gap-1 text-[10px]">
                          <Icon name="crown" className="size-3" />
                          Keyholder
                        </Status>
                      ) : (
                        <span className="text-[11px] font-medium text-muted">
                          Member
                        </span>
                      )}
                      {isLeader ? (
                        <Link
                          to={`/admin/groups/${entry.group.id}`}
                          className="grid size-8 place-items-center rounded-lg text-violet hover:bg-violet-soft"
                          title="Reassign Keyholder in group before removing"
                          aria-label={`Manage Keyholder in ${entry.group.name}`}
                        >
                          <Icon name="lock" className="size-4" />
                        </Link>
                      ) : (
                        <ConfirmAction
                          trigger={<Icon name="close" className="size-4" />}
                          triggerClassName={removeButtonClass}
                          triggerAriaLabel={`Remove ${user.username} from ${entry.group.name}`}
                          title={`Remove ${user.username} from ${entry.group.name}?`}
                          description="The user will lose access inherited through this group."
                          confirmLabel="Remove membership"
                          onConfirm={() => removeMembership(entry.group.id)}
                        />
                      )}
                    </StaggerRow>
                  );
                })}
              </AssignedList>
            )}
            {!access.loading && !access.data?.length && (
              <AssignmentEmpty>No groups</AssignmentEmpty>
            )}
            {groupPicker && (
              <PickerPopover>
                <div className="max-h-[200px] overflow-auto">
                  {availableGroups.map((group) => (
                    <button
                      type="button"
                      className={pickerOptionClass}
                      key={group.id}
                      onClick={() => addMembership(group.id)}
                    >
                      <span
                        className={cn(glyphClass, "size-8 rounded-[9px]")}
                      >
                        <Icon name="groups" className="size-4" />
                      </span>
                      <span>
                        <b>{group.name}</b>
                        <small>
                          {group.leader_username
                            ? `@${group.leader_username}`
                            : `Group #${group.id}`}
                        </small>
                      </span>
                      <em className="text-right text-xs font-bold not-italic text-violet">
                        Add
                      </em>
                    </button>
                  ))}
                  {!availableGroups.length && (
                    <p className="m-0 p-4 text-center text-xs text-muted">
                      No groups available
                    </p>
                  )}
                </div>
              </PickerPopover>
            )}
          </section>

          <section className={detailSectionClass}>
            <AssignmentHeading>
              <h3 className="text-sm font-bold text-ink">
                Database access ({databaseAccess.length})
              </h3>
            </AssignmentHeading>
            <AssignedList>
              {databaseAccess.map(({ group, permission, database }) => (
                <StaggerRow
                  className={cn(
                    assignedRowClass,
                    "grid-cols-[36px_minmax(0,1fr)_auto]",
                  )}
                  key={`${group.id}-${permission.database_id}`}
                >
                  <span className={glyphClass}>
                    <Icon name="database" className="size-4" />
                  </span>
                  <span>
                    <b>
                      {database?.name ?? `Database #${permission.database_id}`}
                    </b>
                    <small>{group.name}</small>
                  </span>
                  <Status
                    tone={
                      permission.permission === "view_edit" ? "violet" : "accent"
                    }
                  >
                    {permission.permission === "view_edit"
                      ? "Editor"
                      : "Viewer"}
                  </Status>
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
          <h3 className="m-0 text-sm font-bold text-ink">Account</h3>
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
          <ErrorNotice className="my-2.5" message={accountError} />
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

  const isLoading =
    (users.loading && !users.data) ||
    (groups.loading && !groups.data) ||
    (databases.loading && !databases.data);

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
        status={
          isLoading ? "Loading users…" : `${users.data?.length ?? 0} users`
        }
        actions={
          <Button variant="dark" onClick={() => setShowCreate(true)}>
            <Icon name="plus" />
            Add user
          </Button>
        }
      />

      <Modal
        open={showCreate}
        onOpenChange={setShowCreate}
        title="Create New User"
        description="Register a new platform user account with username and password."
        size="md"
      >
        <form onSubmit={handleCreateUser} className="space-y-4">
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
          <div className="flex justify-end gap-2 pt-2 border-t border-line/60">
            <Button onClick={() => setShowCreate(false)}>Cancel</Button>
            <Button type="submit" variant="dark" disabled={creating}>
              <Icon name="save" />
              {creating ? "Creating…" : "Save user"}
            </Button>
          </div>
        </form>
      </Modal>

      <AnimatePresence mode="wait">
        {isLoading ? (
          <m.div
            key="users-loading"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18, ease: "easeOut" }}
            className="py-16"
          >
            <Loading label="Loading registered users…" />
          </m.div>
        ) : (
          <m.div
            key="users-content"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.22, ease: "easeOut" }}
            className="space-y-4"
          >
            {(users.error || groups.error || databases.error || createError) && (
              <ErrorNotice
                message={
                  users.error || groups.error || databases.error || createError
                }
              />
            )}

      <div className="grid min-w-0 grid-cols-1 gap-3 min-[901px]:min-h-[calc(100dvh-110px)] min-[901px]:grid-cols-[360px_minmax(560px,1fr)]">
        <section className={cn(surfaceClass, "min-w-0 overflow-hidden")}>
          <header className="flex min-h-16 items-center border-b border-line px-3 py-2.5">
            <Search
              value={search}
              onChange={setSearch}
              placeholder="Search users"
            />
          </header>
          {shown.map((user) => {
            const ledCount = (groups.data ?? []).filter(
              (g) => g.leader_user_id === user.id,
            ).length;
            return (
              <button
                type="button"
                className={cn(
                  "grid min-h-[72px] w-full grid-cols-[38px_minmax(0,1fr)_auto_18px] items-center gap-3 border-0 border-b border-line bg-white px-4 py-3 text-left hover:bg-violet-soft/20 cursor-pointer transition-colors duration-150",
                  selected?.id === user.id &&
                    "bg-violet-soft/60 shadow-[inset_3px_0_var(--violet)]",
                )}
                key={user.id}
                onClick={() => selectUser(user)}
              >
                <span className={avatarClass}>
                  {user.username.slice(0, 2).toUpperCase()}
                </span>
                <span className="min-w-0">
                  <b className="block truncate text-xs font-bold text-ink">
                    {user.username}
                  </b>
                  <span className="mt-1 flex flex-wrap items-center gap-1.5 font-mono text-[11px] text-muted">
                    <span>#{user.id}</span>
                    <span>·</span>
                    <span
                      className={cn(
                        user.disabled
                          ? "text-danger font-semibold"
                          : user.is_admin
                            ? "text-violet font-semibold"
                            : "text-success font-medium",
                      )}
                    >
                      {user.disabled ? "Disabled" : user.is_admin ? "Admin" : "Active"}
                    </span>
                  </span>
                </span>
                {ledCount > 0 ? (
                  <Status tone="violet" className="gap-1 text-[10px]">
                    <Icon name="crown" className="size-3" />
                    {ledCount}
                  </Status>
                ) : (
                  <span />
                )}
                <Icon name="chevron" className="size-4 text-muted" />
              </button>
            );
          })}
          {shown.length === 0 && (
            <div className="p-8 text-center text-xs text-muted">
              No users found matching &ldquo;{search}&rdquo;.
            </div>
          )}
        </section>
        <section className={cn(surfaceClass, "relative min-w-0")}>
          {selected ? (
            <UserAccessDetail
              key={selected.id}
              user={selected}
              groups={groups.data ?? []}
              databases={databases.data ?? []}
              onToggle={() => toggle(selected)}
            />
          ) : (
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
          )}
        </section>
      </div>
          </m.div>
        )}
      </AnimatePresence>
    </>
  );
}
