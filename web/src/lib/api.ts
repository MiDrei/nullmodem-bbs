export interface BinkpUplink {
	/** The uplink's own FTN address, for display/reference only -- not verified against what it claims when connecting. */
	address: string;
	/** "host:port", e.g. "bbs.example.com:24554". */
	host: string;
	password: string;
	/** Excludes this uplink from the mailer daemon's regular, interval-based scheduled poll -- it's still dialed immediately whenever there's netmail or echomail actually pending for it, plus its own poll_interval_seconds as a slow fallback if set, or manually via "Send Now". */
	poll_disabled: boolean;
	/** Overrides the global default poll interval for this uplink specifically (seconds). 0 means "use the default" -- some hubs only permit polling every hour or two. Meaningless when poll_disabled is set. */
	poll_interval_seconds: number;
	/** Authenticates the FTS-0001 packet itself (max 8 characters -- the wire format's packet header field), distinct from the BinkP session password above. Stamped on every outbound packet for this uplink; an inbound packet with a different password is rejected. */
	packet_password: string;
	/** Authenticates inbound TIC file-echo announcements from this uplink. */
	tic_password: string;
	/** Authenticates outbound echomail area subscription requests to this uplink's "Areafix" robot -- see requestAreafixSubscription. */
	areafix_password: string;
	/** Authenticates outbound file-echo area subscription requests to this uplink's "Filefix" robot -- see requestAreafixSubscription. */
	filefix_password: string;
	/** Which FTN network this uplink carries echomail for (e.g. "fsxNet", "HobbyNet"), matched case-insensitively against a message area's own Network to decide which uplink a locally-posted echo message goes out through. Empty means this uplink never sends locally-originated echomail. */
	network: string;
}

export interface BBSConfig {
	name: string;
	sysop: string;
	new_user_sl: number;
	/** This system's own FTN addresses/AKAs (zone:net/node.point), if any. The first is "primary": stamped on outgoing netmail. Most systems have exactly one; more than one is for a point reachable through the same uplink under multiple FTN networks. */
	ftn_addresses: string[];
	telnet_enabled: boolean;
	telnet_addr: string;
	ssh_enabled: boolean;
	ssh_addr: string;
	binkp_uplinks: BinkpUplink[];
	/** Default poll interval (seconds) for an uplink that doesn't set its own poll_interval_seconds. */
	binkp_default_poll_interval_seconds: number;
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

export interface BinkpStatus {
	own_ftn_addresses: string[];
	uplink_count: number;
	crash_only_uplink_count: number;
	pending_outbound: number;
	pending_crash: number;
}

export interface Dashboard {
	bbs_name: string;
	version: string;
	user_count: number;
	message_area_count: number;
	file_area_count: number;
	nodes: Node[];
	binkp: BinkpStatus;
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
	/** Set when internal/tosser auto-created this area for an inbound echomail AREA kludge it hadn't seen before -- invisible everywhere in the BBS until approved (see /pending-areas). Always false for an area created by hand. */
	pending: boolean;
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
	/** Mirrors MessageArea.pending -- set when a TIC/file-echo toss auto-created this area for a tag not seen before (see /pending-areas). */
	pending: boolean;
}

export interface PendingAreas {
	message_areas: MessageArea[];
	file_areas: FileArea[];
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

export function testBinkpConnection(
	token: string,
	uplink: BinkpUplink
): Promise<{ remote_addresses: string[] }> {
	return request(
		'/api/binkp/test-connection',
		{ method: 'POST', body: JSON.stringify(uplink) },
		token
	);
}

/** "echo" for an Areafix (message-area) request, "file" for a Filefix (file-echo area) request. */
export type AreafixKind = 'echo' | 'file';

/** One area to add (subscribe: true) or drop (false) in a requestAreafixChanges batch. */
export interface AreaChange {
	area_tag: string;
	subscribe: boolean;
}

/** Queues a single Crash-priority netmail asking uplink's Areafix (echo) or Filefix (file) robot to add/drop every area in changes -- sent via cmd/mailer's usual crash-trigger, typically within its 5-minute throttle window rather than immediately. */
export function requestAreafixChanges(
	token: string,
	uplink: BinkpUplink,
	changes: AreaChange[],
	kind: AreafixKind
): Promise<{ queued_message_id: number }> {
	return request(
		'/api/binkp/areafix/changes',
		{ method: 'POST', body: JSON.stringify({ uplink, changes, kind }) },
		token
	);
}

/** Queues a "%LIST" request asking uplink's robot to reply with every area it carries. The reply arrives later as ordinary netmail -- poll getAreafixListReply for it. */
export function requestAreafixList(
	token: string,
	uplink: BinkpUplink,
	kind: AreafixKind
): Promise<{ queued_message_id: number }> {
	return request(
		'/api/binkp/areafix/list',
		{ method: 'POST', body: JSON.stringify({ uplink, kind }) },
		token
	);
}

/** One area line recovered from a "%LIST" reply -- see requestAreafixList/getAreafixListReply. The parse is best-effort (hub software varies), so always show raw_body alongside it. */
export interface ParsedArea {
	Tag: string;
	Description: string;
	Subscribed: boolean;
}

export interface AreafixListReply {
	found: boolean;
	posted_at?: string;
	subject?: string;
	raw_body?: string;
	areas?: ParsedArea[];
}

/** The most recent inbound netmail from uplinkAddress, parsed as a "%LIST" reply -- found is false if nothing has arrived from that address yet. */
export function getAreafixListReply(token: string, uplinkAddress: string): Promise<AreafixListReply> {
	return request(
		`/api/binkp/areafix/list-reply?address=${encodeURIComponent(uplinkAddress)}`,
		{ method: 'GET' },
		token
	);
}

/** Area tags this system has itself outbound-requested from uplinkHost via requestAreafixChanges, for showing current state next to the subscribe/unsubscribe controls. */
export function listAreafixSubscriptions(
	token: string,
	uplinkHost: string,
	kind: AreafixKind
): Promise<{ area_tags: string[] }> {
	return request(
		`/api/binkp/areafix/subscriptions?host=${encodeURIComponent(uplinkHost)}&kind=${kind}`,
		{ method: 'GET' },
		token
	);
}

/** One local area in a listAreafixGrants response: whether uplinkHost is currently allowed to subscribe to it via the inbound Areafix/Filefix robot. */
export interface AreaGrant {
	tag: string;
	name: string;
	granted: boolean;
}

/** Every local echo (or file) area, alongside whether uplinkHost (a downlink) is currently granted access to it -- the source for the per-downlink checkbox list that governs what its own Areafix/Filefix requests to us will accept. */
export function listAreafixGrants(
	token: string,
	uplinkHost: string,
	kind: AreafixKind
): Promise<{ areas: AreaGrant[] }> {
	return request(
		`/api/binkp/areafix/grants?host=${encodeURIComponent(uplinkHost)}&kind=${kind}`,
		{ method: 'GET' },
		token
	);
}

/** Replaces the full set of areas uplinkHost (a downlink) is granted access to -- any currently-granted area missing from grantedTags is revoked, matching a checkbox list's own "save" semantics. */
export function setAreafixGrants(
	token: string,
	uplinkHost: string,
	kind: AreafixKind,
	grantedTags: string[]
): Promise<{ granted_tags: number }> {
	return request(
		'/api/binkp/areafix/grants',
		{ method: 'PUT', body: JSON.stringify({ host: uplinkHost, kind, granted_tags: grantedTags }) },
		token
	);
}

export function sendNowBinkp(
	token: string,
	uplink: BinkpUplink
): Promise<{
	sent: number;
	sent_echo: number;
	forwarded_echo: number;
	forwarded_files: number;
	received: number;
	received_echo: number;
	received_files: number;
	remote_addresses: string[];
}> {
	return request(
		'/api/binkp/send-now',
		{ method: 'POST', body: JSON.stringify(uplink) },
		token
	);
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

export type MessageAreaInput = Omit<MessageArea, 'id' | 'pending'>;

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

export type FileAreaInput = Omit<FileArea, 'id' | 'pending'>;

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

export function listPendingAreas(token: string): Promise<PendingAreas> {
	return request<PendingAreas>('/api/pending-areas', { method: 'GET' }, token);
}

/** Every distinct group ("network") already in use across message and file areas combined, sorted -- suggestions for that field on the area forms. */
export function listGroups(token: string): Promise<string[]> {
	return request<string[]>('/api/groups', { method: 'GET' }, token);
}

export function approvePendingMessageArea(token: string, id: number): Promise<MessageArea> {
	return request<MessageArea>(
		`/api/pending-areas/message-areas/${id}/approve`,
		{ method: 'POST' },
		token
	);
}

export function approvePendingFileArea(token: string, id: number): Promise<FileArea> {
	return request<FileArea>(`/api/pending-areas/file-areas/${id}/approve`, { method: 'POST' }, token);
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
