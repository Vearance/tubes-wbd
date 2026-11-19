import { useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router";
import {
  api,
  type ManagedDatabase,
  type ManagedDatabaseInput,
} from "../../api";
import { UtilityHeader } from "../../components/AppShell";
import { Icon } from "../../components/Icon";
import {
  Button,
  ConfirmAction,
  EmptyState,
  ErrorNotice,
  Loading,
  Status,
} from "../../components/ui";
import { useResource } from "../../hooks";
import { cn } from "../../lib/utils";
import { glyphClass, surfaceClass } from "./common";

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
    "grid gap-[7px] text-[11px] font-semibold text-ink [&_input]:h-11 [&_input]:w-full [&_input]:rounded-[10px] [&_input]:border [&_input]:border-line-strong [&_input]:bg-white [&_input]:px-3 [&_input]:text-ink [&_input]:focus:border-violet [&_select]:h-11 [&_select]:w-full [&_select]:rounded-[10px] [&_select]:border [&_select]:border-line-strong [&_select]:bg-white [&_select]:px-3 [&_select]:text-ink [&_select]:focus:border-violet";

  return (
    <form className={cn(surfaceClass, "mb-4 overflow-hidden")} onSubmit={save}>
      <header className="border-b border-line px-[22px] py-5">
        <h2 className="m-0 text-xl font-bold text-balance">
          {initialData
            ? "Edit PostgreSQL database connection"
            : "Add PostgreSQL database"}
        </h2>
      </header>
      <div className="grid grid-cols-1 gap-4 p-4 min-[601px]:p-[22px] min-[901px]:grid-cols-2">
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
        <label className={fieldClass}>
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
          className="mx-[22px] mb-4 rounded-[10px] bg-violet-soft px-3 py-[10px] text-[10px] text-[#60428b]"
          role="status"
        >
          {message}
        </p>
      )}
      <footer className="flex flex-wrap justify-end gap-2 border-t border-line px-4 py-3 min-[601px]:px-[22px] min-[601px]:py-[14px] min-[901px]:flex-nowrap [&_button]:flex-auto min-[601px]:[&_button]:flex-1 min-[901px]:[&_button]:flex-none max-[600px]:[&_button]:w-full">
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
  const [selectedState, setSelectedState] = useState<ManagedDatabase | null>(
    null,
  );
  const [showForm, setShowForm] = useState(false);
  const [editingDb, setEditingDb] = useState<ManagedDatabase | null>(null);
  const [message, setMessage] = useState("");

  const selected =
    (databaseId
      ? databases.data?.find((db) => String(db.id) === databaseId)
      : null) ?? selectedState;

  function selectDatabase(db: ManagedDatabase) {
    setSelectedState(db);
    navigate(`/admin/databases/${db.id}`);
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

  if (databases.loading) return <Loading page />;

  return (
    <>
      <UtilityHeader
        path="Administration"
        title="Databases"
        status={`${databases.data?.length ?? 0} connections`}
        actions={
          databases.data?.length && !showForm ? (
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
          ) : undefined
        }
      />
      {databases.error && <ErrorNotice message={databases.error} />}
      {showForm && (
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
      )}
      <div className="grid grid-cols-1 gap-3 min-[901px]:grid-cols-3">
        {databases.data?.map((database) => (
          <button
            className={cn(
              "grid min-h-[78px] w-full grid-cols-[40px_minmax(0,1fr)_auto] items-center gap-[10px] rounded-[14px] border border-line bg-white p-[14px] text-left no-underline transition-colors hover:border-violet cursor-pointer",
              selected?.id === database.id && "border-violet bg-violet-soft/20",
            )}
            onClick={() => selectDatabase(database)}
            key={database.id}
          >
            <span className={glyphClass}>
              <Icon name="database" />
            </span>
            <span>
              <strong className="block">{database.name}</strong>
              <small className="mt-1 block text-[9px] text-muted truncate">
                {database.username}@{database.host}:{database.port}/
                {database.db_name}
              </small>
            </span>
            <Status tone="green">{database.ssl_mode}</Status>
          </button>
        ))}
      </div>
      {!databases.data?.length && !showForm && (
        <section
          className={cn(
            surfaceClass,
            "grid min-h-[calc(100dvh-112px)] place-items-center",
          )}
        >
          <EmptyState
            icon="database"
            title="No databases"
            action={
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
          >
            Register a PostgreSQL database for group access.
          </EmptyState>
        </section>
      )}
      {selected && !showForm && (
        <section
          className={cn(
            surfaceClass,
            "mt-[14px] flex flex-wrap items-center gap-[18px] p-5 shadow-xs",
          )}
        >
          <div className="flex-1 min-w-[240px]">
            <p className="m-0 text-[10px] font-bold uppercase tracking-[.08em] text-violet">
              Active Connection
            </p>
            <h2 className="my-1 text-xl font-bold">{selected.name}</h2>
            <p className="text-xs text-muted m-0">
              {selected.host}:{selected.port} · {selected.db_name} (SSL:{" "}
              {selected.ssl_mode})
            </p>
          </div>
          <div className="flex items-center gap-2 flex-wrap">
            <Button onClick={() => test(selected.id)}>
              <Icon name="refresh" />
              Test connection
            </Button>
            <Button
              onClick={() => {
                setEditingDb(selected);
                setShowForm(true);
              }}
            >
              <Icon name="edit" />
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
                <Button className="border-danger/40 text-danger hover:bg-danger-soft">
                  <Icon name="trash" />
                  Delete
                </Button>
              }
            />
          </div>
          {message && (
            <p
              className="w-full text-xs text-violet font-semibold m-0 bg-violet-soft px-3 py-1.5 rounded-md"
              role="status"
            >
              {message}
            </p>
          )}
        </section>
      )}
    </>
  );
}
