export type PermissionLevel = "view_only" | "view_edit";
export type AuthUser = { id: number; username: string; is_admin: boolean };
export type LoginResponse = {
	csrf_token: string;
	expires_at: string;
	user: AuthUser;
};
export type AccessibleDatabase = {
	id: number;
	name: string;
	permission: PermissionLevel;
};
export type AdminUser = {
	id: number;
	username: string;
	is_admin: boolean;
	disabled: boolean;
};
export type Group = {
	id: number;
	name: string;
	leader_user_id?: number | null;
	leader_username?: string | null;
};
export type ManagedDatabase = {
	id: number;
	name: string;
	host: string;
	port: number;
	username: string;
	db_name: string;
	ssl_mode: string;
	created_at: string;
	updated_at: string;
};
export type ManagedDatabaseInput = {
	name: string;
	host: string;
	port: number;
	username: string;
	password?: string;
	db_name: string;
	ssl_mode?: string;
};
export type DatabasePermission = {
	group_id: number;
	database_id: number;
	permission: PermissionLevel;
};
export type TableInfo = { schema: string; name: string; kind: string };
export type Column = {
	name: string;
	type: string;
	nullable: boolean;
	default?: string | null;
};
export type Constraint = { name: string; type: string; definition: string };
export type IndexInfo = {
	name: string;
	definition: string;
	unique: boolean;
	primary: boolean;
};
export type TableStructure = {
	table: TableInfo;
	columns: Column[];
	constraints: Constraint[];
	indexes: IndexInfo[];
};
export type TableRows = {
	columns: string[];
	rows: Array<Array<string | null>>;
	page: number;
	page_size: number;
	has_next: boolean;
};
export type MigrationDraftStatus =
	| "draft"
	| "pending_review"
	| "approved"
	| "queued"
	| "running"
	| "applied"
	| "failed";
export type MigrationBatchDefinition = {
	schema: string;
	table: string;
	key_column: string;
	scope_sql: string;
	batch_size: number;
};
export type MigrationDraft = {
	id: number;
	database_id: number;
	name: string;
	sql: string;
	down_sql?: string;
	batch?: MigrationBatchDefinition;
	irreversible?: boolean;
	irreversible_reason?: string;
	status: MigrationDraftStatus;
	applied_version?: number | null;
	created_by: number;
	updated_by: number;
	created_at: string;
	updated_at: string;
};
export type MigrationDraftInput = {
	name: string;
	sql: string;
	down_sql?: string;
	batch?: MigrationBatchDefinition;
	irreversible?: boolean;
	irreversible_reason?: string;
};
export type MigrationExecutionStatus =
	| "pending_review"
	| "rejected"
	| "approved"
	| "queued"
	| "running"
	| "success"
	| "failed"
	| "partial"
	| "compensation_pending"
	| "compensating"
	| "compensated";
export type MigrationApprovalDecision = "pending" | "approved" | "rejected";
export type MigrationApproval = {
	group_id: number;
	group_name: string;
	leader_user_id?: number | null;
	leader_username?: string | null;
	decision: MigrationApprovalDecision;
	decided_by?: number | null;
	decided_by_username?: string | null;
	decided_at?: string | null;
	note?: string;
};
export type MigrationApprovalSummary = {
	execution_id: number;
	execution_status: MigrationExecutionStatus;
	can_decide: boolean;
	approvals: MigrationApproval[];
};
export type MigrationExecutionType = "up" | "rollback" | "forward";
export type MigrationExecution = {
	id: number;
	database_id: number;
	draft_id: number;
	type: MigrationExecutionType;
	status: MigrationExecutionStatus;
	compensation: boolean;
	name: string;
	sql: string;
	down_sql?: string;
	batch?: MigrationBatchDefinition;
	batch_count?: number;
	rows_processed?: number;
	last_cursor?: number | null;
	batch_done?: boolean;
	irreversible?: boolean;
	irreversible_reason?: string;
	executed_by: number;
	target_execution_id?: number | null;
	version?: number | null;
	attempt: number;
	queued_at?: string | null;
	available_at?: string | null;
	started_at?: string | null;
	finished_at?: string | null;
	error_code?: string;
	error_message?: string;
	created_at: string;
	updated_at: string;
};
export type MigrationHistoryEntry = {
	version: number;
	attempt_number: number;
	status: "success" | "failed";
	error_code?: string;
	error_message?: string;
	execution_id?: number | null;
	execution_type?: MigrationExecutionType;
	target_execution_id?: number | null;
	active?: boolean;
	name: string;
	executed_by: number;
	executed_at: string;
	duration_ms: number;
};
export type MigrationHistoryDetail = MigrationHistoryEntry & {
	sql: string;
	down_sql?: string;
	irreversible_reason?: string;
};
export type MigrationHistoryPage = {
	entries: MigrationHistoryEntry[];
	page: number;
	page_size: number;
	has_next: boolean;
	forward?: MigrationForwardCandidate;
};
export type MigrationForwardCandidate = {
	rollback_execution_id: number;
	target_execution_id: number;
	name: string;
};
export type ExplainResponse = {
	statements: Array<{ sql: string; plan: Record<string, unknown>[] }>;
};

const API_BASE =
	(import.meta.env.VITE_API_URL as string | undefined)?.replace(/\/$/, "") ??
	"";
const CSRF_KEY = "bscheme_csrf";
const USER_KEY = "bscheme_user";

export class ApiError extends Error {
	status: number;
	constructor(status: number, message: string) {
		super(message);
		this.status = status;
	}
}
export const session = {
	user(): AuthUser | null {
		try {
			return JSON.parse(
				localStorage.getItem(USER_KEY) ?? "null",
			) as AuthUser | null;
		} catch {
			return null;
		}
	},
	csrf: () => localStorage.getItem(CSRF_KEY),
	save(data: LoginResponse) {
		localStorage.setItem(CSRF_KEY, data.csrf_token);
		localStorage.setItem(USER_KEY, JSON.stringify(data.user));
	},
	clear() {
		localStorage.removeItem(CSRF_KEY);
		localStorage.removeItem(USER_KEY);
	},
};

type RequestOptions = Omit<RequestInit, "body"> & { body?: unknown };
async function request<T>(
	path: string,
	options: RequestOptions = {},
): Promise<T> {
	const headers = new Headers(options.headers);
	const method = (options.method ?? "GET").toUpperCase();
	if (options.body !== undefined)
		headers.set("Content-Type", "application/json");
	const csrf = session.csrf();
	if (csrf && !["GET", "HEAD", "OPTIONS"].includes(method))
		headers.set("X-CSRF-Token", csrf);
	const response = await fetch(`${API_BASE}${path}`, {
		...options,
		method,
		headers,
		credentials: "include",
		body:
			options.body === undefined
				? undefined
				: JSON.stringify(options.body),
	});
	if (response.status === 401) session.clear();
	if (!response.ok) {
		let message = `Request failed (${response.status})`;
		try {
			message =
				((await response.json()) as { message?: string }).message ??
				message;
		} catch {
			/* non-JSON response */
		}
		throw new ApiError(response.status, message);
	}
	if (
		response.status === 204 ||
		response.headers.get("content-length") === "0"
	)
		return undefined as T;
	return response.json() as Promise<T>;
}
const qs = (values: Record<string, string | number | undefined>) => {
	const p = new URLSearchParams();
	Object.entries(values).forEach(([k, v]) => {
		if (v !== undefined) p.set(k, String(v));
	});
	return `?${p}`;
};

const requestHistoryPage = (id: number, page: number, hideRollback: boolean) =>
	request<MigrationHistoryPage>(
		`/databases/${id}/migrations/history${qs({
			page,
			page_size: 100,
			hide_rollback: String(hideRollback),
		})}`,
	);

export const api = {
	login: (username: string, password: string) =>
		request<LoginResponse>("/auth/login", {
			method: "POST",
			body: { username, password },
		}),
	logout: () => request<void>("/auth/logout", { method: "POST" }),
	adminPing: () => request<unknown>("/admin/ping"),
	databases: () => request<AccessibleDatabase[]>("/databases"),
	schemas: (id: number) => request<string[]>(`/databases/${id}/schemas`),
	tables: (id: number, schema: string) =>
		request<TableInfo[]>(`/databases/${id}/tables${qs({ schema })}`),
	structure: (id: number, schema: string, table: string) =>
		request<TableStructure>(
			`/databases/${id}/table-structure${qs({ schema, table })}`,
		),
	rows: (
		id: number,
		schema: string,
		table: string,
		page = 1,
		pageSize = 50,
	) =>
		request<TableRows>(
			`/databases/${id}/table-rows${qs({ schema, table, page, page_size: pageSize })}`,
		),
	explain: (id: number, sql: string) =>
		request<ExplainResponse>(`/databases/${id}/migrations/explain`, {
			method: "POST",
			body: { sql },
		}),
	history: (id: number, page = 1, hideRollback = false) =>
		requestHistoryPage(id, page, hideRollback),
	historyAll: async (id: number, hideRollback = false) => {
		const entries: MigrationHistoryEntry[] = [];
		let forward: MigrationForwardCandidate | undefined;
		let page = 1;
		let hasNext = true;
		while (hasNext) {
			const result = await requestHistoryPage(id, page, hideRollback);
			entries.push(...result.entries);
			if (page === 1) forward = result.forward;
			hasNext = result.has_next;
			page += 1;
		}
		return { entries, forward };
	},
	historyEntry: (id: number, version: number) =>
		request<MigrationHistoryDetail>(
			`/databases/${id}/migrations/history/${version}`,
		),
	drafts: (id: number) =>
		request<MigrationDraft[]>(`/databases/${id}/migrations/drafts`),
	draft: (draftId: number) =>
		request<MigrationDraft>(`/migrations/drafts/${draftId}`),
	createDraft: (
		id: number,
		input: MigrationDraftInput | string,
		maybeSql?: string,
	) => {
		const body =
			typeof input === "string"
				? { name: input, sql: maybeSql ?? "" }
				: input;
		return request<MigrationDraft>(`/databases/${id}/migrations/drafts`, {
			method: "POST",
			body,
		});
	},
	updateDraft: (id: number, input: MigrationDraftInput) =>
		request<MigrationDraft>(`/migrations/drafts/${id}`, {
			method: "PATCH",
			body: input,
		}),
	executeDraft: (id: number) =>
		request<MigrationExecution>(`/migrations/drafts/${id}/execute`, {
			method: "POST",
		}),
	executions: (id: number) =>
		request<MigrationExecution[]>(`/databases/${id}/migrations/executions`),
	execution: (id: number) =>
		request<MigrationExecution>(`/migrations/executions/${id}`),
	executionApprovals: (executionId: number) =>
		request<MigrationApprovalSummary>(
			`/migrations/executions/${executionId}/approvals`,
		),
	approveExecution: (executionId: number, note?: string) =>
		request<MigrationApprovalSummary>(
			`/migrations/executions/${executionId}/approve`,
			{ method: "POST", body: note?.trim() ? { note: note.trim() } : {} },
		),
	rejectExecution: (executionId: number, note: string) =>
		request<MigrationApprovalSummary>(
			`/migrations/executions/${executionId}/reject`,
			{ method: "POST", body: { note: note.trim() } },
		),
	enqueueExecution: (executionId: number) =>
		request<MigrationExecution>(
			`/migrations/executions/${executionId}/enqueue`,
			{ method: "POST" },
		),
	compensateExecution: (executionId: number) =>
		request<MigrationExecution>(
			`/migrations/executions/${executionId}/compensate`,
			{ method: "POST" },
		),
	pendingApprovals: async () => {
		try {
			return await request<MigrationExecution[]>(
				"/migrations/approvals/pending",
			);
		} catch {
			return [];
		}
	},
	rollback: (id: number) =>
		request<MigrationExecution>(`/databases/${id}/migrations/rollback`, {
			method: "POST",
		}),
	forward: (id: number) =>
		request<MigrationExecution>(`/databases/${id}/migrations/forward`, {
			method: "POST",
		}),
	adminUsers: () => request<AdminUser[]>("/admin/users"),
	createUser: (username: string, password: string) =>
		request<AdminUser>("/admin/users", {
			method: "POST",
			body: { username, password },
		}),
	updateUser: (id: number, disabled: boolean) =>
		request<AdminUser>(`/admin/users/${id}`, {
			method: "PATCH",
			body: { disabled },
		}),
	adminGroups: () => request<Group[]>("/admin/groups"),
	createGroup: (name: string) =>
		request<Group>("/admin/groups", { method: "POST", body: { name } }),
	groupMembers: (id: number) =>
		request<AdminUser[]>(`/admin/groups/${id}/members`),
	addGroupMember: (groupId: number, userId: number) =>
		request<void>(`/admin/groups/${groupId}/members`, {
			method: "POST",
			body: { user_id: userId },
		}),
	removeGroupMember: (groupId: number, userId: number) =>
		request<void>(`/admin/groups/${groupId}/members/${userId}`, {
			method: "DELETE",
		}),
	setGroupLeader: (groupId: number, userId: number) =>
		request<Group>(`/admin/groups/${groupId}/leader`, {
			method: "PUT",
			body: { user_id: userId },
		}),
	clearGroupLeader: (groupId: number) =>
		request<void>(`/admin/groups/${groupId}/leader`, {
			method: "DELETE",
		}),
	adminDatabases: () => request<ManagedDatabase[]>("/admin/databases"),
	createDatabase: (body: ManagedDatabaseInput) =>
		request<ManagedDatabase>("/admin/databases", { method: "POST", body }),
	updateDatabase: (id: number, body: ManagedDatabaseInput) =>
		request<ManagedDatabase>(`/admin/databases/${id}`, {
			method: "PATCH",
			body,
		}),
	deleteDatabase: (id: number) =>
		request<void>(`/admin/databases/${id}`, { method: "DELETE" }),
	testDatabase: (body: ManagedDatabaseInput) =>
		request<void>("/admin/databases/test", { method: "POST", body }),
	testStoredDatabase: (id: number) =>
		request<void>(`/admin/databases/${id}/test`, { method: "POST" }),
	groupDatabases: (id: number) =>
		request<DatabasePermission[]>(`/admin/groups/${id}/databases`),
	setGroupDatabase: (
		groupId: number,
		databaseId: number,
		permission: PermissionLevel,
	) =>
		request<DatabasePermission>(
			`/admin/groups/${groupId}/databases/${databaseId}`,
			{ method: "PUT", body: { permission } },
		),
	removeGroupDatabase: (groupId: number, databaseId: number) =>
		request<void>(`/admin/groups/${groupId}/databases/${databaseId}`, {
			method: "DELETE",
		}),
};
