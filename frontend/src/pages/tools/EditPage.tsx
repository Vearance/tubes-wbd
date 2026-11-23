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

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-[minmax(0,1fr)_380px]">
        <section className="rounded-[18px] border border-line/80 bg-white p-5 shadow-xs space-y-5">
          <div className="flex flex-wrap gap-1.5">
            {RECIPES.map((item) => {
              const active = recipe === item.key
              return (
                <m.button
                  key={item.key}
                  type="button"
                  whileHover={{ y: -1 }}
                  whileTap={{ scale: 0.97 }}
                  transition={{ duration: 0.12 }}
                  onClick={() => setRecipe(item.key)}
                  className={cn(
                    'inline-flex items-center gap-1.5 rounded-xl border px-3.5 py-2 text-xs font-bold transition-[border-color,background-color,box-shadow] cursor-pointer shadow-2xs',
                    active
                      ? 'border-violet/40 bg-violet-soft text-violet shadow-xs'
                      : 'border-line/80 bg-white text-muted hover:border-violet/40 hover:text-ink hover:shadow-xs',
                  )}
                >
                  <Icon name={item.icon} className="size-3.5" />
                  {item.label}
                </m.button>
              )
            })}
          </div>

          <div className="border-t border-line pt-4 space-y-4">
            {recipe === 'add_column' && (
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <label className="block text-xs font-bold text-ink">
                  <span>Column name</span>
                  <input
                    type="text"
                    value={newColName}
                    onChange={(e) => setNewColName(e.target.value)}
                    className="mt-1.5 h-10 w-full rounded-[10px] border border-line-strong px-3 font-mono text-xs outline-none focus:border-violet"
                  />
                </label>
                <label className="block text-xs font-bold text-ink">
                  <span>PostgreSQL type</span>
                  <select
                    value={newColType}
                    onChange={(e) => setNewColType(e.target.value)}
                    className="mt-1.5 h-10 w-full rounded-[10px] border border-line-strong bg-white px-3 font-mono text-xs outline-none focus:border-violet"
                  >
                    {PG_TYPES.map((t) => (
                      <option key={t} value={t}>
                        {t}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="block text-xs font-bold text-ink">
                  <span>Default expression</span>
                  <input
                    type="text"
                    value={defaultExpr}
                    onChange={(e) => setDefaultExpr(e.target.value)}
                    placeholder="e.g. 'pending' or NOW()"
                    className="mt-1.5 h-10 w-full rounded-[10px] border border-line-strong px-3 font-mono text-xs outline-none focus:border-violet"
                  />
                </label>
                <label className="flex items-center gap-2 pt-6 text-xs font-semibold text-ink cursor-pointer">
                  <input
                    type="checkbox"
                    checked={nullable}
                    onChange={(e) => setNullable(e.target.checked)}
                    className="size-4 accent-violet"
                  />
                  Nullable
                </label>
              </div>
            )}

            {recipe !== 'add_column' && (
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <label className="block text-xs font-bold text-ink">
                  <span>Target column</span>
                  <select
                    value={activeColName}
                    onChange={(e) => setSelectedCol(e.target.value)}
                    className="mt-1.5 h-10 w-full rounded-[10px] border border-line-strong bg-white px-3 font-mono text-xs outline-none focus:border-violet"
                  >
                    {columns.map((col) => (
                      <option key={col.name} value={col.name}>
                        {col.name} ({col.type})
                      </option>
                    ))}
                  </select>
                </label>

                {recipe === 'rename_column' && (
                  <label className="block text-xs font-bold text-ink">
                    <span>New column name</span>
                    <input
                      type="text"
                      value={renameTarget}
                      onChange={(e) => setRenameTarget(e.target.value)}
                      placeholder={`${activeColName}_v2`}
                      className="mt-1.5 h-10 w-full rounded-[10px] border border-line-strong px-3 font-mono text-xs outline-none focus:border-violet"
                    />
                  </label>
                )}

                {recipe === 'alter_column' && (
                  <label className="block text-xs font-bold text-ink">
                    <span>New PostgreSQL type</span>
                    <select
                      value={newColType}
                      onChange={(e) => setNewColType(e.target.value)}
                      className="mt-1.5 h-10 w-full rounded-[10px] border border-line-strong bg-white px-3 font-mono text-xs outline-none focus:border-violet"
                    >
                      {PG_TYPES.map((t) => (
                        <option key={t} value={t}>
                          {t}
                        </option>
                      ))}
                    </select>
                  </label>
                )}

                {recipe === 'create_index' && (
                  <>
                    <label className="block text-xs font-bold text-ink">
                      <span>Index name</span>
                      <input
                        type="text"
                        value={indexName}
                        onChange={(e) => setIndexName(e.target.value)}
                        placeholder={`idx_${table}_${activeColName}`}
                        className="mt-1.5 h-10 w-full rounded-[10px] border border-line-strong px-3 font-mono text-xs outline-none focus:border-violet"
                      />
                    </label>
                    <label className="flex items-center gap-2 text-xs font-semibold text-ink cursor-pointer">
                      <input
                        type="checkbox"
                        checked={indexUnique}
                        onChange={(e) => setIndexUnique(e.target.checked)}
                        className="size-4 accent-violet"
                      />
                      Unique index
                    </label>
                  </>
                )}

                {recipe === 'drop_column' && (
                  <label className="block text-xs font-bold text-warning sm:col-span-2">
                    <span>Irreversible reason</span>
                    <input
                      type="text"
                      value={dropReason}
                      onChange={(e) => setDropReason(e.target.value)}
                      className="mt-1.5 h-10 w-full rounded-[10px] border border-warning/40 px-3 text-xs text-ink outline-none focus:border-warning"
                    />
                  </label>
                )}
              </div>
            )}
          </div>

          <div className="border-t border-line pt-4 space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-bold uppercase tracking-wider text-muted">
                Blueprint SQL
              </span>
              {blueprint.downSql ? (
                <Status tone="green">Reversible</Status>
              ) : (
                <Status tone="amber">Irreversible</Status>
              )}
            </div>

            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              <div>
                <span className="mb-1 block text-[11px] font-bold text-ink">
                  Up SQL
                </span>
                <pre className="m-0 min-h-24 overflow-auto rounded-xl border border-[#3a3740] bg-[#242227] p-3 font-mono text-xs leading-6 text-[#f2edf4]">
                  {blueprint.upSql}
                </pre>
              </div>
              <div>
                <span className="mb-1 block text-[11px] font-bold text-success">
                  {blueprint.downSql ? 'Down SQL' : 'Reason'}
                </span>
                <pre className="m-0 min-h-24 overflow-auto rounded-xl border border-line bg-[#fbfafd] p-3 font-mono text-xs leading-6 text-ink">
                  {blueprint.downSql || blueprint.irreversibleReason}
                </pre>
              </div>
            </div>

            <div className="flex flex-wrap justify-end gap-2 pt-2">
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

        <aside className="h-fit rounded-[18px] border border-line/80 bg-white p-5 shadow-xs space-y-3">
          <div className="flex items-center justify-between border-b border-line pb-3">
            <h2 className="m-0 font-mono text-sm font-bold text-ink">
              {schema}.{table}
            </h2>
            <Link
              to={`/databases/${id}/${schema}/${encodeURIComponent(table)}`}
              className="text-xs font-semibold text-violet no-underline hover:underline"
            >
              View table →
            </Link>
          </div>

          <div className="max-h-96 overflow-auto divide-y divide-line/60">
            {columns.map((col) => (
              <div
                key={col.name}
                className="flex items-center justify-between gap-2 py-2 text-xs"
              >
                <div className="min-w-0">
                  <strong className="block truncate font-mono font-semibold text-ink">
                    {col.name}
                  </strong>
                  <span className="text-[10px] text-muted">
                    {col.nullable ? 'NULL' : 'NOT NULL'}
                    {col.default ? ` · ${col.default}` : ''}
                  </span>
                </div>
                <span className="shrink-0 rounded bg-canvas px-2 py-0.5 font-mono text-[11px] text-violet">
                  {col.type}
                </span>
              </div>
            ))}
          </div>
        </aside>
      </div>
          </m.div>
        )}
      </AnimatePresence>
    </>
  )
}
