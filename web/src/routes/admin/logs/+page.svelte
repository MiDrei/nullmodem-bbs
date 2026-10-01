<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import {
		listLogs,
		listDoors,
		listBinkpSessions,
		getBinkpSessionTranscript,
		ApiError,
		type LogCategory,
		type LogEntry,
		type LogQuery,
		type BinkpSessionEntry
	} from '$lib/api';

	const POLL_MS = 3000;
	const PAGE = 200;
	const TAB_KEY = 'nullmodem.admin.logsTab';

	// The applog categories (internal/applog.Filter), then BinkP's own
	// session records.
	type Tab = 'all' | Exclude<LogCategory, ''> | 'binkp';
	const tabs: [Tab, string][] = [
		['all', 'All'],
		['mailer', 'Mailer'],
		['system', 'System'],
		['telnet', 'Telnet'],
		['ssh', 'SSH'],
		['web', 'Web'],
		['binkp', 'BinkP sessions']
	];
	const tabHints: Partial<Record<Tab, string>> = {
		all: 'Every daemon at once -- with "Warnings & errors", a quick health check.',
		mailer: 'BinkP polls and calls, tossing, Areafix/Filefix, TIC, maintenance, InterBBS Last Callers.',
		system: 'The BBS daemon itself, doors and their background programs.',
		telnet: 'Telnet callers.',
		ssh: 'SSH callers.',
		web: 'The web daemon: logins, admin changes, notifications.'
	};
	let activeTab = $state<Tab>('all');

	// ---- applog entries of the active tab, filtered on the server ----
	type LevelChoice = 'all' | 'problems' | LogEntry['level'];
	let entries = $state<LogEntry[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);
	let loadingOlder = $state(false);
	let moreOlder = $state(false);
	let levelChoice = $state<LevelChoice>('all');
	let search = $state('');
	let appliedSearch = $state('');
	/** System tab: '' everything, '-' the daemon without doors, else a door's name. */
	let door = $state('');
	let doorNames = $state<string[]>([]);
	let pollTimer: ReturnType<typeof setInterval> | undefined;
	let searchTimer: ReturnType<typeof setTimeout> | undefined;
	let generation = 0;
	let shownTab: Tab | null = null;

	function currentQuery(): LogQuery {
		const q: LogQuery = { category: activeTab === 'all' || activeTab === 'binkp' ? '' : activeTab };
		if (levelChoice === 'problems') q.levels = ['warn', 'error'];
		else if (levelChoice !== 'all') q.levels = [levelChoice];
		if (appliedSearch) q.q = appliedSearch;
		if (activeTab === 'system' && door === '-') q.excludePrefixes = doorNames.map((d) => `${d}:`);
		else if (activeTab === 'system' && door) q.prefixes = [`${door}:`];
		return q;
	}

	async function handleAuth(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return true;
		}
		return false;
	}

	async function reload() {
		if (!auth.token || activeTab === 'binkp') return;
		const gen = ++generation;
		loaded = false;
		// Another tab: not its entries while the new ones load.
		if (shownTab !== activeTab) {
			entries = [];
			shownTab = activeTab;
		}
		try {
			const got = await listLogs(auth.token, { ...currentQuery(), limit: PAGE });
			if (gen !== generation) return;
			entries = got;
			moreOlder = got.length === PAGE;
			loadError = null;
		} catch (err) {
			if (await handleAuth(err)) return;
			if (gen === generation) loadError = err instanceof ApiError ? err.message : 'Could not load logs.';
		} finally {
			if (gen === generation) loaded = true;
		}
	}

	async function loadOlder() {
		if (!auth.token || entries.length === 0) return;
		const gen = generation;
		loadingOlder = true;
		try {
			const got = await listLogs(auth.token, { ...currentQuery(), beforeId: entries[0].id, limit: PAGE });
			if (gen !== generation) return;
			entries = [...got, ...entries];
			moreOlder = got.length === PAGE;
		} catch (err) {
			if (await handleAuth(err)) return;
		} finally {
			loadingOlder = false;
		}
	}

	async function poll() {
		if (!auth.token || !loaded || activeTab === 'binkp') return;
		const gen = generation;
		const last = entries.length ? entries[entries.length - 1].id : 0;
		try {
			const got = await listLogs(auth.token, last ? { ...currentQuery(), afterId: last } : { ...currentQuery(), limit: PAGE });
			if (gen !== generation || got.length === 0) return;
			entries = last ? [...entries, ...got] : got;
		} catch (err) {
			// Transient poll failures aren't worth surfacing as a hard error.
			await handleAuth(err);
		}
	}

	// Another tab or filter: load afresh.
	$effect(() => {
		activeTab;
		levelChoice;
		appliedSearch;
		door;
		reload();
	});

	$effect(() => {
		const v = search.trim();
		clearTimeout(searchTimer);
		searchTimer = setTimeout(() => (appliedSearch = v), 350);
	});

	function selectTab(t: Tab) {
		activeTab = t;
		try {
			localStorage.setItem(TAB_KEY, t);
		} catch {
			// Not remembered; fine.
		}
	}

	function formatTime(iso: string): string {
		const d = new Date(iso);
		const today = d.toDateString() === new Date().toDateString();
		return today ? d.toLocaleTimeString() : d.toLocaleString([], { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' });
	}

	const levelBadgeClasses: Record<LogEntry['level'], string> = {
		info: 'bg-slate-800 text-slate-400',
		warn: 'bg-amber-950 text-amber-400',
		error: 'bg-red-950 text-red-400'
	};

	/** Which tab an entry belongs to, for the All tab's source column. */
	function categoryOf(e: LogEntry): string {
		if (e.source === 'bbs') {
			if (e.message.startsWith('[telnet]')) return 'Telnet';
			if (e.message.startsWith('[ssh]')) return 'SSH';
			return 'System';
		}
		return e.source === 'mailer' ? 'Mailer' : e.source === 'web' ? 'Web' : e.source;
	}

	function shownMessage(e: LogEntry): string {
		if (activeTab === 'telnet') return e.message.replace(/^\[telnet\]\s*/, '');
		if (activeTab === 'ssh') return e.message.replace(/^\[ssh\]\s*/, '');
		return e.message;
	}

	// ---- BinkP: a genuinely different data source (internal/binkplog) ----
	let sessions = $state<BinkpSessionEntry[]>([]);
	let sessionsError = $state<string | null>(null);
	let sessionsLoaded = $state(false);
	let sessionsLoading = $state(false);

	async function loadSessions() {
		if (!auth.token) return;
		sessionsLoading = true;
		try {
			sessions = await listBinkpSessions(auth.token);
			sessionsError = null;
		} catch (err) {
			if (await handleAuth(err)) return;
			sessionsError = err instanceof ApiError ? err.message : 'Could not load BinkP sessions.';
		} finally {
			sessionsLoaded = true;
			sessionsLoading = false;
		}
	}

	function formatDateTime(iso: string): string {
		return new Date(iso).toLocaleString();
	}

	// ---- Detail popup: log entries and BinkP sessions ----
	let detailLog = $state<LogEntry | null>(null);
	let detailSession = $state<BinkpSessionEntry | null>(null);
	let transcript = $state<string | null>(null);
	let transcriptError = $state<string | null>(null);
	let transcriptLoading = $state(false);

	function openLogDetail(e: LogEntry) {
		detailLog = e;
	}

	async function openSessionDetail(e: BinkpSessionEntry) {
		detailSession = e;
		transcript = null;
		transcriptError = null;
		if (!auth.token) return;
		transcriptLoading = true;
		try {
			transcript = await getBinkpSessionTranscript(auth.token, e.id);
		} catch (err) {
			transcriptError = err instanceof ApiError ? err.message : 'Could not load transcript.';
		} finally {
			transcriptLoading = false;
		}
	}

	function closeDetail() {
		detailLog = null;
		detailSession = null;
		transcript = null;
		transcriptError = null;
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && (detailLog || detailSession)) closeDetail();
	}

	$effect(() => {
		if (activeTab === 'binkp' && !sessionsLoaded && !sessionsLoading) {
			loadSessions();
		}
	});

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			const t = localStorage.getItem(TAB_KEY) as Tab | null;
			if (t && tabs.some(([k]) => k === t)) activeTab = t;
		} catch {
			// Default tab then.
		}
		listDoors(auth.token)
			.then((d) => (doorNames = d.doors.map((x) => x.name)))
			.catch(() => {});
		pollTimer = setInterval(poll, POLL_MS);
	});

	onDestroy(() => {
		if (pollTimer) clearInterval(pollTimer);
		clearTimeout(searchTimer);
	});
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="mb-6 flex flex-wrap items-center justify-between gap-3">
	<h1 class="page-title">Logs</h1>
	{#if activeTab === 'binkp'}
		<button type="button" class="btn-secondary btn-sm" disabled={sessionsLoading} onclick={loadSessions}>
			{sessionsLoading ? 'Refreshing…' : 'Refresh'}
		</button>
	{/if}
</div>

<div class="log-tabs mb-3 flex flex-wrap gap-1 border-b border-slate-800">
	{#each tabs as [tab, label] (tab)}
		<button
			type="button"
			class="border-b-2 px-3 py-2 text-sm font-medium transition {activeTab === tab
				? 'border-cyan-400 text-slate-100'
				: 'border-transparent text-slate-500 hover:text-slate-300'}"
			onclick={() => selectTab(tab)}
		>
			{label}
		</button>
	{/each}
</div>

{#if activeTab !== 'binkp'}
	<div class="mb-4 flex flex-wrap items-center gap-2">
		<select bind:value={levelChoice} class="field field-sm">
			<option value="all">All levels</option>
			<option value="problems">Warnings &amp; errors</option>
			<option value="info">Info</option>
			<option value="warn">Warn</option>
			<option value="error">Error</option>
		</select>
		{#if activeTab === 'system'}
			<select bind:value={door} class="field field-sm">
				<option value="">Daemon and doors</option>
				<option value="-">Daemon only</option>
				{#each doorNames as d (d)}
					<option value={d}>{d}</option>
				{/each}
			</select>
		{/if}
		<input type="search" class="field field-sm w-56" placeholder="Search…" bind:value={search} />
		{#if tabHints[activeTab]}<span class="text-xs text-faint">{tabHints[activeTab]}</span>{/if}
	</div>

	{#if loadError}
		<p class="text-sm text-red-400">{loadError}</p>
	{:else if !loaded && entries.length === 0}
		<p class="text-sm text-slate-400">Loading…</p>
	{:else if entries.length === 0}
		<p class="text-sm text-slate-500">No log entries{levelChoice !== 'all' || appliedSearch || door ? ' matching the filter' : ' yet'}.</p>
	{:else}
		<div class="overflow-hidden rounded-xl border border-line">
			<table class="w-full text-left text-sm">
				<thead class="bg-slate-900 card-label">
					<tr>
						<th class="px-3 py-2 font-medium">Time</th>
						<th class="px-3 py-2 font-medium">Level</th>
						{#if activeTab === 'all'}<th class="px-3 py-2 font-medium">Source</th>{/if}
						<th class="px-3 py-2 font-medium">Message</th>
						<th class="px-3 py-2"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-slate-800">
					{#each entries.slice().reverse() as entry (entry.id)}
						<tr class="hover:bg-slate-900/60">
							<td class="px-3 py-2 font-mono text-xs whitespace-nowrap text-slate-500">{formatTime(entry.logged_at)}</td>
							<td class="px-3 py-2">
								<span class="rounded px-1.5 py-0.5 text-[10px] tracking-wide uppercase {levelBadgeClasses[entry.level]}"
									>{entry.level}</span
								>
							</td>
							{#if activeTab === 'all'}<td class="px-3 py-2 text-xs text-muted">{categoryOf(entry)}</td>{/if}
							<td class="max-w-xl truncate px-3 py-2 font-mono text-xs text-slate-300">{shownMessage(entry)}</td>
							<td class="px-3 py-2 text-right">
								<button type="button" class="btn-secondary btn-xs" onclick={() => openLogDetail(entry)}>Detail</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		{#if moreOlder}
			<div class="mt-3 flex justify-center">
				<button type="button" class="btn-secondary btn-sm" disabled={loadingOlder} onclick={loadOlder}>
					{loadingOlder ? 'Loading…' : 'Load older entries'}
				</button>
			</div>
		{/if}
	{/if}
{:else if sessionsError}
	<p class="text-sm text-red-400">{sessionsError}</p>
{:else if !sessionsLoaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else if sessions.length === 0}
	<p class="text-sm text-slate-500">No BinkP sessions recorded yet.</p>
{:else}
	<div class="overflow-hidden rounded-xl border border-line">
		<table class="w-full text-left text-sm">
			<thead class="bg-slate-900 card-label">
				<tr>
					<th class="px-3 py-2 font-medium">Time</th>
					<th class="px-3 py-2 font-medium">Direction</th>
					<th class="px-3 py-2 font-medium">Peer Address</th>
					<th class="px-3 py-2 font-medium">Peer Host</th>
					<th class="px-3 py-2 font-medium">Outcome</th>
					<th class="px-3 py-2"></th>
				</tr>
			</thead>
			<tbody class="divide-y divide-slate-800">
				{#each sessions as s (s.id)}
					<tr class="hover:bg-slate-900/60">
						<td class="px-3 py-2 font-mono text-xs text-slate-500">{formatDateTime(s.started_at)}</td>
						<td class="px-3 py-2 text-xs text-slate-300">
							{s.direction === 'outbound' ? '→ out' : '← in'}
						</td>
						<td class="px-3 py-2 font-mono text-xs text-slate-300">{s.peer_address || '—'}</td>
						<td class="px-3 py-2 font-mono text-xs text-slate-500">{s.peer_host || '—'}</td>
						<td class="px-3 py-2">
							<span
								class="rounded px-1.5 py-0.5 text-[10px] tracking-wide uppercase {s.outcome === 'ok'
									? 'bg-slate-800 text-slate-400'
									: 'bg-red-950 text-red-400'}">{s.outcome}</span
							>
						</td>
						<td class="px-3 py-2 text-right">
							<button
								type="button"
								class="btn-secondary btn-xs"
								onclick={() => openSessionDetail(s)}
							>
								Detail
							</button>
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

{#if detailLog || detailSession}
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4" onclick={closeDetail}>
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			class="max-h-[85vh] w-full max-w-3xl overflow-auto rounded-2xl border border-line-strong bg-surface p-6 shadow-2xl shadow-black/50"
			onclick={(e) => e.stopPropagation()}
			role="dialog"
			aria-modal="true"
			tabindex="-1"
		>
			<div class="mb-4 flex items-center justify-between gap-4">
				<h2 class="card-label">
					{detailSession ? 'BinkP Session' : 'Log Entry'}
				</h2>
				<button
					type="button"
					class="shrink-0 rounded-full border border-slate-700 px-2.5 py-1 text-xs text-slate-300 hover:bg-slate-800"
					onclick={closeDetail}
				>
					Close
				</button>
			</div>

			{#if detailLog}
				<dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
					<dt class="text-slate-500">Time</dt>
					<dd class="font-mono text-slate-300">{formatDateTime(detailLog.logged_at)}</dd>
					<dt class="text-slate-500">Level</dt>
					<dd>
						<span class="rounded px-1.5 py-0.5 text-[10px] tracking-wide uppercase {levelBadgeClasses[detailLog.level]}"
							>{detailLog.level}</span
						>
					</dd>
					<dt class="text-slate-500">Source</dt>
					<dd class="font-mono text-slate-300">{detailLog.source}</dd>
				</dl>
				<pre class="mt-3 overflow-x-auto rounded-xl border border-line bg-sunken p-3 font-mono text-xs whitespace-pre-wrap text-ink-soft">{detailLog.message}</pre>
			{:else if detailSession}
				<dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
					<dt class="text-slate-500">Time</dt>
					<dd class="font-mono text-slate-300">{formatDateTime(detailSession.started_at)}</dd>
					<dt class="text-slate-500">Direction</dt>
					<dd class="text-slate-300">{detailSession.direction}</dd>
					<dt class="text-slate-500">Peer Address</dt>
					<dd class="font-mono text-slate-300">{detailSession.peer_address || '—'}</dd>
					<dt class="text-slate-500">Peer Host</dt>
					<dd class="font-mono text-slate-300">{detailSession.peer_host || '—'}</dd>
					<dt class="text-slate-500">Outcome</dt>
					<dd class="text-slate-300">
						{detailSession.outcome}{detailSession.detail ? ` — ${detailSession.detail}` : ''}
					</dd>
				</dl>
				{#if transcriptLoading}
					<p class="mt-3 text-sm text-slate-400">Loading transcript…</p>
				{:else if transcriptError}
					<p class="mt-3 text-sm text-red-400">{transcriptError}</p>
				{:else if transcript}
					<pre class="mt-3 max-h-[50vh] overflow-auto rounded-xl border border-line bg-sunken p-3 font-mono text-xs whitespace-pre-wrap text-ink-soft">{transcript}</pre>
				{/if}
			{/if}
		</div>
	</div>
{/if}
