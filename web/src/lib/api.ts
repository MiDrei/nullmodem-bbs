import { i18n } from './i18n.svelte';
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
	/** Send the session password in the clear even when the uplink offers CRAM-MD5 -- for diagnosing a hub only. */
	no_cram: boolean;
	/** Restricts this uplink to a subset of this system's own FTN addresses (BBSConfig.ftn_addresses): only these are presented via M_ADR when polling it, and only their zones count as this uplink's own for Crash-mail routing -- lets one hub's M_ADR handshake not leak AKAs that belong to a different network entirely. Empty means unrestricted (every configured address applies, the old default). */
	aka_addresses: string[];
	/** True for one of this system's own points/nodes it feeds (shown under "Nodes / Points"), false for an upstream hub/network feed (shown under "Hubs", the default). A downlink with a point address is a point: it only gets netmail addressed to it and its subscribed areas. */
	downlink: boolean;
}

export interface BBSConfig {
	name: string;
	sysop: string;
	/** Where the board is, sent to BinkP peers (LOC). */
	location: string;
	new_user_sl: number;
	/** An RSS feed per area a new caller may read, without login. */
	public_feeds: boolean;
	/** The monthly recap netmail to the sysops (on the 1st). */
	monthly_recap: boolean;
	/** This system's own FTN addresses/AKAs (zone:net/node.point), if any. The first is "primary": stamped on outgoing netmail. Most systems have exactly one; more than one is for a point reachable through the same uplink under multiple FTN networks. */
	ftn_addresses: string[];
	/** The FTN networks this system belongs to: short name (the group of their areas, what an uplink names) and FTN domain. */
	networks: FTNNetwork[];
	telnet_enabled: boolean;
	telnet_addr: string;
	ssh_enabled: boolean;
	ssh_addr: string;
	binkp_uplinks: BinkpUplink[];
	/** Default poll interval (seconds) for an uplink that doesn't set its own poll_interval_seconds. */
	binkp_default_poll_interval_seconds: number;
	/** InterBBS Last Callers: post a record per caller to a data echo, show the list. */
	last_callers: {
		enabled: boolean;
		area: string;
		address: string;
		system: string;
		show_at_login: boolean;
	};
}

export interface FTNNetwork {
	name: string;
	/** As after the @ in 21:3/100@fsxnet. */
	domain: string;
	/** The name it was loaded under -- send back unchanged so a rename carries over to uplinks and areas. */
	original_name?: string;
}

/** The domain of an FTN address ("21:3/194@fsxnet" -> "fsxnet"), or "". */
export function addressDomain(addr: string): string {
	const i = addr.lastIndexOf('@');
	return i >= 0 ? addr.slice(i + 1).trim().toLowerCase() : '';
}

export interface LoginResponse {
	token: string;
	username: string;
	security_level: number;
	/** Portal login only: the caller's profile time zone (IANA name), or "" if not set. */
	timezone?: string;
	/** Portal login only: the account's language, "" for the board's own. */
	language?: string;
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
	/** New accounts waiting for approval -- see /admin/users. */
	pending_user_count: number;
	/** Addresses locked out now for failed logins -- see /admin/security. */
	locked_out_count: number;
	/** Page rooms with a caller waiting for the sysop ("page-bob"). */
	paging: string[];
	problems: HealthProblem[];
	/** How the mailer gets on with each uplink, from the kept BinkP sessions. */
	uplinks: UplinkStatus[];
	system: SystemStatus;
}

export interface UplinkStatus {
	address: string;
	network: string;
	host: string;
	downlink: boolean;
	hold: boolean;
	poll_disabled: boolean;
	/** RFC3339, or "" if never. */
	last_ok: string;
	last_error: string;
	error: string;
	sessions_24h: number;
	errors_24h: number;
}

export interface SystemStatus {
	db_bytes: number;
	free_bytes: number;
	backup_enabled: boolean;
	last_backup: { name: string; size: number; time: string } | null;
	offsite_enabled: boolean;
	offsite: { last_ok: string; last_name: string; last_try: string; last_error: string; remote: number };
	services: { name: string; version: string; started_at: string; running: boolean }[];
	/** The latest warnings and errors of the last week, newest first. */
	warnings: { at: string; level: string; source: string; message: string }[];
}

export interface BBSUser {
	id: number;
	username: string;
	/** Optional, collected at Telnet/SSH registration or corrected by a sysop -- "" if never set. */
	real_name: string;
	security_level: number;
	created_at: string;
	last_login_at: string | null;
	total_calls: number;
	/** False while the account waits for the sysop's approval. */
	validated: boolean;
	/** Logs into the admin with an authenticator code too. */
	two_factor: boolean;
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
	/** A data area (e.g. FSX_DAT): tossed and forwarded, but left out of callers' area lists. */
	hidden?: boolean;
	/** Own cleanup limits: 0 = the default, -1 = keep everything, else days / messages. */
	keep_days?: number;
	keep_max?: number;
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
	/** Own cleanup limit: 0 = the default, -1 = keep everything, else days. */
	keep_days?: number;
	/** Anyone may download its files through a share link, without login. */
	public?: boolean;
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
	/** The file's public page (/share/f/…), when its area is public. */
	share_page?: string;
}

/** A file anyone may download (a public file area). */
export interface PublicFile {
	id: number;
	filename: string;
	summary: string;
	description?: string;
	preformatted?: boolean;
	grid?: Grid;
	size: string;
	area: string;
	network?: string;
	uploaded_by: string;
	uploaded_at: string;
	downloads: number;
	page: string;
	download: string;
}

export function listPublicFiles(limit = 8): Promise<PublicFile[]> {
	return request(`/api/public/files?limit=${limit}`, { method: 'GET' });
}

export function getPublicFile(id: number): Promise<PublicFile> {
	return request(`/api/public/files/${id}`, { method: 'GET' });
}

export interface MenuItem {
	key: string;
	label: string;
	action: string;
	min_sl: number;
	/** The label in other languages, as the sysop wrote them. */
	labels?: Record<string, string>;
	/** What callers in each language see (read only). */
	shown?: Record<string, string>;
}

export interface MenuDef {
	name: string;
	title: string;
	titles?: Record<string, string>;
	titles_shown?: Record<string, string>;
	/** A hand-designed screen shown instead of the generated list. */
	screen?: string;
	items: MenuItem[];
}

export interface ScreenSummary {
	name: string;
	/** The language it's for ("de", from its name); "" for every language without its own. */
	lang: string;
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
	// No content (204, or a 202 "accepted" without a body): nothing to
	// parse -- reading it as JSON would turn a success into an error.
	const text = await res.text();
	if (res.status === 204 || text.trim() === '') {
		return undefined as T;
	}
	return JSON.parse(text) as T;
}

async function request<T>(path: string, options: RequestInit = {}, token?: string): Promise<T> {
	const headers = new Headers(options.headers);
	if (options.body !== undefined) headers.set('Content-Type', 'application/json');
	if (token) headers.set('Authorization', `Bearer ${token}`);
	// Errors come back in the page's language.
	headers.set('X-Lang', i18n.lang);

	const res = await fetch(path, { ...options, headers });
	return handleResponse<T>(res);
}

// requestForm is used for multipart/form-data uploads: the browser
// must set the Content-Type itself (it includes a generated boundary
// string), so it's deliberately left unset here.
async function requestForm<T>(path: string, formData: FormData, token: string): Promise<T> {
	const res = await fetch(path, {
		method: 'POST',
		headers: { Authorization: `Bearer ${token}`, 'X-Lang': i18n.lang },
		body: formData
	});
	return handleResponse<T>(res);
}

/** What the BBS shows before anyone has a token: its name, software version, and the Telnet/SSH ports on this same host (absent when that server is disabled). */
export interface BBSInfo {
	name: string;
	version: string;
	telnet_port?: string;
	ssh_port?: string;
}

export function getBBSInfo(): Promise<BBSInfo> {
	return request('/api/bbs/info', { method: 'GET' });
}

/** The public front page's data (GET /api/public/overview, no login): how to reach the board and what's going on, handles only. */
export interface PublicOverview {
	name: string;
	sysop: string;
	location: string;
	version: string;
	telnet_port?: string;
	ssh_port?: string;
	binkp_port?: string;
	networks: { name: string; addresses: string[] }[];
	online: { node: number; handle: string; since: string }[];
	callers: { handle: string; at: string; place?: string }[];
	oneliners: { handle: string; text: string; at: string }[];
	doors: string[];
	stats: Record<string, number>;
	since_year?: number;
}

export function getPublicOverview(): Promise<PublicOverview> {
	return request('/api/public/overview', { method: 'GET' });
}

/** What goes on at the board (internal/stats): the public part, and with getStats the sysop's too. */
export interface StatsRanked {
	name: string;
	count: number;
	detail?: string;
	minutes?: number;
}
export interface StatsDay {
	date: string;
	count: number;
}
export interface StatsReport {
	days: number;
	calls: number;
	callers: number;
	posts: number;
	calls_per_day: StatsDay[];
	top_callers: StatsRanked[] | null;
	top_posters: StatsRanked[] | null;
	top_areas: StatsRanked[] | null;
	networks: { network: string; in: number; out: number; weeks: { week: string; in: number; out: number }[] }[] | null;
	top_doors: StatsRanked[] | null;
	top_files: StatsRanked[] | null;
	via?: StatsRanked[] | null;
	by_hour?: number[];
	new_users?: StatsRanked[] | null;
	uplinks?: StatsRanked[] | null;
	posts_per_day?: StatsDay[];
}

/** The public RSS feeds (empty while the sysop has them off). */
export function getPublicFeeds(): Promise<{ tag: string; name: string; network: string; url: string }[]> {
	return request('/api/public/feeds', { method: 'GET' });
}

export function getPublicStats(): Promise<StatsReport> {
	return request('/api/public/stats', { method: 'GET' });
}

/** A recap of the last 30 days to every sysop's netmail, now. */
export function sendRecap(token: string): Promise<{ sent: number }> {
	return request('/api/stats/recap', { method: 'POST' }, token);
}

export function getStats(token: string, days: number): Promise<StatsReport> {
	return request(`/api/stats?days=${days}`, { method: 'GET' }, token);
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

/** Thrown by login when the account needs its two-factor code (again). */
export class TwoFactorRequired extends ApiError {}

export async function login(username: string, password: string, code = ''): Promise<LoginResponse> {
	const res = await fetch('/api/auth/login', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ username, password, code })
	});
	const body = await res.json().catch(() => ({}));
	if (res.ok) return body as LoginResponse;
	if (body?.totp_required) throw new TwoFactorRequired(res.status, body.error ?? 'Enter your code.');
	throw new ApiError(res.status, body?.error ?? res.statusText);
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

/** One request to a hub's Areafix/Filefix robot (outgoing; its password isn't included) or one of the robot's replies. */
export interface AreafixHistoryEntry {
	id: number;
	outgoing: boolean;
	at: string;
	subject?: string;
	body: string;
	sent?: boolean;
}

/** Requests to that robot at the hub and its replies, newest first. */
export function getAreafixHistory(token: string, uplinkAddress: string, kind: AreafixKind): Promise<AreafixHistoryEntry[]> {
	return request(`/api/binkp/areafix/history?address=${encodeURIComponent(uplinkAddress)}&kind=${kind}`, { method: 'GET' }, token);
}

/** Sends one robot command (%LINKED, %UNLINKED, %HELP, %PAUSE, %RESUME, ...); its answer shows in the history. */
export function sendAreafixCommand(token: string, uplink: BinkpUplink, kind: AreafixKind, command: string): Promise<{ queued_message_id: number }> {
	return request('/api/binkp/areafix/command', { method: 'POST', body: JSON.stringify({ uplink, kind, command }) }, token);
}

/** Asks uplink's Areafix/Filefix robot which areas it has linked to us ("%QUERY"); see getAreafixQueryReply. */
export function requestAreafixQuery(token: string, uplink: BinkpUplink, kind: AreafixKind): Promise<{ queued_message_id: number }> {
	return request('/api/binkp/areafix/query', { method: 'POST', body: JSON.stringify({ uplink, kind }) }, token);
}

/** The hub's answer to the last "%QUERY": asked is when it went out (zero time: never), tags the areas it says are linked. */
export interface AreafixQueryReply {
	asked: string;
	found: boolean;
	posted_at?: string;
	subject?: string;
	raw_body?: string;
	tags: string[];
}

/** known: area tags the page knows (the hub's list, our record), so a reply's prose isn't taken for areas. */
export function getAreafixQueryReply(token: string, uplinkAddress: string, kind: AreafixKind, known: string[]): Promise<AreafixQueryReply> {
	return request(
		`/api/binkp/areafix/query-reply?address=${encodeURIComponent(uplinkAddress)}&kind=${kind}&known=${encodeURIComponent(known.join(','))}`,
		{ method: 'GET' },
		token
	);
}

/** Makes our record of what we're subscribed to at uplinkHost exactly tags (the hub's word); nothing is sent. */
export function adoptAreafixSubscriptions(token: string, uplinkHost: string, kind: AreafixKind, tags: string[]): Promise<{ added: number; removed: number }> {
	return request('/api/binkp/areafix/subscriptions', { method: 'PUT', body: JSON.stringify({ host: uplinkHost, kind, area_tags: tags }) }, token);
}

/** The most recent inbound netmail from uplinkAddress, parsed as a "%LIST" reply -- found is false if nothing has arrived from that address yet. */
export function getAreafixListReply(token: string, uplinkAddress: string, kind: AreafixKind = 'echo'): Promise<AreafixListReply> {
	return request(
		`/api/binkp/areafix/list-reply?address=${encodeURIComponent(uplinkAddress)}&kind=${kind}`,
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

/** realName omitted (undefined) leaves the stored real name untouched -- pass it explicitly (including "") only when the sysop actually means to change it. */
export function setUserSecurityLevel(
	token: string,
	id: number,
	securityLevel: number,
	realName?: string
): Promise<BBSUser> {
	const body: { security_level: number; real_name?: string } = { security_level: securityLevel };
	if (realName !== undefined) body.real_name = realName;
	return request<BBSUser>(`/api/users/${id}`, { method: 'PUT', body: JSON.stringify(body) }, token);
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

/** A menu for the editor: what the stock menu of this version has that it lacks, and which menus lead here. */
export interface MenuEdit {
	menu: MenuDef;
	missing_defaults: MenuItem[];
	used_by: string[];
}

export function getMenu(token: string, name: string): Promise<MenuEdit> {
	return request(`/api/menus/${encodeURIComponent(name)}`, { method: 'GET' }, token);
}

export function saveMenu(token: string, m: MenuDef): Promise<MenuEdit> {
	return request(`/api/menus/${encodeURIComponent(m.name)}`, { method: 'PUT', body: JSON.stringify(m) }, token);
}

export function deleteMenu(token: string, name: string): Promise<void> {
	return request(`/api/menus/${encodeURIComponent(name)}`, { method: 'DELETE' }, token);
}

export interface MenuBuiltin {
	name: string;
	label: string;
	description: string;
	sysop?: boolean;
}

export function getMenuActions(token: string): Promise<{ builtins: MenuBuiltin[]; menus: string[] }> {
	return request('/api/menu-actions', { method: 'GET' }, token);
}

/** A menu as a caller at sl sees it; not_shown: items its screen doesn't seem to mention; only_on_screen: keys it shows that nothing answers to. */
export function previewMenu(
	token: string,
	m: MenuDef,
	sl: number,
	lang = 'en'
): Promise<{ grid: Grid; has_screen: boolean; not_shown: MenuItem[]; only_on_screen: string[] }> {
	return request('/api/menu-preview', { method: 'POST', body: JSON.stringify({ menu: m, sl, lang }) }, token);
}

/** A language the board speaks (internal/i18n). */
export interface Language {
	code: string;
	name: string;
}

export interface Languages {
	languages: Language[];
	/** What callers read before logging in, and after if they never chose one. */
	board: string;
	fallback: string;
	/** How many texts the sysop changed, per language. */
	changed: Record<string, number>;
}

/** One text of the catalog in a language. */
export interface CatalogText {
	key: string;
	english: string;
	/** The built-in text in this language, or what it falls back to (from). */
	builtin: string;
	from?: string;
	/** The sysop's own text; "" keeps the built-in one. */
	text: string;
	placeholders: string[];
	/** Keys merged into this one: where the text is used besides. */
	was?: string[];
}

export function getLanguages(token: string): Promise<Languages> {
	return request('/api/i18n', { method: 'GET' }, token);
}

export function setBoardLanguage(token: string, language: string): Promise<void> {
	return request('/api/i18n/board', { method: 'PUT', body: JSON.stringify({ language }) }, token);
}

export function getCatalog(token: string, lang: string): Promise<CatalogText[]> {
	return request(`/api/i18n/${encodeURIComponent(lang)}`, { method: 'GET' }, token);
}

/** All of the sysop's own texts for lang (one left out goes back to the built-in text). */
export function saveCatalog(token: string, lang: string, texts: Record<string, string>): Promise<void> {
	return request(`/api/i18n/${encodeURIComponent(lang)}`, { method: 'PUT', body: JSON.stringify({ texts }) }, token);
}

/** The Logs page's tabs (internal/applog categories); "" is everything. */
export type LogCategory = '' | 'mailer' | 'system' | 'telnet' | 'ssh' | 'web';

export interface LogQuery {
	category?: LogCategory;
	levels?: LogEntry['level'][];
	/** Only lines starting with one of these (a door's start with "Name:"). */
	prefixes?: string[];
	excludePrefixes?: string[];
	q?: string;
	/** Older than this id (loading more), or newer (polling). */
	beforeId?: number;
	afterId?: number;
	limit?: number;
}

/** Log entries matching query, oldest first. */
export function listLogs(token: string, query: LogQuery = {}): Promise<LogEntry[]> {
	const params = new URLSearchParams();
	if (query.category) params.set('category', query.category);
	for (const l of query.levels ?? []) params.append('level', l);
	for (const p of query.prefixes ?? []) params.append('prefix', p);
	for (const p of query.excludePrefixes ?? []) params.append('exclude_prefix', p);
	if (query.q) params.set('q', query.q);
	if (query.beforeId) params.set('before_id', String(query.beforeId));
	if (query.afterId) params.set('after_id', String(query.afterId));
	if (query.limit) params.set('limit', String(query.limit));
	return request<LogEntry[]>(`/api/logs?${params}`, { method: 'GET' }, token);
}

/** One recorded BinkP session's summary (see internal/binkplog) -- the admin Logs page's BinkP tab. Its full frame-by-frame transcript is fetched separately via getBinkpSessionTranscript, only once a row's Detail is opened. */
export interface BinkpSessionEntry {
	id: number;
	/** "outbound" (we dialed) or "inbound" (they dialed us). */
	direction: 'outbound' | 'inbound';
	peer_address: string;
	peer_host: string;
	started_at: string;
	size_bytes: number;
	/** "ok" or "error"; detail carries the error message. */
	outcome: 'ok' | 'error';
	detail: string;
}

export function listBinkpSessions(token: string, limit = 100): Promise<BinkpSessionEntry[]> {
	return request<BinkpSessionEntry[]>(`/api/binkp/sessions?limit=${limit}`, { method: 'GET' }, token);
}

/** Fetches one session's full recorded transcript (M_PWD already redacted server-side) as plain text. */
export async function getBinkpSessionTranscript(token: string, id: number): Promise<string> {
	const res = await fetch(`/api/binkp/sessions/${id}/transcript`, {
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
	return res.text();
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
	/** One of the caller's areas: in the new scan, QWK and the reader. */
	mine: boolean;
}

/** Puts an area into the caller's areas, or takes it out. */
export function setMyArea(token: string, areaId: number, mine: boolean): Promise<void> {
	return request(`/api/bbs/message-areas/${areaId}/mine`, { method: 'PUT', body: JSON.stringify({ mine }) }, token);
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
	/** The message this one answers, if known. */
	reply_to?: number;
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
	/** A mail through the email gateway: the other side's address. Absent for netmail. */
	email?: string;
	/** A mail written here: how sending it goes; email_error says why it's stuck or failed. */
	email_status?: 'queued' | 'sent' | 'failed';
	email_error?: string;
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

/** Marks every message in the area read for the caller. */
export function markBBSAreaRead(token: string, areaId: number): Promise<{ marked: number }> {
	return request(`/api/bbs/message-areas/${areaId}/mark-read`, { method: 'POST' }, token);
}

export function getBBSMessage(token: string, id: number): Promise<BBSMessage> {
	return request<BBSMessage>(`/api/bbs/messages/${id}`, { method: 'GET' }, token);
}

export function postBBSMessage(
	token: string,
	areaId: number,
	toName: string,
	subject: string,
	body: string,
	replyTo?: number
): Promise<BBSMessage> {
	return request<BBSMessage>(
		`/api/bbs/message-areas/${areaId}/messages`,
		{ method: 'POST', body: JSON.stringify({ to_name: toName, subject, body, reply_to: replyTo ?? 0 }) },
		token
	);
}

/** One message of a thread, in reading order (depth-first). */
export interface ThreadEntry {
	id: number;
	reply_to?: number;
	depth: number;
	from_name: string;
	to_name: string;
	subject: string;
	posted_at: string;
	read: boolean;
	/** Linked by its "Re:" subject only. */
	guessed?: boolean;
}

export function getBBSThread(token: string, messageId: number): Promise<{ area_id: number; messages: ThreadEntry[] }> {
	return request(`/api/bbs/messages/${messageId}/thread`, { method: 'GET' }, token);
}

/** An area's thread: its first message and how much happened since. */
export interface ThreadSummary {
	id: number;
	from_name: string;
	subject: string;
	posted_at: string;
	replies: number;
	unread: number;
	last_at: string;
	last_from: string;
}

export function listBBSThreads(token: string, areaId: number, limit: number, offset: number): Promise<{ threads: ThreadSummary[]; total: number }> {
	return request(`/api/bbs/message-areas/${areaId}/threads?limit=${limit}&offset=${offset}`, { method: 'GET' }, token);
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
	crash = false,
	/** The netmail this answers -- a mail that came in by email is answered by email. */
	replyTo = 0
): Promise<BBSNetmail> {
	return request<BBSNetmail>(
		'/api/bbs/netmail',
		{ method: 'POST', body: JSON.stringify({ to, to_name: toName, subject, body, crash, reply_to: replyTo }) },
		token
	);
}

/** Looks like an email address (the server re-checks). */
export function isEmailAddress(s: string): boolean {
	return /^[^\s@<>]+@[^\s@<>:/]+\.[^\s@<>:/]+$/.test(s.trim());
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

export interface QWKArea {
	id: number;
	name: string;
	description: string;
	selected: boolean;
}

export function listQWKAreas(token: string): Promise<QWKArea[]> {
	return request<QWKArea[]>('/api/bbs/qwk/areas', { method: 'GET' }, token);
}

export function setQWKAreas(token: string, areaIds: number[]): Promise<void> {
	return request<void>(
		'/api/bbs/qwk/areas',
		{ method: 'PUT', body: JSON.stringify({ area_ids: areaIds }) },
		token
	);
}

/** Downloads the caller's current .QWK packet the same way downloadBBSFile does. Returns false (nothing fetched, nothing to save) when the server reports no new mail via 204. */
export async function downloadQWKPacket(token: string): Promise<boolean> {
	const res = await fetch('/api/bbs/qwk/download', {
		headers: { Authorization: `Bearer ${token}` }
	});
	if (res.status === 204) return false;
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
	const disposition = res.headers.get('Content-Disposition') ?? '';
	const match = disposition.match(/filename="([^"]+)"/);
	const filename = match ? match[1] : 'packet.qwk';
	const blob = await res.blob();
	const url = URL.createObjectURL(blob);
	const a = document.createElement('a');
	a.href = url;
	a.download = filename;
	document.body.appendChild(a);
	a.click();
	a.remove();
	URL.revokeObjectURL(url);
	return true;
}

export interface QWKReplyRejected {
	/** Position of the reply in the uploaded packet, from 0. */
	index: number;
	to: string;
	subject: string;
	reason: string;
}

export interface QWKReplyResult {
	posted: number;
	sent: number;
	skipped: number;
	/** The replies that were not delivered, and why. */
	rejected: QWKReplyRejected[];
}

export function uploadQWKReply(token: string, file: File): Promise<QWKReplyResult> {
	const form = new FormData();
	form.set('file', file);
	return requestForm<QWKReplyResult>('/api/bbs/qwk/upload', form, token);
}

/** The caller's own account overview and settings -- the same the Telnet/SSH profile shows. */
export interface BBSProfile {
	username: string;
	real_name: string;
	security_level: number;
	total_calls: number;
	created_at: string;
	/** IANA zone name, or "" if not set (the portal then uses the browser's own zone, Telnet/SSH uses UTC). */
	timezone: string;
	/** QWK packets carry echomail's SEEN-BY/PATH lines (for a reader that hides them, like NullModem Reader). */
	qwk_routing: boolean;
	/** Where the caller is ("Neunkirch, Switzerland"), shown on the InterBBS last callers list; "" if not set. */
	location: string;
	/** The language the caller reads the board in (Telnet and web); "" for the board's own. */
	language: string;
	/** The caller's address at the email gateway; absent when they can't use it. */
	email?: string;
}

export function getBBSProfile(token: string): Promise<BBSProfile> {
	return request<BBSProfile>('/api/bbs/profile', { method: 'GET' }, token);
}

/** Fields left undefined are left unchanged. */
export function updateBBSProfile(
	token: string,
	changes: { real_name?: string; timezone?: string; qwk_routing?: boolean; location?: string; language?: string }
): Promise<BBSProfile> {
	return request<BBSProfile>('/api/bbs/profile', { method: 'PUT', body: JSON.stringify(changes) }, token);
}

export function changeBBSPassword(token: string, currentPassword: string, newPassword: string): Promise<void> {
	return request<void>(
		'/api/bbs/profile/password',
		{
			method: 'POST',
			body: JSON.stringify({ current_password: currentPassword, new_password: newPassword })
		},
		token
	);
}

// --- Doors (sysop) ---

export interface Door {
	name: string;
	/** "" or "native" for a native Linux door, "dosbox" for a DOS door under DOSBox-X. */
	kind: string;
	min_sl: number;
	exe: string;
	dir: string;
	args: string[];
	dosbox_dir: string;
	dosbox_launch_cmd: string;
	/** "" = the kind's default (DOOR32.SYS native, DOOR.SYS dosbox). */
	dropfile: string;
	dropfile_in_door_dir: boolean;
	lock_files: string[];
	/** Native doors only: talk over stdin/stdout instead of the DOOR32.SYS socket. */
	stdio: boolean;
	/** Reduce the door's 256/true colours to the 16 classic ANSI colours. */
	ansi16: boolean;
	/** Kind "rlogin": the door server and the user names sent to it. */
	remote: RemoteDoor;
	template: string;
	/** Daily maintenance: DOS commands (dosbox) or a command line (native); "" none. */
	daily: string;
	/** Its time, "HH:MM" ("" = 00:05). */
	daily_at: string;
	/** How its last run went, read-only. */
	daily_state?: { last_at: string; ok: boolean; detail: string } | null;
	/** Files the door writes for the board (scores, news); public ones also on the front page. */
	bulletins?: DoorBulletin[];
	/** What its template offers, while it has none. */
	template_bulletins?: DoorBulletin[];
	/** Native doors only: a background program kept running while the door is set up (uMRC's umrc-bridge). */
	program: string[];
	/** Read-only: the door's directory exists and has files. */
	installed: boolean;
	/** Read-only, for a door from a template with a release feed: the release installed, the newest one and its notes. */
	version?: string;
	latest_version?: string;
	release_url?: string;
	update_available: boolean;
}

export interface DoorsResponse {
	doors: Door[];
	dropfile_formats: string[];
	doors_dir: string;
}

export interface DoorTemplate {
	id: string;
	name: string;
	description: string;
	license: string;
	dir: string;
	/** "native" or "dosbox" (also when empty). */
	kind: string;
	exe?: string;
	args?: string[];
	stdio?: boolean;
	ansi16?: boolean;
	dosbox_launch_cmd?: string;
	dropfile: string;
	dropfile_in_door_dir: boolean;
	lock_files?: string[];
	setup?: string;
	source_url?: string;
	program?: string[];
	/** uMRC: installing asks for what the chat network shows about this board. */
	mrc?: boolean;
	downloadable: boolean;
	installed: boolean;
	configured: boolean;
}

export function listDoors(token: string): Promise<DoorsResponse> {
	return request<DoorsResponse>('/api/doors', { method: 'GET' }, token);
}

export function putDoors(token: string, doors: Door[]): Promise<DoorsResponse> {
	return request<DoorsResponse>('/api/doors', { method: 'PUT', body: JSON.stringify({ doors }) }, token);
}

export function listDoorTemplates(token: string): Promise<DoorTemplate[]> {
	return request<DoorTemplate[]>('/api/doors/templates', { method: 'GET' }, token);
}

/** Installs (downloads, if the template allows) and adds a door from a template. */
export function addDoorFromTemplate(token: string, id: string, mrc?: MRCConfig): Promise<DoorsResponse> {
	return request<DoorsResponse>(
		`/api/doors/templates/${encodeURIComponent(id)}`,
		{ method: 'POST', body: mrc ? JSON.stringify({ mrc }) : undefined },
		token
	);
}

/** Asks the doors' projects for new releases now. */
export function checkDoorUpdates(token: string): Promise<DoorsResponse> {
	return request<DoorsResponse>('/api/doors/check-updates', { method: 'POST' }, token);
}

/** Brings a door to its newest release; save games and settings stay. */
export function updateDoor(token: string, name: string): Promise<DoorsResponse> {
	return request<DoorsResponse>(`/api/doors/update/${encodeURIComponent(name)}`, { method: 'POST' }, token);
}

/** uMRC's mrc.cfg: the Multi-Relay Chat host, and what the chat network shows about this board. */
export interface MRCConfig {
	host: string;
	port: string;
	ssl: boolean;
	bbs_name: string;
	software: string;
	website: string;
	telnet: string;
	ssh: string;
	sysop: string;
	description: string;
}

export function getMRCConfig(token: string): Promise<MRCConfig> {
	return request<MRCConfig>('/api/doors/mrc', { method: 'GET' }, token);
}

/** Saves mrc.cfg; the door's umrc-bridge restarts to pick it up. */
export function putMRCConfig(token: string, mrc: MRCConfig): Promise<MRCConfig> {
	return request<MRCConfig>('/api/doors/mrc', { method: 'PUT', body: JSON.stringify(mrc) }, token);
}

// --- Services (sysop) ---

export interface ServiceStatus {
	name: 'bbs' | 'mailer' | 'web';
	running: boolean;
	version: string;
	pid: number;
	started_at?: string;
	heartbeat_at?: string;
	/** A restart was asked for and hasn't happened yet. */
	restart_pending: boolean;
	restart_mode?: string;
	/** Why a restart is due -- saved changes the daemon only reads at startup. */
	restart_needed: string[];
	/** Callers online (bbs only). */
	online: number;
}

export function listServices(token: string): Promise<ServiceStatus[]> {
	return request<ServiceStatus[]>('/api/services', { method: 'GET' }, token);
}

/** mode "idle" (bbs only) waits until no caller is online. */
export function restartService(token: string, name: string, mode: 'now' | 'idle' = 'now'): Promise<void> {
	return request<void>(`/api/services/${encodeURIComponent(name)}/restart`, { method: 'POST', body: JSON.stringify({ mode }) }, token);
}

/** One InterBBS last caller (see internal/lastcallers). */
export interface LastCaller {
	alias: string;
	bbs: string;
	date: string;
	time: string;
	location: string;
	system: string;
	address: string;
}

export function listLastCallers(token: string): Promise<LastCaller[]> {
	return request<LastCaller[]>('/api/bbs/last-callers', { method: 'GET' }, token);
}

// --- Maintenance (sysop) ---

/** The nightly cleanup's settings; 0 keeps everything where a limit is optional. */
export interface MaintenanceSettings {
	enabled: boolean;
	hour: number;
	message_keep_days: number;
	message_keep_max: number;
	data_area_keep_days: number;
	file_keep_days: number;
	netmail_keep_days: number;
	log_keep_rows: number;
	transcript_keep_days: number;
	archive_keep_days: number;
	vacuum: boolean;
	pending_user_days: number;
}

/** What a cleanup deleted -- or, as a preview (dry_run), would delete. */
export interface MaintenanceReport {
	dry_run: boolean;
	started_at: string;
	seconds: number;
	messages: number;
	message_areas: { tag: string; count: number }[];
	files: number;
	file_bytes: number;
	netmail: number;
	logs: number;
	transcripts: number;
	archive: number;
	pending_users?: number;
	db_bytes_before: number;
	db_bytes_after: number;
	wal_bytes: number;
	vacuumed: boolean;
	errors: string[];
}

export interface MaintenanceState {
	settings: MaintenanceSettings;
	last: MaintenanceReport | null;
}

export function getMaintenance(token: string): Promise<MaintenanceState> {
	return request<MaintenanceState>('/api/maintenance', { method: 'GET' }, token);
}

export function putMaintenance(token: string, settings: MaintenanceSettings): Promise<MaintenanceState> {
	return request<MaintenanceState>('/api/maintenance', { method: 'PUT', body: JSON.stringify(settings) }, token);
}

/** Runs the cleanup now, or with dry only counts what it would delete. */
export function runMaintenance(token: string, dry: boolean): Promise<MaintenanceReport> {
	return request<MaintenanceReport>('/api/maintenance/run', { method: 'POST', body: JSON.stringify({ dry }) }, token);
}

/** The mobile reader's notifications (internal/push): this BBS's VAPID public key. */
export function getPushKey(token: string): Promise<{ public_key: string }> {
	return request('/api/bbs/push/key', { method: 'GET' }, token);
}

export interface PushPrefs {
	netmail: boolean;
	echomail: boolean;
}

/** This device's notification settings; 404 if it isn't subscribed. */
export function getPushSubscription(token: string, endpoint: string): Promise<PushPrefs> {
	return request(`/api/bbs/push/subscription?${new URLSearchParams({ endpoint })}`, { method: 'GET' }, token);
}

export function savePushSubscription(token: string, sub: PushSubscriptionJSON, prefs: PushPrefs): Promise<void> {
	return request('/api/bbs/push/subscription', { method: 'PUT', body: JSON.stringify({ ...sub, ...prefs }) }, token);
}

export function deletePushSubscription(token: string, endpoint: string): Promise<void> {
	return request('/api/bbs/push/subscription', { method: 'DELETE', body: JSON.stringify({ endpoint }) }, token);
}

export function testPush(token: string, endpoint: string): Promise<void> {
	return request('/api/bbs/push/test', { method: 'POST', body: JSON.stringify({ endpoint }) }, token);
}

/** The nightly backup (internal/backup). */
export interface BackupSettings {
	enabled: boolean;
	hour: number;
	keep_daily: number;
	keep_weekly: number;
	dir: string;
	include_files: boolean;
}

export interface BackupInfo {
	name: string;
	size: number;
	time: string;
}

export interface BackupState {
	settings: BackupSettings;
	backups: BackupInfo[];
	free_bytes: number;
}

export function getBackups(token: string): Promise<BackupState> {
	return request('/api/backups', { method: 'GET' }, token);
}

export function putBackupSettings(token: string, settings: BackupSettings): Promise<BackupState> {
	return request('/api/backups/settings', { method: 'PUT', body: JSON.stringify(settings) }, token);
}

export function runBackup(token: string): Promise<BackupState> {
	return request('/api/backups/run', { method: 'POST' }, token);
}

export function deleteBackup(token: string, name: string): Promise<void> {
	return request(`/api/backups/${encodeURIComponent(name)}`, { method: 'DELETE' }, token);
}

/** A backup's archive, for saving. */
export async function downloadBackup(token: string, name: string): Promise<Blob> {
	const res = await fetch(`/api/backups/${encodeURIComponent(name)}`, { headers: { Authorization: `Bearer ${token}` } });
	if (!res.ok) throw new ApiError(res.status, res.statusText);
	return res.blob();
}

export function approveUser(token: string, id: number): Promise<BBSUser> {
	return request(`/api/users/${id}/approve`, { method: 'POST' }, token);
}

/** Deletes an account still waiting for approval (turned down). */
export function deletePendingUser(token: string, id: number): Promise<void> {
	return request(`/api/users/${id}`, { method: 'DELETE' }, token);
}

/** Login protection and new-user approval (internal/guard). */
export interface SecuritySettings {
	lockout_enabled: boolean;
	max_failures: number;
	window_minutes: number;
	lockout_minutes: number;
	max_lockout_hours: number;
	max_connections_per_ip: number;
	approve_new_users: boolean;
	pending_sl: number;
	new_user_sl: number;
	blocked_handles: string[];
	require_admin_totp: boolean;
}

export interface IPLockout {
	ip: string;
	until: string;
	locked_at: string;
	strikes: number;
	reason: string;
}

export interface IPRule {
	pattern: string;
	kind: 'allow' | 'block';
	note: string;
	created_at: string;
}

export interface LoginFailure {
	ip: string;
	handle: string;
	source: string;
	at: string;
}

export interface SecurityState {
	settings: SecuritySettings;
	lockouts: IPLockout[];
	rules: IPRule[];
	failures: LoginFailure[];
	your_ip: string;
}

export function getSecurity(token: string): Promise<SecurityState> {
	return request('/api/security', { method: 'GET' }, token);
}

export function putSecuritySettings(token: string, settings: SecuritySettings): Promise<SecurityState> {
	return request('/api/security/settings', { method: 'PUT', body: JSON.stringify(settings) }, token);
}

export function unlockIP(token: string, ip: string): Promise<SecurityState> {
	return request('/api/security/unlock', { method: 'POST', body: JSON.stringify({ ip }) }, token);
}

export function addIPRule(token: string, pattern: string, kind: 'allow' | 'block', note: string): Promise<SecurityState> {
	return request('/api/security/rules', { method: 'POST', body: JSON.stringify({ pattern, kind, note }) }, token);
}

export function deleteIPRule(token: string, pattern: string): Promise<SecurityState> {
	return request('/api/security/rules', { method: 'DELETE', body: JSON.stringify({ pattern }) }, token);
}

/** Chat (internal/chat): a room with something going on. */
export interface ChatPresence {
	username: string;
	source: string;
}

export interface ChatLine {
	id: number;
	room: string;
	username: string;
	source: string;
	kind: 'say' | 'join' | 'leave' | 'page';
	text: string;
	at: string;
}

export interface ChatRoom {
	name: string;
	present: ChatPresence[];
	last_line: ChatLine | null;
	paging: boolean;
}

export interface ChatRoomState {
	lines: ChatLine[];
	present: ChatPresence[];
}

export function listChatRooms(token: string): Promise<ChatRoom[]> {
	return request('/api/chat/rooms', { method: 'GET' }, token);
}

/** The room's lines after afterId (the newest without); polling keeps you in it unless watch. */
export function getChatRoom(token: string, room: string, afterId = 0, watch = false): Promise<ChatRoomState> {
	const q = new URLSearchParams({ after: String(afterId) });
	if (watch) q.set('watch', '1');
	return request(`/api/chat/rooms/${encodeURIComponent(room)}?${q}`, { method: 'GET' }, token);
}

export function chatAction(token: string, room: string, action: 'enter' | 'say' | 'leave', text = ''): Promise<void> {
	return request(`/api/chat/rooms/${encodeURIComponent(room)}`, { method: 'POST', body: JSON.stringify({ action, text }) }, token);
}

/** Deletes everything said in room; who's in it stays. */
export function clearChatRoom(token: string, room: string): Promise<{ deleted: number }> {
	return request<{ deleted: number }>(`/api/chat/rooms/${encodeURIComponent(room)}/lines`, { method: 'DELETE' }, token);
}

/** announce_sysops: the rooms say when a sysop enters or leaves (off: they come and go without a word). */
export interface ChatSettingsState {
	announce_sysops: boolean;
}

export function getChatSettings(token: string): Promise<ChatSettingsState> {
	return request<ChatSettingsState>('/api/chat/settings', { method: 'GET' }, token);
}

export function saveChatSettings(token: string, s: ChatSettingsState): Promise<ChatSettingsState> {
	return request<ChatSettingsState>('/api/chat/settings', { method: 'PUT', body: JSON.stringify(s) }, token);
}

/** A room callers may enter; discord_channel bridges it (a channel ID). */
export interface ChatRoomSettings {
	name: string;
	title: string;
	topic: string;
	min_sl: number;
	sort_order: number;
	discord_channel: string;
	/** The Matrix room ID it's bridged to (an address "#room:server" is accepted when saving). */
	matrix_room: string;
}

export function listChatRoomSettings(token: string): Promise<ChatRoomSettings[]> {
	return request('/api/chat/room-settings', { method: 'GET' }, token);
}

export function saveChatRoom(token: string, r: ChatRoomSettings): Promise<ChatRoomSettings> {
	return request(`/api/chat/room-settings/${encodeURIComponent(r.name)}`, { method: 'PUT', body: JSON.stringify(r) }, token);
}

export function deleteChatRoom(token: string, name: string): Promise<void> {
	return request(`/api/chat/room-settings/${encodeURIComponent(name)}`, { method: 'DELETE' }, token);
}

/** The Discord bridge's bot -- never its token. */
export interface DiscordState {
	enabled: boolean;
	has_token: boolean;
	quiet: boolean;
	status: { enabled: boolean; has_token: boolean; connected: boolean; bot?: string; bot_id?: string; error?: string; since?: string; guilds?: string[] };
	channels: { id: string; name: string; guild: string }[];
	invite_url?: string;
}

export function getDiscord(token: string): Promise<DiscordState> {
	return request('/api/chat/discord', { method: 'GET' }, token);
}

export function saveDiscord(token: string, d: { enabled: boolean; quiet: boolean; token?: string; clear_token?: boolean }): Promise<DiscordState> {
	return request('/api/chat/discord', { method: 'PUT', body: JSON.stringify(d) }, token);
}

/** The Matrix bridge's bot -- never its token. */
export interface MatrixState {
	enabled: boolean;
	homeserver: string;
	user_id: string;
	has_token: boolean;
	quiet: boolean;
	status: { enabled: boolean; has_token: boolean; connected: boolean; user_id?: string; error?: string; warnings?: string[] };
	rooms: { id: string; name: string }[];
}

export function getMatrix(token: string): Promise<MatrixState> {
	return request('/api/chat/matrix', { method: 'GET' }, token);
}

export function saveMatrix(
	token: string,
	m: { enabled: boolean; quiet: boolean; homeserver?: string; user?: string; password?: string; forget?: boolean }
): Promise<MatrixState> {
	return request('/api/chat/matrix', { method: 'PUT', body: JSON.stringify(m) }, token);
}

/** A chat room as callers see it (portal, reader): which networks it reaches, who's in, the last line. */
export interface BBSChatRoom {
	name: string;
	title: string;
	topic: string;
	min_sl: number;
	bridges: string[];
	present: ChatPresence[];
	last_line: ChatLine | null;
}

export function listBBSChatRooms(token: string): Promise<BBSChatRoom[]> {
	return request('/api/bbs/chat/rooms', { method: 'GET' }, token);
}

/** The room's lines after afterId (the newest without); polling keeps the caller in it. */
export function getBBSChatRoom(token: string, room: string, afterId = 0): Promise<ChatRoomState> {
	return request(`/api/bbs/chat/rooms/${encodeURIComponent(room)}?after=${afterId}`, { method: 'GET' }, token);
}

export function bbsChatAction(token: string, room: string, action: 'enter' | 'say' | 'leave', text = ''): Promise<void> {
	// keepalive: a leave still goes out while the page closes.
	return request(`/api/bbs/chat/rooms/${encodeURIComponent(room)}`, { method: 'POST', body: JSON.stringify({ action, text }), keepalive: action === 'leave' }, token);
}

/** The off-site copy of the backups -- never its passwords or keys. */
export interface OffsiteSettings {
	enabled: boolean;
	kind: '' | 'sftp' | 'swift' | 's3' | 'webdav';
	recipient: string;
	keep_daily: number;
	keep_weekly: number;
	sftp_host: string;
	sftp_port: number;
	sftp_user: string;
	sftp_dir: string;
	sftp_has_password: boolean;
	sftp_public_key: string;
	sftp_host_key: string;
	swift_auth_url: string;
	swift_user: string;
	swift_has_password: boolean;
	swift_project: string;
	swift_user_domain: string;
	swift_project_domain: string;
	swift_region: string;
	swift_container: string;
	swift_prefix: string;
	s3_endpoint: string;
	s3_region: string;
	s3_bucket: string;
	s3_access_key: string;
	s3_has_secret_key: boolean;
	s3_prefix: string;
	s3_path_style: boolean;
	webdav_url: string;
	webdav_user: string;
	webdav_has_password: boolean;
	status: { last_ok: string; last_name: string; last_try: string; last_error: string; remote: number };
}

export type OffsiteInput = Omit<
	OffsiteSettings,
	'status' | 'sftp_has_password' | 'swift_has_password' | 'sftp_public_key' | 's3_has_secret_key' | 'webdav_has_password'
> & {
	sftp_password?: string;
	swift_password?: string;
	s3_secret_key?: string;
	webdav_password?: string;
	forget_sftp_key?: boolean;
};

export const getOffsite = (token: string): Promise<OffsiteSettings> => request('/api/backups/offsite', { method: 'GET' }, token);
export const saveOffsite = (token: string, o: OffsiteInput): Promise<OffsiteSettings> =>
	request('/api/backups/offsite', { method: 'PUT', body: JSON.stringify(o) }, token);
export const newOffsiteAgeKey = (token: string): Promise<{ private: string; public: string }> =>
	request('/api/backups/offsite/age-key', { method: 'POST' }, token);
export const newOffsiteSSHKey = (token: string): Promise<OffsiteSettings> => request('/api/backups/offsite/ssh-key', { method: 'POST' }, token);
export const testOffsite = (token: string): Promise<{ ok?: boolean; remote?: string[]; host_key?: string; error?: string }> =>
	request('/api/backups/offsite/test', { method: 'POST' }, token);
export const runOffsite = (token: string): Promise<OffsiteSettings> => request('/api/backups/offsite/run', { method: 'POST' }, token);

export interface Oneliner {
	id: number;
	username: string;
	text: string;
	at: string;
}

export function listOneliners(token: string): Promise<Oneliner[]> {
	return request('/api/oneliners', { method: 'GET' }, token);
}

export function deleteOneliner(token: string, id: number): Promise<void> {
	return request(`/api/oneliners/${id}`, { method: 'DELETE' }, token);
}

export interface RemoteDoor {
	host: string;
	port: number;
	client_user: string;
	server_user: string;
	term_type: string;
}

/** A system in an FTN nodelist (internal/nodelist). */
export interface NodelistEntry {
	network: string;
	zone: number;
	net: number;
	node: number;
	keyword: string;
	name: string;
	location: string;
	sysop: string;
	flags: string;
	address: string;
	host: string;
}

export interface NodelistImport {
	network: string;
	filename: string;
	imported_at: string;
	entries: number;
}

export function searchNodelist(token: string, q: string, network = ''): Promise<{ entries: NodelistEntry[]; imports: NodelistImport[] }> {
	return request(`/api/bbs/nodelist?${new URLSearchParams({ q, network })}`, { method: 'GET' }, token);
}

/** The nodelist's entry for an address; ApiError 404 when none has it. */
export function lookupNodelist(token: string, addr: string): Promise<NodelistEntry> {
	return request(`/api/bbs/nodelist/lookup?${new URLSearchParams({ addr })}`, { method: 'GET' }, token);
}

export function getNodelistStatus(token: string): Promise<NodelistImport[]> {
	return request('/api/nodelists', { method: 'GET' }, token);
}

export function syncNodelists(token: string): Promise<{ imported: number; imports: NodelistImport[] }> {
	return request('/api/nodelists/sync', { method: 'POST' }, token);
}

/** A poll (internal/community) with its results. */
export interface PollOption {
	id: number;
	text: string;
	votes: number;
}

export interface Poll {
	id: number;
	question: string;
	options: PollOption[];
	total: number;
	closed: boolean;
	created_at: string;
	my_vote: number;
}

export function listBBSPolls(token: string): Promise<Poll[]> {
	return request('/api/bbs/polls', { method: 'GET' }, token);
}

export function voteBBSPoll(token: string, pollId: number, optionId: number): Promise<Poll> {
	return request(`/api/bbs/polls/${pollId}/vote`, { method: 'POST', body: JSON.stringify({ option_id: optionId }) }, token);
}

/** A named security level ("20 – Regular user"). */
export interface SecurityLevel {
	level: number;
	name: string;
}

/** The named security levels; custom is false while they're the board's own defaults. */
export interface SecurityLevels {
	levels: SecurityLevel[];
	custom: boolean;
}

export function getSecurityLevels(token: string): Promise<SecurityLevels> {
	return request('/api/security-levels', { method: 'GET' }, token);
}

/** Saves the named levels; an empty list goes back to the board's own. */
export function putSecurityLevels(token: string, levels: SecurityLevel[]): Promise<SecurityLevels> {
	return request('/api/security-levels', { method: 'PUT', body: JSON.stringify({ levels }) }, token);
}

/** A news item as the admin edits it (expires_at: YYYY-MM-DD, through that day; empty: never). */
export interface NewsItem {
	id: number;
	created_at: string;
	author: string;
	title_en: string;
	text_en: string;
	title_de: string;
	text_de: string;
	expires_at: string;
	expired: boolean;
}

export type NewsInput = Pick<NewsItem, 'title_en' | 'text_en' | 'title_de' | 'text_de' | 'expires_at'>;

export function listNews(token: string): Promise<NewsItem[]> {
	return request('/api/news', { method: 'GET' }, token);
}

export function createNews(token: string, n: NewsInput): Promise<NewsItem[]> {
	return request('/api/news', { method: 'POST', body: JSON.stringify(n) }, token);
}

export function updateNews(token: string, id: number, n: NewsInput): Promise<NewsItem[]> {
	return request(`/api/news/${id}`, { method: 'PUT', body: JSON.stringify(n) }, token);
}

export function deleteNews(token: string, id: number): Promise<NewsItem[]> {
	return request(`/api/news/${id}`, { method: 'DELETE' }, token);
}

/** A news item in the reader's language (front page, portal). */
export interface PublicNews {
	id: number;
	created_at: string;
	title: string;
	text: string;
}

/** The latest three news items, in the page's language. */
export function getPublicNews(): Promise<PublicNews[]> {
	return request('/api/public/news', { method: 'GET' });
}

export function listPolls(token: string): Promise<Poll[]> {
	return request('/api/polls', { method: 'GET' }, token);
}

export function createPoll(token: string, question: string, options: string[]): Promise<Poll> {
	return request('/api/polls', { method: 'POST', body: JSON.stringify({ question, options }) }, token);
}

export function closePoll(token: string, id: number, closed: boolean): Promise<void> {
	return request(`/api/polls/${id}/close`, { method: 'POST', body: JSON.stringify({ closed }) }, token);
}

export function deletePoll(token: string, id: number): Promise<void> {
	return request(`/api/polls/${id}`, { method: 'DELETE' }, token);
}

/** An entry in the callers' BBS list. */
export interface BBSListEntry {
	id: number;
	name: string;
	address: string;
	sysop: string;
	software: string;
	description: string;
	added_by_id: number;
	added_by: string;
	updated_at: string;
	/** The online check: zero times ("0001-…") mean not yet. */
	checked_at: string;
	online: boolean;
	last_up_at: string;
}

export type BBSListInput = Pick<BBSListEntry, 'name' | 'address' | 'sysop' | 'software' | 'description'>;

export function listBBSList(token: string): Promise<BBSListEntry[]> {
	return request('/api/bbs/bbslist', { method: 'GET' }, token);
}

export function saveBBSListEntry(token: string, entry: BBSListInput, id?: number): Promise<BBSListEntry> {
	return request(id ? `/api/bbs/bbslist/${id}` : '/api/bbs/bbslist', { method: id ? 'PUT' : 'POST', body: JSON.stringify(entry) }, token);
}

export function deleteBBSListEntry(token: string, id: number): Promise<void> {
	return request(`/api/bbs/bbslist/${id}`, { method: 'DELETE' }, token);
}

export function adminListBBSList(token: string): Promise<BBSListEntry[]> {
	return request('/api/bbslist', { method: 'GET' }, token);
}

export function adminDeleteBBSListEntry(token: string, id: number): Promise<void> {
	return request(`/api/bbslist/${id}`, { method: 'DELETE' }, token);
}

/** The signed-in sysop's two-factor login. */
export interface TOTPStatus {
	enabled: boolean;
	recovery_codes_left: number;
}

export interface TOTPSetup {
	secret: string;
	url: string;
	/** PNG data URL of the QR code. */
	qr: string;
}

export function getTOTP(token: string): Promise<TOTPStatus> {
	return request('/api/account/totp', { method: 'GET' }, token);
}

export function startTOTP(token: string): Promise<TOTPSetup> {
	return request('/api/account/totp/start', { method: 'POST' }, token);
}

export function confirmTOTP(token: string, code: string): Promise<{ recovery_codes: string[] }> {
	return request('/api/account/totp/confirm', { method: 'POST', body: JSON.stringify({ code }) }, token);
}

export function disableTOTP(token: string, code: string): Promise<TOTPStatus> {
	return request('/api/account/totp/disable', { method: 'POST', body: JSON.stringify({ code }) }, token);
}

export function newRecoveryCodes(token: string, code: string): Promise<{ recovery_codes: string[] }> {
	return request('/api/account/totp/recovery', { method: 'POST', body: JSON.stringify({ code }) }, token);
}

export function setUserPassword(token: string, id: number, password: string): Promise<void> {
	return request(`/api/users/${id}/password`, { method: 'PUT', body: JSON.stringify({ password }) }, token);
}

export function resetUserTOTP(token: string, id: number): Promise<void> {
	return request(`/api/users/${id}/totp`, { method: 'DELETE' }, token);
}

/** Something the health monitor sees not working (internal/health). */
export interface HealthProblem {
	key: string;
	title: string;
	detail: string;
	since: string;
}

/** A message search result (subject, text, from, to). */
export interface SearchHit {
	id: number;
	area_id: number;
	area_tag: string;
	area_name: string;
	subject: string;
	from_name: string;
	to_name: string;
	posted_at: string;
	snippet: string;
}

export function searchMessages(token: string, q: string, areaId = 0): Promise<SearchHit[]> {
	const p = new URLSearchParams({ q });
	if (areaId) p.set('area_id', String(areaId));
	return request(`/api/bbs/messages/search?${p}`, { method: 'GET' }, token);
}

export interface DoorBulletin {
	title: string;
	file: string;
	public: boolean;
}

/** A bulletin a door has written, drawn: for the portal and the front page. */
export interface DoorBulletinView {
	door: string;
	title: string;
	updated: string;
	grid: Grid;
	public: boolean;
}

/** Gives a door its template's bulletins (and tells the game to write them). */
export function applyDoorTemplateBulletins(token: string, name: string): Promise<{ doors: Door[] }> {
	return request(`/api/door-bulletins/${encodeURIComponent(name)}`, { method: 'POST' }, token);
}

export function getDoorBulletins(token: string): Promise<DoorBulletinView[]> {
	return request('/api/bbs/door-bulletins', { method: 'GET' }, token);
}

export function getPublicDoorBulletins(): Promise<DoorBulletinView[]> {
	return request('/api/public/door-bulletins', { method: 'GET' });
}

/** Runs a door's daily maintenance now (the BBS service picks it up within half a minute). */
export function runDoorDaily(token: string, name: string): Promise<void> {
	return request(`/api/door-daily/${encodeURIComponent(name)}`, { method: 'POST' }, token);
}

/** A mail server of the email gateway; the password is never sent back. */
export interface MailServer {
	host: string;
	port: number;
	security: 'tls' | 'starttls' | 'none';
	user: string;
	has_password: boolean;
	/** In: a new password; "" keeps the old one. */
	password?: string;
	folder?: string;
}

export interface EmailMail {
	id: number;
	incoming: boolean;
	user: string;
	address: string;
	subject: string;
	at: string;
	status: 'received' | 'queued' | 'sent' | 'failed';
	error?: string;
}

/** The netmail <-> email gateway (System -> Email gateway). */
export interface EmailSettings {
	enabled: boolean;
	domain: string;
	imap: MailServer;
	smtp: MailServer;
	min_sl: number;
	daily_limit: number;
	delete_fetched: boolean;
	deliver_spam: boolean;
	status: {
		last_fetch: string;
		last_fetch_error: string;
		last_send: string;
		last_send_error: string;
		received: number;
		sent: number;
		dropped: number;
	};
	waiting: number;
	failed: number;
	example: string;
	recent: EmailMail[];
}

export function getEmailSettings(token: string): Promise<EmailSettings> {
	return request<EmailSettings>('/api/email', { method: 'GET' }, token);
}

export function saveEmailSettings(token: string, s: EmailSettings): Promise<EmailSettings> {
	return request<EmailSettings>('/api/email', { method: 'PUT', body: JSON.stringify(s) }, token);
}

/** Logs in to both servers with these (unsaved) settings. */
export function testEmailSettings(token: string, s: EmailSettings): Promise<{ ok: boolean; error?: string }> {
	return request<{ ok: boolean; error?: string }>('/api/email/test', { method: 'POST', body: JSON.stringify(s) }, token);
}

/** Fetches the mailbox and sends what's waiting, now. */
export function fetchEmailNow(token: string): Promise<{ ok: boolean }> {
	return request<{ ok: boolean }>('/api/email/fetch', { method: 'POST' }, token);
}
