import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { AnimatePresence, m } from 'motion/react'
import { api } from '../../api'
import { UtilityHeader } from '../../components/AppShell'
import { Icon, type IconName } from '../../components/Icon'
import { useWorkspaceContext } from '../../components/workspaceContext'
import {
  Button,
  ErrorNotice,
  Loading,
  Status,
} from '../../components/ui'
import { useResource } from '../../hooks'
import { cn } from '../../lib/utils'

type BlueprintRecipe =
  | 'add_column'
  | 'rename_column'
  | 'alter_column'
  | 'create_index'
  | 'drop_column'

const RECIPES: Array<{
  key: BlueprintRecipe
  label: string
  icon: IconName
}> = [
  { key: 'add_column', label: 'Add Column', icon: 'plus' },
  { key: 'rename_column', label: 'Rename Column', icon: 'edit' },
  { key: 'alter_column', label: 'Change Type', icon: 'columns' },
  { key: 'create_index', label: 'Create Index', icon: 'key' },
  { key: 'drop_column', label: 'Drop Column', icon: 'trash' },
]

const PG_TYPES = [
  'text',
  'integer',
  'bigint',
  'boolean',
  'timestamptz',
  'uuid',
  'numeric(12,2)',
  'jsonb',
]

function qIdent(ident: string): string {
  const clean = ident.trim().replace(/"/g, '""')
  if (/^[a-z_][a-z0-9_]*$/.test(clean)) return clean
  return `"${clean || 'identifier'}"`
}

export function EditPage() {
  const navigate = useNavigate()
  const { databaseId = '0', schema = 'public', table = '' } = useParams()
  const id = Number(databaseId)
  const { databases } = useWorkspaceContext()

  const activeDb = databases.find((db) => db.id === id)
  const databaseName = activeDb?.name ?? `Database #${id}`
  const canEdit = activeDb?.permission === 'view_edit'

  const structure = useResource(
    () => (table ? api.structure(id, schema, table) : Promise.resolve(null)),
    [id, schema, table],
  )

  const [recipe, setRecipe] = useState<BlueprintRecipe>('add_column')
  const [newColName, setNewColName] = useState('notes')
  const [newColType, setNewColType] = useState('text')
  const [nullable, setNullable] = useState(true)
  const [defaultExpr, setDefaultExpr] = useState('')
  const [selectedCol, setSelectedCol] = useState('')
  const [renameTarget, setRenameTarget] = useState('')
  const [indexUnique, setIndexUnique] = useState(false)
  const [indexName, setIndexName] = useState('')
  const [dropReason, setDropReason] = useState(
    'Drops deprecated column and permanently deletes its stored values.',
  )

  const columns = structure.data?.columns ?? []
  const activeColName =
    selectedCol && columns.some((c) => c.name === selectedCol)
      ? selectedCol
      : columns[0]?.name ?? 'id'
  const activeColObj = columns.find((c) => c.name === activeColName)

  const blueprint = useMemo(() => {
    const fqTable = `${qIdent(schema)}.${qIdent(table || 'table_name')}`
    const safeTable = (table || 'table').replace(/[^a-zA-Z0-9_]/g, '_')

    switch (recipe) {
      case 'add_column': {
        const col = qIdent(newColName || 'new_column')
        const nullClause = nullable ? '' : ' NOT NULL'
        const defClause = defaultExpr.trim()
          ? ` DEFAULT ${defaultExpr.trim()}`
          : ''
        return {
          draftName: `add_${newColName || 'column'}_to_${safeTable}`,
          upSql: `ALTER TABLE ${fqTable}\n  ADD COLUMN ${col} ${newColType}${nullClause}${defClause};`,
          downSql: `ALTER TABLE ${fqTable}\n  DROP COLUMN IF EXISTS ${col};`,
          irreversibleReason: '',
        }
      }
      case 'rename_column': {
        const from = qIdent(activeColName)
        const to = qIdent(renameTarget || `${activeColName}_v2`)
        return {
          draftName: `rename_${activeColName}_on_${safeTable}`,
          upSql: `ALTER TABLE ${fqTable}\n  RENAME COLUMN ${from} TO ${to};`,
          downSql: `ALTER TABLE ${fqTable}\n  RENAME COLUMN ${to} TO ${from};`,
          irreversibleReason: '',
        }
      }
      case 'alter_column': {
        const col = qIdent(activeColName)
        const prevType = activeColObj?.type ?? 'text'
        return {
          draftName: `alter_${activeColName}_type_on_${safeTable}`,
          upSql: `ALTER TABLE ${fqTable}\n  ALTER COLUMN ${col} TYPE ${newColType};`,
          downSql: `ALTER TABLE ${fqTable}\n  ALTER COLUMN ${col} TYPE ${prevType};`,
          irreversibleReason: '',
        }
      }
      case 'create_index': {
        const col = qIdent(activeColName)
        const idx = qIdent(
          indexName.trim() || `idx_${safeTable}_${activeColName}`,
        )
        const uniqueKw = indexUnique ? 'UNIQUE ' : ''
        return {
          draftName: `create_${indexName.trim() || `idx_${safeTable}_${activeColName}`}`,
          upSql: `CREATE ${uniqueKw}INDEX ${idx}\n  ON ${fqTable} (${col});`,
          downSql: `DROP INDEX IF EXISTS ${qIdent(schema)}.${idx};`,
          irreversibleReason: '',
        }
      }
      case 'drop_column': {
        const col = qIdent(activeColName)
        return {
          draftName: `drop_${activeColName}_from_${safeTable}`,
          upSql: `ALTER TABLE ${fqTable}\n  DROP COLUMN ${col};`,
          downSql: '',
          irreversibleReason:
            dropReason.trim() ||
            'Dropping a column permanently removes stored data.',
        }
      }
    }
  }, [
    recipe,
    schema,
    table,
    newColName,
    newColType,
    nullable,
    defaultExpr,
    activeColName,
    activeColObj?.type,
    renameTarget,
    indexUnique,
    indexName,
    dropReason,
  ])

  const isLoading = structure.loading && !structure.data

  function openInWorkbench() {
    const params = new URLSearchParams({
      newDraft: '1',
      name: blueprint.draftName,
      sql: blueprint.upSql,
    })
    if (blueprint.downSql) {
      params.set('downSql', blueprint.downSql)
    } else if (blueprint.irreversibleReason) {
      params.set('irreversibleReason', blueprint.irreversibleReason)
    }
    navigate(`/databases/${id}/migrations?${params.toString()}`)
  }

  function testInQueryEditor() {
    const params = new URLSearchParams({ sql: blueprint.upSql })
    navigate(`/databases/${id}/${schema}/query?${params.toString()}`)
  }

  return (
    <>
      <UtilityHeader
        backTo={`/databases/${id}/${schema}/${encodeURIComponent(table)}`}
        path={`${databaseName} / ${schema} / ${table}`}
        title="DDL Blueprint"
        status={
          isLoading
            ? 'Inspecting structure…'
            : structure.data
              ? `${structure.data.table.kind} · ${columns.length} columns`
              : undefined
        }
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" size="sm" onClick={testInQueryEditor}>
              <Icon name="code" className="size-3.5" />
              Explain Up SQL
            </Button>
            {canEdit && (
              <Button variant="dark" size="sm" onClick={openInWorkbench}>
                <Icon name="migration" className="size-3.5" />
                Stage Draft
              </Button>
            )}
          </div>
        }
      />

      <AnimatePresence mode="wait">
        {isLoading ? (
          <m.div
            key="blueprint-loading"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18, ease: 'easeOut' }}
            className="py-16"
          >
            <Loading label={`Inspecting ${schema}.${table} structure…`} />
          </m.div>
        ) : (
          <m.div
            key="blueprint-content"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.22, ease: 'easeOut' }}
            className="space-y-4"
          >
            {structure.error && <ErrorNotice message={structure.error} />}

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_360px]">
        <section className="rounded-[20px] border border-line bg-white shadow-xs overflow-hidden">
          {/* Operation Selector */}
          <div className="border-b border-line bg-[#fbfafd] p-3.5 sm:p-4">
            <div className="flex flex-wrap gap-2">
              {RECIPES.map((item) => {
                const active = recipe === item.key
                return (
                  <button
                    key={item.key}
                    type="button"
                    onClick={() => setRecipe(item.key)}
                    className={cn(
                      'inline-flex items-center gap-2 rounded-xl px-3.5 py-2 text-xs font-bold transition-all cursor-pointer',
                      active
                        ? 'bg-ink text-white shadow-xs'
                        : 'border border-line/80 bg-white text-muted hover:border-line-strong hover:text-ink',
                    )}
                  >
                    <Icon name={item.icon} className="size-3.5" />
                    <span>{item.label}</span>
                  </button>
                )
              })}
            </div>
          </div>

          {/* Operation Parameters */}
          <div className="p-6 sm:p-7 space-y-6">
            {recipe === 'add_column' && (
              <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                <label className="block text-sm font-bold text-ink">
                  <span>Column name</span>
                  <input
                    type="text"
                    value={newColName}
                    onChange={(e) => setNewColName(e.target.value)}
                    placeholder="e.g. notes"
                    className="mt-2 h-11 w-full rounded-[12px] border border-line-strong px-3.5 font-mono text-xs outline-none focus:border-violet"
                  />
                </label>
                <label className="block text-sm font-bold text-ink">
                  <span>PostgreSQL type</span>
                  <select
                    value={newColType}
                    onChange={(e) => setNewColType(e.target.value)}
                    className="mt-2 h-11 w-full rounded-[12px] border border-line-strong bg-white px-3.5 font-mono text-xs outline-none focus:border-violet cursor-pointer"
                  >
                    {PG_TYPES.map((t) => (
                      <option key={t} value={t}>
                        {t}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="block text-sm font-bold text-ink">
                  <span>Default expression</span>
                  <input
                    type="text"
                    value={defaultExpr}
                    onChange={(e) => setDefaultExpr(e.target.value)}
                    placeholder="e.g. 'pending' or NOW()"
                    className="mt-2 h-11 w-full rounded-[12px] border border-line-strong px-3.5 font-mono text-xs outline-none focus:border-violet"
                  />
                </label>
                <div className="flex flex-col justify-end">
                  <label className="flex h-11 items-center gap-3 rounded-[12px] border border-line bg-canvas/30 px-4 text-xs font-bold text-ink cursor-pointer hover:border-line-strong transition-colors">
                    <input
                      type="checkbox"
                      checked={nullable}
                      onChange={(e) => setNullable(e.target.checked)}
                      className="size-4 rounded accent-violet"
                    />
                    <span>Nullable</span>
                  </label>
                </div>
              </div>
            )}

            {recipe === 'rename_column' && (
              <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                <label className="block text-sm font-bold text-ink">
                  <span>Target column</span>
                  <select
                    value={activeColName}
                    onChange={(e) => setSelectedCol(e.target.value)}
                    className="mt-2 h-11 w-full rounded-[12px] border border-line-strong bg-white px-3.5 font-mono text-xs outline-none focus:border-violet cursor-pointer"
                  >
                    {columns.map((col) => (
                      <option key={col.name} value={col.name}>
                        {col.name} ({col.type})
                      </option>
                    ))}
                  </select>
                </label>
                <label className="block text-sm font-bold text-ink">
                  <span>New column name</span>
                  <input
                    type="text"
                    value={renameTarget}
                    onChange={(e) => setRenameTarget(e.target.value)}
                    placeholder={`${activeColName}_v2`}
                    className="mt-2 h-11 w-full rounded-[12px] border border-line-strong px-3.5 font-mono text-xs outline-none focus:border-violet"
                  />
                </label>
              </div>
            )}

            {recipe === 'alter_column' && (
              <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                <label className="block text-sm font-bold text-ink">
                  <span>Target column</span>
                  <select
                    value={activeColName}
                    onChange={(e) => setSelectedCol(e.target.value)}
                    className="mt-2 h-11 w-full rounded-[12px] border border-line-strong bg-white px-3.5 font-mono text-xs outline-none focus:border-violet cursor-pointer"
                  >
                    {columns.map((col) => (
                      <option key={col.name} value={col.name}>
                        {col.name} ({col.type})
                      </option>
                    ))}
                  </select>
                </label>
                <label className="block text-sm font-bold text-ink">
                  <span>New PostgreSQL type</span>
                  <select
                    value={newColType}
                    onChange={(e) => setNewColType(e.target.value)}
                    className="mt-2 h-11 w-full rounded-[12px] border border-line-strong bg-white px-3.5 font-mono text-xs outline-none focus:border-violet cursor-pointer"
                  >
                    {PG_TYPES.map((t) => (
                      <option key={t} value={t}>
                        {t}
                      </option>
                    ))}
                  </select>
                </label>
              </div>
            )}

            {recipe === 'create_index' && (
              <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                <label className="block text-sm font-bold text-ink">
                  <span>Target column</span>
                  <select
                    value={activeColName}
                    onChange={(e) => setSelectedCol(e.target.value)}
                    className="mt-2 h-11 w-full rounded-[12px] border border-line-strong bg-white px-3.5 font-mono text-xs outline-none focus:border-violet cursor-pointer"
                  >
                    {columns.map((col) => (
                      <option key={col.name} value={col.name}>
                        {col.name} ({col.type})
                      </option>
                    ))}
                  </select>
                </label>
                <label className="block text-sm font-bold text-ink">
                  <span>Index name</span>
                  <input
                    type="text"
                    value={indexName}
                    onChange={(e) => setIndexName(e.target.value)}
                    placeholder={`idx_${table}_${activeColName}`}
                    className="mt-2 h-11 w-full rounded-[12px] border border-line-strong px-3.5 font-mono text-xs outline-none focus:border-violet"
                  />
                </label>
                <div className="sm:col-span-2">
                  <label className="flex h-11 max-w-xs items-center gap-3 rounded-[12px] border border-line bg-canvas/30 px-4 text-xs font-bold text-ink cursor-pointer hover:border-line-strong transition-colors">
                    <input
                      type="checkbox"
                      checked={indexUnique}
                      onChange={(e) => setIndexUnique(e.target.checked)}
                      className="size-4 rounded accent-violet"
                    />
                    <span>Unique index</span>
                  </label>
                </div>
              </div>
            )}

            {recipe === 'drop_column' && (
              <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                <label className="block text-sm font-bold text-danger sm:col-span-2">
                  <span>Target column</span>
                  <select
                    value={activeColName}
                    onChange={(e) => setSelectedCol(e.target.value)}
                    className="mt-2 h-11 w-full rounded-[12px] border border-danger/40 bg-white px-3.5 font-mono text-xs text-danger outline-none focus:border-danger cursor-pointer"
                  >
                    {columns.map((col) => (
                      <option key={col.name} value={col.name}>
                        {col.name} ({col.type})
                      </option>
                    ))}
                  </select>
                </label>
                <label className="block text-sm font-bold text-warning sm:col-span-2">
                  <span>Reason</span>
                  <input
                    type="text"
                    value={dropReason}
                    onChange={(e) => setDropReason(e.target.value)}
                    className="mt-2 h-11 w-full rounded-[12px] border border-warning/40 px-3.5 text-xs text-ink outline-none focus:border-warning"
                  />
                </label>
              </div>
            )}
          </div>

          {/* Blueprint SQL Preview */}
          <div className="border-t border-line bg-[#fbfafd]/60 p-6 sm:p-7 space-y-5">
            <div className="flex items-center justify-between">
              <span className="text-xs font-bold uppercase tracking-wider text-muted">
                Blueprint SQL
              </span>
              {blueprint.downSql ? (
                <Status tone="green">Reversible</Status>
              ) : (
                <Status tone="amber">Irreversible</Status>
              )}
            </div>

            <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
              <div>
                <span className="mb-2 block text-xs font-bold text-ink">
                  Up SQL
                </span>
                <pre className="m-0 min-h-24 overflow-auto rounded-xl border border-line-strong/80 bg-white p-3.5 font-mono text-xs leading-5 text-ink">
                  {blueprint.upSql}
                </pre>
              </div>

              <div>
                <span className="mb-2 block text-xs font-bold text-ink">
                  {blueprint.downSql ? 'Down SQL' : 'Reason'}
                </span>
                <pre className="m-0 min-h-24 overflow-auto rounded-xl border border-line-strong/80 bg-white p-3.5 font-mono text-xs leading-5 text-ink">
                  {blueprint.downSql || blueprint.irreversibleReason}
                </pre>
              </div>
            </div>

            <div className="flex flex-wrap items-center justify-end gap-3 pt-4 border-t border-line/80">
              <Button variant="outline" size="sm" onClick={testInQueryEditor}>
                <Icon name="code" className="size-3.5" />
                Explain Up SQL
              </Button>
              {canEdit ? (
                <Button variant="dark" size="sm" onClick={openInWorkbench}>
                  <Icon name="migration" className="size-3.5" />
                  Stage in Workbench
                  <Icon name="chevron" className="size-3.5" />
                </Button>
              ) : (
                <Link
                  to={`/databases/${id}/migrations`}
                  className="inline-flex min-h-8 items-center gap-1.5 rounded-lg border border-line px-3 text-xs font-bold text-ink no-underline hover:bg-canvas"
                >
                  Migrations
                </Link>
              )}
            </div>
          </div>
        </section>

        {/* Sidebar: Table Columns */}
        <aside className="h-fit rounded-[20px] border border-line bg-white shadow-xs overflow-hidden">
          <div className="flex items-center justify-between border-b border-line bg-[#fbfafd] px-5 py-4">
            <h2 className="m-0 font-mono text-sm font-bold text-ink">
              {schema}.{table}
            </h2>
            <Link
              to={`/databases/${id}/${schema}/${encodeURIComponent(table)}`}
              className="text-xs font-bold text-violet no-underline hover:underline flex items-center gap-1"
            >
              <span>View table</span>
              <Icon name="chevron" className="size-3" />
            </Link>
          </div>

          <div className="max-h-[520px] overflow-auto divide-y divide-line">
            {columns.map((col) => {
              const isSelected = activeColName === col.name
              return (
                <button
                  type="button"
                  key={col.name}
                  onClick={() => setSelectedCol(col.name)}
                  className={cn(
                    "flex w-full items-center justify-between gap-3 px-5 py-3 text-left transition-colors cursor-pointer text-xs",
                    isSelected
                      ? "bg-violet-soft/60 shadow-[inset_3px_0_var(--violet)]"
                      : "bg-white hover:bg-canvas/60"
                  )}
                  title={`Select ${col.name} as target`}
                >
                  <div className="min-w-0 flex-1">
                    <strong className={cn("block truncate font-mono text-xs", isSelected ? "text-violet font-bold" : "text-ink font-semibold")}>
                      {col.name}
                    </strong>
                    <span className="text-[10px] text-muted">
                      {col.nullable ? 'NULL' : 'NOT NULL'}
                      {col.default ? ` · ${col.default}` : ''}
                    </span>
                  </div>
                  <span className="shrink-0 rounded-md border border-line bg-canvas/80 px-2 py-0.5 font-mono text-[11px] text-violet font-semibold">
                    {col.type}
                  </span>
                </button>
              )
            })}
          </div>
        </aside>
      </div>
          </m.div>
        )}
      </AnimatePresence>
    </>
  )
}
