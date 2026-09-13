export interface BBSConfig {
	name: string;
	sysop: string;
	new_user_sl: number;
	/** This system's own FTN address (zone:net/node.point) on whichever network it belongs to, if any -- stamped on outgoing netmail. Optional. */
	ftn_address: string;
	telnet_enabled: boolean;
	telnet_addr: string;
	ssh_enabled: boolean;
	ssh_addr: string;
}

export interface LoginResponse {
	token: string;
	username: string;
	security_level: number;
	expires_at: string;
}

export interface Node {
	node: number;
	remote_ip: string;
	term_type: string;
	username: string;
	connected_at: string;
}

export interface Dashboard {
	bbs_name: string;
	version: string;
	user_count: number;
	message_area_count: number;
	file_area_count: number;
	nodes: Node[];
}

export interface BBSUser {
	id: number;
	username: string;
	security_level: number;
	created_at: string;
	last_login_at: string | null;
	total_calls: number;
}

export interface MessageArea {
	id: number;
	tag: string;
	name: string;
	description: string;
	/** FTN network this echo area belongs to (e.g. "fsxNet", "FidoNet"), or "" for a local-only area. */
	network: string;
	min_sl_read: number;
	min_sl_write: number;
	sort_order: number;
}

export interface FileArea {
	id: number;
	tag: string;
	name: string;
	description: string;
	/** FTN network this file area belongs to (e.g. "fsxNet", "FidoNet"), or "" for a local-only area. */
	network: string;
	min_sl_download: number;
	min_sl_upload: number;
	sort_order: number;
}

export interface BBSFile {
	id: number;
	area_id: number;
	filename: string;
	description: string;
	size_bytes: number;
	size_human: string;
	uploaded_by: string;
	uploaded_at: string;
	download_count: number;
}

export interface MenuItem {
	key: string;
	label: string;
	action: string;
	min_sl: number;
}

export interface MenuDef {
	name: string;
	title: string;
	items: MenuItem[];
}

export interface ScreenSummary {
	name: string;
}

export interface ScreenPreview {
	name: string;
	html: string;
}

export interface GridCell {
	char: number;
	fg: number;
	bg: number;
}

export interface Grid {
	width: number;
	height: number;
	cells: GridCell[];
}

export interface LogEntry {
	id: number;
	logged_at: string;
	source: string;
	level: 'info' | 'warn' | 'error';
	message: string;
}

export class ApiError extends Error {
	status: number;
	constructor(status: number, message: string) {
		super(message);
		this.status = status;
	}
}

async function handleResponse<T>(res: Response): Promise<T> {
	if (!res.ok) {
		let message = res.statusText;
		try {
			const body = await res.json();
			if (body?.error) message = body.error;
		} catch {
			// response body wasn't JSON; fall back to statusText
		}
		throw new ApiError(res.status, message);
	}
	if (res.status === 204) {
		return undefined as T;
	}
	return (await res.json()) as T;
}

async function request<T>(path: string, options: RequestInit = {}, token?: string): Promise<T> {
	const headers = new Headers(options.headers);
	if (options.body !== undefined) headers.set('Content-Type', 'application/json');
	if (token) headers.set('Authorization', `Bearer ${token}`);

	const res = await fetch(path, { ...options, headers });
	return handleResponse<T>(res);
}

// requestForm is used for multipart/form-data uploads: the browser
// must set the Content-Type itself (it includes a generated boundary
// string), so it's deliberately left unset here.
async function requestForm<T>(path: string, formData: FormData, token: string): Promise<T> {
	const res = await fetch(path, {
		method: 'POST',
		headers: { Authorization: `Bearer ${token}` },
		body: formData
	});
	return handleResponse<T>(res);
}

export function login(username: string, password: string): Promise<LoginResponse> {
	return request<LoginResponse>('/api/auth/login', {
		method: 'POST',
		body: JSON.stringify({ username, password })
	});
}

export function getConfig(token: string): Promise<BBSConfig> {
	return request<BBSConfig>('/api/config', { method: 'GET' }, token);
}

export function putConfig(
	token: string,
	config: BBSConfig
): Promise<{ config: BBSConfig; note: string }> {
	return request('/api/config', { method: 'PUT', body: JSON.stringify(config) }, token);
}

export function getDashboard(token: string): Promise<Dashboard> {
	return request<Dashboard>('/api/dashboard', { method: 'GET' }, token);
}

export function listUsers(token: string): Promise<BBSUser[]> {
	return request<BBSUser[]>('/api/users', { method: 'GET' }, token);
}

export function setUserSecurityLevel(
	token: string,
	id: number,
	securityLevel: number
): Promise<BBSUser> {
	return request<BBSUser>(
		`/api/users/${id}`,
		{ method: 'PUT', body: JSON.stringify({ security_level: securityLevel }) },
		token
	);
}

export type MessageAreaInput = Omit<MessageArea, 'id'>;

export function listMessageAreas(token: string): Promise<MessageArea[]> {
	return request<MessageArea[]>('/api/message-areas', { method: 'GET' }, token);
}

export function createMessageArea(
	token: string,
	area: MessageAreaInput
): Promise<MessageArea> {
	return request<MessageArea>(
		'/api/message-areas',
		{ method: 'POST', body: JSON.stringify(area) },
		token
	);
}

export function updateMessageArea(
	token: string,
	id: number,
	area: MessageAreaInput
): Promise<MessageArea> {
	return request<MessageArea>(
		`/api/message-areas/${id}`,
		{ method: 'PUT', body: JSON.stringify(area) },
		token
	);
}

export function deleteMessageArea(token: string, id: number): Promise<void> {
	return request<void>(`/api/message-areas/${id}`, { method: 'DELETE' }, token);
}

export type FileAreaInput = Omit<FileArea, 'id'>;

export function listFileAreas(token: string): Promise<FileArea[]> {
	return request<FileArea[]>('/api/file-areas', { method: 'GET' }, token);
}

export function createFileArea(token: string, area: FileAreaInput): Promise<FileArea> {
	return request<FileArea>('/api/file-areas', { method: 'POST', body: JSON.stringify(area) }, token);
}

export function updateFileArea(
	token: string,
	id: number,
	area: FileAreaInput
): Promise<FileArea> {
	return request<FileArea>(
		`/api/file-areas/${id}`,
		{ method: 'PUT', body: JSON.stringify(area) },
		token
	);
}

export function deleteFileArea(token: string, id: number): Promise<void> {
	return request<void>(`/api/file-areas/${id}`, { method: 'DELETE' }, token);
}

export function listAreaFiles(token: string, areaId: number): Promise<BBSFile[]> {
	return request<BBSFile[]>(`/api/file-areas/${areaId}/files`, { method: 'GET' }, token);
}

export function uploadAreaFile(
	token: string,
	areaId: number,
	file: File,
	description: string
): Promise<BBSFile> {
	const form = new FormData();
	form.set('file', file);
	form.set('description', description);
	return requestForm<BBSFile>(`/api/file-areas/${areaId}/files`, form, token);
}

export function deleteFile(token: string, id: number): Promise<void> {
	return request<void>(`/api/files/${id}`, { method: 'DELETE' }, token);
}

export function listScreens(token: string): Promise<ScreenSummary[]> {
	return request<ScreenSummary[]>('/api/screens', { method: 'GET' }, token);
}

export function previewScreen(token: string, name: string): Promise<ScreenPreview> {
	return request<ScreenPreview>(`/api/screens/${encodeURIComponent(name)}`, { method: 'GET' }, token);
}

export function createScreen(
	token: string,
	name: string,
	width?: number,
	height?: number
): Promise<ScreenSummary> {
	return request<ScreenSummary>(
		'/api/screens',
		{ method: 'POST', body: JSON.stringify({ name, width, height }) },
		token
	);
}

export function deleteScreen(token: string, name: string): Promise<void> {
	return request<void>(`/api/screens/${encodeURIComponent(name)}`, { method: 'DELETE' }, token);
}

export function importScreen(token: string, file: File, name?: string): Promise<ScreenSummary> {
	const form = new FormData();
	form.set('file', file);
	if (name) form.set('name', name);
	return requestForm<ScreenSummary>('/api/screens/import', form, token);
}

export function getScreenGrid(token: string, name: string): Promise<Grid> {
	return request<Grid>(`/api/screens/${encodeURIComponent(name)}/grid`, { method: 'GET' }, token);
}

export function saveScreenGrid(token: string, name: string, grid: Grid): Promise<void> {
	return request<void>(
		`/api/screens/${encodeURIComponent(name)}/grid`,
		{ method: 'PUT', body: JSON.stringify(grid) },
		token
	);
}

export function listMenus(token: string): Promise<MenuDef[]> {
	return request<MenuDef[]>('/api/menus', { method: 'GET' }, token);
}

export function setMenuItemSL(
	token: string,
	menuName: string,
	itemKey: string,
	minSL: number
): Promise<{ menu: MenuDef; note: string }> {
	return request(
		`/api/menus/${encodeURIComponent(menuName)}/items/${encodeURIComponent(itemKey)}`,
		{ method: 'PUT', body: JSON.stringify({ min_sl: minSL }) },
		token
	);
}

export function listLogs(token: string, afterId?: number, limit = 200): Promise<LogEntry[]> {
	const params = new URLSearchParams();
	if (afterId !== undefined) params.set('after_id', String(afterId));
	else params.set('limit', String(limit));
	return request<LogEntry[]>(`/api/logs?${params}`, { method: 'GET' }, token);
}
