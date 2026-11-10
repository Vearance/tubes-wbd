export type PermissionLevel = 'view_only' | 'view_edit'
export type AuthUser = { id: number; username: string; is_admin: boolean }
export type LoginResponse = { csrf_token: string; expires_at: string; user: AuthUser }
export type AccessibleDatabase = { id: number; name: string; permission: PermissionLevel }
export type AdminUser = { id: number; username: string; is_admin: boolean; disabled: boolean }
export type Group = { id: number; name: string }
export type ManagedDatabase = { id: number; name: string; host: string; port: number; username: string; db_name: string; ssl_mode: string; created_at: string; updated_at: string }
export type ManagedDatabaseInput = { name: string; host: string; port: number; username: string; password?: string; db_name: string; ssl_mode?: string }
export type DatabasePermission = { group_id: number; database_id: number; permission: PermissionLevel }
export type TableInfo = { schema: string; name: string; kind: string }
export type Column = { name: string; type: string; nullable: boolean; default?: string | null }
export type Constraint = { name: string; type: string; definition: string }
export type IndexInfo = { name: string; definition: string; unique: boolean; primary: boolean }
export type TableStructure = { table: TableInfo; columns: Column[]; constraints: Constraint[]; indexes: IndexInfo[] }
export type TableRows = { columns: string[]; rows: Array<Array<string | null>>; page: number; page_size: number; has_next: boolean }
export type MigrationDraft = { id: number; database_id: number; name: string; sql: string; status: 'draft' | 'running' | 'applied' | 'failed'; applied_version?: number | null; created_by: number; updated_by: number; created_at: string; updated_at: string }
export type MigrationHistoryEntry = { version: number; name: string; executed_by: number; executed_at: string; duration_ms: number }
export type MigrationHistoryPage = { entries: MigrationHistoryEntry[]; page: number; page_size: number; has_next: boolean }
export type MigrationRun = { id: number; draft_id: number; database_id: number; status: 'success' | 'failed'; version?: number | null; sql: string; executed_by: number; started_at: string; finished_at: string; error_code?: string; error_message?: string }
export type ExplainResponse = { statements: Array<{ sql: string; plan: Record<string, unknown>[] }> }

const API_BASE = (import.meta.env.VITE_API_URL as string | undefined)?.replace(/\/$/, '') ?? ''
const CSRF_KEY = 'bscheme_csrf'
const USER_KEY = 'bscheme_user'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) { super(message); this.status = status }
}
export const session = {
  user(): AuthUser | null { try { return JSON.parse(localStorage.getItem(USER_KEY) ?? 'null') as AuthUser | null } catch { return null } },
  csrf: () => localStorage.getItem(CSRF_KEY),
  save(data: LoginResponse) { localStorage.setItem(CSRF_KEY, data.csrf_token); localStorage.setItem(USER_KEY, JSON.stringify(data.user)) },
  clear() { localStorage.removeItem(CSRF_KEY); localStorage.removeItem(USER_KEY) },
}

type RequestOptions = Omit<RequestInit, 'body'> & { body?: unknown }
async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers = new Headers(options.headers)
  const method = (options.method ?? 'GET').toUpperCase()
  if (options.body !== undefined) headers.set('Content-Type', 'application/json')
  const csrf = session.csrf()
  if (csrf && !['GET', 'HEAD', 'OPTIONS'].includes(method)) headers.set('X-CSRF-Token', csrf)
  const response = await fetch(`${API_BASE}${path}`, { ...options, method, headers, credentials: 'include', body: options.body === undefined ? undefined : JSON.stringify(options.body) })
  if (response.status === 401) session.clear()
  if (!response.ok) {
    let message = `Request failed (${response.status})`
    try { message = (await response.json() as { message?: string }).message ?? message } catch { /* non-JSON response */ }
    throw new ApiError(response.status, message)
  }
  if (response.status === 204 || response.headers.get('content-length') === '0') return undefined as T
  return response.json() as Promise<T>
}
const qs = (values: Record<string, string | number | undefined>) => { const p = new URLSearchParams(); Object.entries(values).forEach(([k, v]) => { if (v !== undefined) p.set(k, String(v)) }); return `?${p}` }

export const api = {
  login: (username: string, password: string) => request<LoginResponse>('/auth/login', { method: 'POST', body: { username, password } }),
  logout: () => request<void>('/auth/logout', { method: 'POST' }),
  adminPing: () => request<unknown>('/admin/ping'),
  databases: () => request<AccessibleDatabase[]>('/databases'),
  schemas: (id: number) => request<string[]>(`/databases/${id}/schemas`),
  tables: (id: number, schema: string) => request<TableInfo[]>(`/databases/${id}/tables${qs({ schema })}`),
  structure: (id: number, schema: string, table: string) => request<TableStructure>(`/databases/${id}/table-structure${qs({ schema, table })}`),
  rows: (id: number, schema: string, table: string, page = 1, pageSize = 50) => request<TableRows>(`/databases/${id}/table-rows${qs({ schema, table, page, page_size: pageSize })}`),
  explain: (id: number, sql: string) => request<ExplainResponse>(`/databases/${id}/migrations/explain`, { method: 'POST', body: { sql } }),
  history: (id: number, page = 1) => request<MigrationHistoryPage>(`/databases/${id}/migrations/history${qs({ page, page_size: 50 })}`),
  drafts: (id: number) => request<MigrationDraft[]>(`/databases/${id}/migrations/drafts`),
  createDraft: (id: number, name: string, sql: string) => request<MigrationDraft>(`/databases/${id}/migrations/drafts`, { method: 'POST', body: { name, sql } }),
  updateDraft: (id: number, name: string, sql: string) => request<MigrationDraft>(`/migrations/drafts/${id}`, { method: 'PATCH', body: { name, sql } }),
  executeDraft: (id: number) => request<MigrationRun>(`/migrations/drafts/${id}/execute`, { method: 'POST' }),
  adminUsers: () => request<AdminUser[]>('/admin/users'),
  createUser: (username: string, password: string) => request<AdminUser>('/admin/users', { method: 'POST', body: { username, password } }),
  updateUser: (id: number, disabled: boolean) => request<AdminUser>(`/admin/users/${id}`, { method: 'PATCH', body: { disabled } }),
  adminGroups: () => request<Group[]>('/admin/groups'),
  createGroup: (name: string) => request<Group>('/admin/groups', { method: 'POST', body: { name } }),
  groupMembers: (id: number) => request<AdminUser[]>(`/admin/groups/${id}/members`),
  addGroupMember: (groupId: number, userId: number) => request<void>(`/admin/groups/${groupId}/members`, { method: 'POST', body: { user_id: userId } }),
  removeGroupMember: (groupId: number, userId: number) => request<void>(`/admin/groups/${groupId}/members/${userId}`, { method: 'DELETE' }),
  adminDatabases: () => request<ManagedDatabase[]>('/admin/databases'),
  createDatabase: (body: ManagedDatabaseInput) => request<ManagedDatabase>('/admin/databases', { method: 'POST', body }),
  updateDatabase: (id: number, body: ManagedDatabaseInput) => request<ManagedDatabase>(`/admin/databases/${id}`, { method: 'PATCH', body }),
  deleteDatabase: (id: number) => request<void>(`/admin/databases/${id}`, { method: 'DELETE' }),
  testDatabase: (body: ManagedDatabaseInput) => request<void>('/admin/databases/test', { method: 'POST', body }),
  testStoredDatabase: (id: number) => request<void>(`/admin/databases/${id}/test`, { method: 'POST' }),
  groupDatabases: (id: number) => request<DatabasePermission[]>(`/admin/groups/${id}/databases`),
  setGroupDatabase: (groupId: number, databaseId: number, permission: PermissionLevel) => request<DatabasePermission>(`/admin/groups/${groupId}/databases/${databaseId}`, { method: 'PUT', body: { permission } }),
  removeGroupDatabase: (groupId: number, databaseId: number) => request<void>(`/admin/groups/${groupId}/databases/${databaseId}`, { method: 'DELETE' }),
}
