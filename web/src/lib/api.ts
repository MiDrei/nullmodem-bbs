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
	/** Classic FTN "Hold" status: never dialed automatically for any reason at all, not even poll_disabled's own "crash-style" immediate dial for pending mail -- only via "Send Now" (which ignores this, same as it already ignores poll_disabled), or by the uplink itself polling us. For a peer with no way to reach us back either (e.g. a point behind NAT), poll_disabled alone isn't enough: pending mail would still trigger a doomed dial attempt on every check. */
	hold: boolean;
	/** Restricts this uplink to a subset of this system's own FTN addresses (BBSConfig.ftn_addresses): only these are presented via M_ADR when polling it, and only their zones count as this uplink's own for Crash-mail routing -- lets one hub's M_ADR handshake not leak AKAs that belong to a different network entirely. Empty means unrestricted (every configured address applies, the old default). */
	aka_addresses: string[];
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
	hold_uplink_count: number;
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
	/** Echomail areas an inbound toss created that are still awaiting approval -- see /admin/pending-areas. */
	pending_message_area_count: number;
	/** Same, for file-echo areas. */
	pending_file_area_count: number;
	/** Inbound netmail stuck with no real recipient -- see /admin/netmail. Capped the same way that page's own list is. */
	unresolved_netmail_count: number;
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
	/** Only present from the BBS portal's file listing (see listBBSAreaFiles), not the sysop admin one. */
	unread?: boolean;
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

/** The BBS's own configured display name -- unauthenticated, shown in both the admin and BBS portal headers/login pages before anyone has a token. */
export function getBBSInfo(): Promise<{ name: string }> {
	return request('/api/bbs/info', { method: 'GET' });
}

export interface WelcomeScreen {
	html: string;
	preformatted: boolean;
	grid?: Grid;
}

/** welcome.ans rendered (unauthenticated), the same banner a Telnet/SSH caller sees on connect -- for the BBS portal login page. Real ANSI art comes back as preformatted+grid, meant for AnsiArt.svelte (see its own doc comment for why), the same as a message/netmail body. Returns null if no welcome.ans is configured (404), rather than throwing, since the login page should just render without a banner in that case. */
export async function getWelcomeScreen(): Promise<WelcomeScreen | null> {
	try {
		return await request<WelcomeScreen>('/api/bbs/welcome-screen', { method: 'GET' });
	} catch (err) {
		if (err instanceof ApiError && err.status === 404) return null;
		throw err;
	}
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

export interface UnresolvedNetmailSummary {
	id: number;
	from_name: string;
	from_address: string;
	to_name: string;
	subject: string;
	posted_at: string;
}

export interface UnresolvedNetmail extends UnresolvedNetmailSummary {
	body: string;
	body_html: string;
}

/** Inbound netmail whose recipient never resolved to a real local user and has no remote FTN destination either -- a mistyped username, or a reply from an automated robot (Areafix/Filefix, ...) with nowhere else to go. Otherwise invisible anywhere in the BBS. */
export function listUnresolvedNetmail(token: string): Promise<UnresolvedNetmailSummary[]> {
	return request<UnresolvedNetmailSummary[]>('/api/netmail/unresolved', { method: 'GET' }, token);
}

export function getUnresolvedNetmail(token: string, id: number): Promise<UnresolvedNetmail> {
	return request<UnresolvedNetmail>(`/api/netmail/unresolved/${id}`, { method: 'GET' }, token);
}

export function deleteUnresolvedNetmail(token: string, id: number): Promise<void> {
	return request<void>(`/api/netmail/unresolved/${id}`, { method: 'DELETE' }, token);
}

/** Dismisses several unresolved messages at once -- a nonexistent or already-resolved id is skipped rather than failing the whole batch. */
export function batchDeleteUnresolvedNetmail(token: string, ids: number[]): Promise<{ deleted: number }> {
	return request<{ deleted: number }>(
		'/api/netmail/unresolved/batch-delete',
		{ method: 'POST', body: JSON.stringify({ ids }) },
		token
	);
}

export interface ArchiveEntry {
	id: number;
	filename: string;
	uplink_address: string;
	uplink_host: string;
	size_bytes: number;
	size_human: string;
	received_at: string;
	/** "ok", "skipped", or "error" -- see internal/tosser's handleInboundFile. */
	outcome: string;
	detail: string;
}

export interface ArchiveList {
	entries: ArchiveEntry[];
	total: number;
	limit: number;
	offset: number;
}

/** Every inbound BinkP file this system has received recently, retained for a few days regardless of whether it tossed successfully -- see internal/archive's own doc comment. The web admin's Packet Analyzer. */
export function listArchive(token: string, limit: number, offset: number): Promise<ArchiveList> {
	const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
	return request<ArchiveList>(`/api/archive?${params}`, { method: 'GET' }, token);
}

export async function downloadArchiveEntry(token: string, id: number, filename: string): Promise<void> {
	const res = await fetch(`/api/archive/${id}/download`, {
		headers: { Authorization: `Bearer ${token}` }
	});
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
	const blob = await res.blob();
	const url = URL.createObjectURL(blob);
	const a = document.createElement('a');
	a.href = url;
	a.download = filename;
	document.body.appendChild(a);
	a.click();
	a.remove();
	URL.revokeObjectURL(url);
}

/** Fetches an archived entry's raw bytes for an inline preview -- each byte maps 1:1 to a code point (not a real charset decode), so a text-shaped file (a .tic, a .pkt's mostly-ASCII framing) reads naturally and nothing is lost for a genuinely binary one either. */
export async function previewArchiveEntry(token: string, id: number): Promise<string> {
	const res = await fetch(`/api/archive/${id}/download`, {
		headers: { Authorization: `Bearer ${token}` }
	});
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
	const buf = new Uint8Array(await res.arrayBuffer());
	let s = '';
	for (let i = 0; i < buf.length; i++) s += String.fromCharCode(buf[i]);
	return s;
}

export function deleteArchiveEntry(token: string, id: number): Promise<void> {
	return request<void>(`/api/archive/${id}`, { method: 'DELETE' }, token);
}

export interface RetossResult {
	received: number;
	received_echo: number;
	received_files: number;
	skipped_files: string[] | null;
}

/** Re-tosses one or more archived entries' raw bytes together, as if they'd just arrived in a single BinkP session -- select both halves of a TIC descriptor/payload pair to have them correlate the same way their original session did. */
export function retossArchiveEntries(token: string, ids: number[]): Promise<RetossResult> {
	return request<RetossResult>('/api/archive/retoss', { method: 'POST', body: JSON.stringify({ ids }) }, token);
}

export interface InspectedMessage {
	orig_addr: string;
	dest_addr: string;
	from_name: string;
	to_name: string;
	subject: string;
	written: string;
	private: boolean;
	/** The echomail area this message belongs to, "" for netmail. */
	area_tag: string;
	body_size: number;
}

export interface InspectedPacket {
	name: string;
	orig_addr: string;
	dest_addr: string;
	created: string;
	messages: InspectedMessage[];
}

export interface InspectedTIC {
	area: string;
	file: string;
	description: string;
	size_bytes: number;
	has_crc32: boolean;
	/** Hex, only meaningful when has_crc32 is true. */
	crc32: string;
	origin: string;
}

export interface ArchiveInspection {
	/** "packet", "bundle", "tic", or "unknown". */
	kind: string;
	/** One entry for a plain packet (kind === "packet") or one per packet found inside a bundle (kind === "bundle"). */
	packets?: InspectedPacket[];
	tic?: InspectedTIC;
	/** Set only when kind is "unknown" because parsing failed outright (rather than the file genuinely being of unrecognized shape). */
	error?: string;
}

/** Parses an archived entry's raw bytes as an FTN artifact (a packet, a packet bundle, or a TIC descriptor) and returns a structured summary of what it actually contains -- what the Packet Analyzer's detail view shows beyond the raw byte preview. */
export function inspectArchiveEntry(token: string, id: number): Promise<ArchiveInspection> {
	return request<ArchiveInspection>(`/api/archive/${id}/inspect`, { method: 'GET' }, token);
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

// ---------------------------------------------------------------------
// BBS user portal (routes/(portal)/*) -- separate login/endpoints from
// the sysop admin API above (see internal/web/auth.go's
// handleBBSLogin/requireBBSUser), open to any registered account.
// ---------------------------------------------------------------------

export interface BBSMessageArea {
	id: number;
	tag: string;
	name: string;
	description: string;
	network: string;
	min_sl_read: number;
	min_sl_write: number;
	total: number;
	new: number;
	yours: number;
}

export interface BBSMessageSummary {
	id: number;
	from_name: string;
	to_name: string;
	subject: string;
	posted_at: string;
	unread: boolean;
}

export interface BBSMessagePage {
	messages: BBSMessageSummary[];
	total: number;
	limit: number;
	offset: number;
}

export interface BBSMessage {
	id: number;
	area_id: number;
	from_name: string;
	to_name: string;
	subject: string;
	body: string;
	body_html: string;
	/** Real ANSI art or aligned block art -- render grid via AnsiArt.svelte instead of body_html (see its own doc comment for why); an ordinary message reads better as normal prose instead. */
	preformatted: boolean;
	/** Only set when preformatted is true. */
	grid?: Grid;
	posted_at: string;
	/** The message immediately before/after this one in the area's own reading order, for Prev/Next reader navigation -- absent at the first/last message. */
	prev_id?: number;
	next_id?: number;
}

export interface BBSNetmailSummary {
	id: number;
	from_name: string;
	to_name: string;
	subject: string;
	posted_at: string;
	unread: boolean;
	from_local: boolean;
}

export interface BBSNetmail {
	id: number;
	from_name: string;
	from_address: string;
	to_name: string;
	to_address: string;
	subject: string;
	body: string;
	body_html: string;
	/** See BBSMessage.preformatted. */
	preformatted: boolean;
	/** See BBSMessage.grid. */
	grid?: Grid;
	posted_at: string;
	/** See BBSMessage.prev_id/next_id -- within the caller's own inbox/sent list instead of an area. */
	prev_id?: number;
	next_id?: number;
	/** False when viewed from the caller's own Sent list (they were the sender, not the recipient) -- Reply/Delete only make sense when true. */
	is_recipient: boolean;
}

export interface BBSFileArea {
	id: number;
	tag: string;
	name: string;
	network: string;
	min_sl_download: number;
	min_sl_upload: number;
	total: number;
	new: number;
	yours: number;
}

export function bbsLogin(username: string, password: string): Promise<LoginResponse> {
	return request<LoginResponse>('/api/bbs/auth/login', {
		method: 'POST',
		body: JSON.stringify({ username, password })
	});
}

export function listBBSMessageAreas(token: string): Promise<BBSMessageArea[]> {
	return request<BBSMessageArea[]>('/api/bbs/message-areas', { method: 'GET' }, token);
}

export function listBBSMessages(
	token: string,
	areaId: number,
	limit: number,
	offset: number
): Promise<BBSMessagePage> {
	const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
	return request<BBSMessagePage>(
		`/api/bbs/message-areas/${areaId}/messages?${params}`,
		{ method: 'GET' },
		token
	);
}

/** Where (0-based, oldest-first) the caller should jump to on entering areaId -- see internal/message.Store.FirstUnreadPosition. Divide by your own page size to know which page to open. */
export function getFirstUnreadMessagePosition(token: string, areaId: number): Promise<{ position: number }> {
	return request(`/api/bbs/message-areas/${areaId}/first-unread`, { method: 'GET' }, token);
}

export function getBBSMessage(token: string, id: number): Promise<BBSMessage> {
	return request<BBSMessage>(`/api/bbs/messages/${id}`, { method: 'GET' }, token);
}

export function postBBSMessage(
	token: string,
	areaId: number,
	toName: string,
	subject: string,
	body: string
): Promise<BBSMessage> {
	return request<BBSMessage>(
		`/api/bbs/message-areas/${areaId}/messages`,
		{ method: 'POST', body: JSON.stringify({ to_name: toName, subject, body }) },
		token
	);
}

export function listBBSNetmail(token: string): Promise<BBSNetmailSummary[]> {
	return request<BBSNetmailSummary[]>('/api/bbs/netmail', { method: 'GET' }, token);
}

export function listBBSNetmailSent(token: string): Promise<BBSNetmailSummary[]> {
	return request<BBSNetmailSummary[]>('/api/bbs/netmail/sent', { method: 'GET' }, token);
}

export function getBBSNetmail(token: string, id: number): Promise<BBSNetmail> {
	return request<BBSNetmail>(`/api/bbs/netmail/${id}`, { method: 'GET' }, token);
}

export function sendBBSNetmail(
	token: string,
	to: string,
	subject: string,
	body: string,
	toName = '',
	crash = false
): Promise<BBSNetmail> {
	return request<BBSNetmail>(
		'/api/bbs/netmail',
		{ method: 'POST', body: JSON.stringify({ to, to_name: toName, subject, body, crash }) },
		token
	);
}

/** Same shape as internal/netmail.IsFTNAddress -- zone:net/node[.point], all-numeric. Used client-side only to decide whether to show the "recipient name" field, not for validation (the server re-checks). */
export function isFTNAddress(s: string): boolean {
	const m = s.match(/^(\d+):(\d+)\/(\d+)(?:\.(\d+))?$/);
	return m !== null;
}

export function deleteBBSNetmail(token: string, id: number): Promise<void> {
	return request<void>(`/api/bbs/netmail/${id}`, { method: 'DELETE' }, token);
}

export function listBBSFileAreas(token: string): Promise<BBSFileArea[]> {
	return request<BBSFileArea[]>('/api/bbs/file-areas', { method: 'GET' }, token);
}

export function listBBSAreaFiles(token: string, areaId: number): Promise<BBSFile[]> {
	return request<BBSFile[]>(`/api/bbs/file-areas/${areaId}/files`, { method: 'GET' }, token);
}

/** Loads one file's full metadata (including its full, possibly multi-line description) and marks it read -- the only other way to mark a file read is downloading it. */
export function getBBSFile(token: string, id: number): Promise<BBSFile> {
	return request<BBSFile>(`/api/bbs/files/${id}`, { method: 'GET' }, token);
}

export function uploadBBSAreaFile(
	token: string,
	areaId: number,
	file: File,
	description: string
): Promise<BBSFile> {
	const form = new FormData();
	form.set('file', file);
	form.set('description', description);
	return requestForm<BBSFile>(`/api/bbs/file-areas/${areaId}/files`, form, token);
}

export interface FilePreviewArchiveEntry {
	name: string;
	size_bytes: number;
}

/** Discriminated union keyed by kind: "image", "text" (CP437/ANSI, rendered the same way an echomail message body is), "archive" (a .zip's table of contents), or "none" when nothing about the file is worth previewing. Only the fields for that kind are set. */
export interface FilePreview {
	kind: 'image' | 'text' | 'archive' | 'none';
	body_html?: string;
	preformatted?: boolean;
	grid?: Grid;
	truncated?: boolean;
	content_type?: string;
	entries?: FilePreviewArchiveEntry[];
	entries_truncated?: boolean;
}

const PREVIEW_IMAGE_EXTS = new Set(['.png', '.jpg', '.jpeg', '.gif', '.webp', '.bmp']);
const PREVIEW_TEXT_EXTS = new Set(['.txt', '.nfo', '.diz', '.asc', '.me', '.1st', '.cat', '.ans']);

/** Mirrors the server's own extension-based preview-kind detection (see internal/web's previewImageExtensions/previewTextExtensions) -- lets the UI decide up front (no request needed) whether a file is worth a Preview button, and specifically whether it's a .zip whose contents list should just be shown outright. */
export function guessFilePreviewKind(filename: string): 'image' | 'text' | 'archive' | 'none' {
	const dot = filename.lastIndexOf('.');
	if (dot < 0) return 'none';
	const ext = filename.slice(dot).toLowerCase();
	if (PREVIEW_IMAGE_EXTS.has(ext)) return 'image';
	if (PREVIEW_TEXT_EXTS.has(ext)) return 'text';
	if (ext === '.zip') return 'archive';
	return 'none';
}

export function previewBBSFile(token: string, id: number): Promise<FilePreview> {
	return request<FilePreview>(`/api/bbs/files/${id}/preview`, { method: 'GET' }, token);
}

/** Fetches an image file's raw bytes (an <img> element can't attach a Bearer token itself) and returns an object URL for its src -- the caller must URL.revokeObjectURL it when done (e.g. onDestroy). */
export async function previewBBSFileImageURL(token: string, id: number): Promise<string> {
	const res = await fetch(`/api/bbs/files/${id}/preview-raw`, {
		headers: { Authorization: `Bearer ${token}` }
	});
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
	const blob = await res.blob();
	return URL.createObjectURL(blob);
}

/** preview-entry counterpart of previewBBSFile, for one named file inside a .zip. */
export function previewBBSFileEntry(token: string, id: number, name: string): Promise<FilePreview> {
	return request<FilePreview>(
		`/api/bbs/files/${id}/preview-entry?name=${encodeURIComponent(name)}`,
		{ method: 'GET' },
		token
	);
}

/** preview-entry-raw counterpart of previewBBSFileImageURL, for one named image inside a .zip. */
export async function previewBBSFileEntryImageURL(
	token: string,
	id: number,
	name: string
): Promise<string> {
	const res = await fetch(`/api/bbs/files/${id}/preview-entry-raw?name=${encodeURIComponent(name)}`, {
		headers: { Authorization: `Bearer ${token}` }
	});
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
	const blob = await res.blob();
	return URL.createObjectURL(blob);
}

// downloadBBSFile fetches the file with the portal's Bearer token
// (a plain <a href> can't attach an Authorization header) and hands
// the browser a synthetic download via an object URL -- the only
// part of this API that can't just be a real link, since every other
// BBS portal endpoint the download route sits alongside requires the
// same bearer auth the rest of the app already uses.
export async function downloadBBSFile(token: string, id: number, filename: string): Promise<void> {
	const res = await fetch(`/api/bbs/files/${id}/download`, {
		headers: { Authorization: `Bearer ${token}` }
	});
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
	const blob = await res.blob();
	const url = URL.createObjectURL(blob);
	const a = document.createElement('a');
	a.href = url;
	a.download = filename;
	document.body.appendChild(a);
	a.click();
	a.remove();
	URL.revokeObjectURL(url);
}
