<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import {
		listLogs,
		listBinkpSessions,
		getBinkpSessionTranscript,
		ApiError,
		type LogEntry,
		type BinkpSessionEntry
	} from '$lib/api';

	const POLL_MS = 3000;
	const MAX_ENTRIES = 500;

	type Tab = 'web' | 'telnet' | 'ssh' | 'binkp';
	let activeTab = $state<Tab>('web');

	// ---- Web/Telnet/SSH: applog entries, shared live-polled pool ----
	let entries = $state<LogEntry[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);
	let levelFilter = $state<'all' | LogEntry['level']>('all');
	let pollTimer: ReturnType<typeof setInterval> | undefined;
	let lastID = 0;

	function merge(newEntries: LogEntry[]) {
		if (newEntries.length === 0) return;
		entries = [...entries, ...newEntries].slice(-MAX_ENTRIES);
		lastID = entries[entries.length - 1].id;
	}

	async function loadInitial() {
		if (!auth.token) return;
		try {
			entries = await listLogs(auth.token);
			if (entries.length > 0) lastID = entries[entries.length - 1].id;
			loadError = null;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : 'Could not load logs.';
		} finally {
			loaded = true;
		}
	}

	async function poll() {
		if (!auth.token || !loaded) return;
		try {
			merge(await listLogs(auth.token, lastID));
			loadError = null;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			// Transient poll failures aren't worth surfacing as a hard error.
		}
	}

	function formatTime(iso: string): string {
		return new Date(iso).toLocaleTimeString();
	}

	const levelBadgeClasses: Record<LogEntry['level'], string> = {
		info: 'bg-slate-800 text-slate-400',
		warn: 'bg-amber-950 text-amber-400',
		error: 'bg-red-950 text-red-400'
	};

	// Telnet/SSH share applog's single "bbs" source -- internal/bbs's
	// Server.Handle prefixes every one of that connection's own log
	// lines with "[telnet]"/"[ssh]" specifically so they can be told
	// apart here (see internal/bbs.Conn.Protocol).
	function stripProtocolPrefix(message: string, prefix: string): string {
		return message.startsWith(prefix) ? message.slice(prefix.length).trim() : message;
	}

	let webEntries = $derived(entries.filter((e) => e.source === 'web'));
	let telnetEntries = $derived(
		entries.filter((e) => e.source === 'bbs' && e.message.startsWith('[telnet]'))
	);
	let sshEntries = $derived(
		entries.filter((e) => e.source === 'bbs' && e.message.startsWith('[ssh]'))
	);

	function tabEntries(tab: Tab): LogEntry[] {
		if (tab === 'web') return webEntries;
		if (tab === 'telnet') return telnetEntries;
		if (tab === 'ssh') return sshEntries;
		return [];
	}

	let visibleEntries = $derived.by(() => {
		const base = tabEntries(activeTab);
		return levelFilter === 'all' ? base : base.filter((e) => e.level === levelFilter);
	});

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
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			sessionsError = err instanceof ApiError ? err.message : 'Could not load BinkP sessions.';
		} finally {
			sessionsLoaded = true;
			sessionsLoading = false;
		}
	}

	function formatDateTime(iso: string): string {
		return new Date(iso).toLocaleString();
	}

	// ---- Detail popup: shared by all four tabs, content depends on kind ----
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
		await loadInitial();
		pollTimer = setInterval(poll, POLL_MS);
	});

	onDestroy(() => {
		if (pollTimer) clearInterval(pollTimer);
	});
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="mb-6 flex flex-wrap items-center justify-between gap-3">
	<h1 class="page-title">Logs</h1>
	{#if activeTab !== 'binkp'}
		<select
			bind:value={levelFilter}
			class="field field-sm"
		>
			<option value="all">All levels</option>
			<option value="info">Info</option>
			<option value="warn">Warn</option>
			<option value="error">Error</option>
		</select>
	{:else}
		<button
			type="button"
			class="btn-secondary btn-sm"
			disabled={sessionsLoading}
			onclick={loadSessions}
		>
			{sessionsLoading ? 'Refreshing…' : 'Refresh'}
		</button>
	{/if}
</div>

<div class="mb-4 flex gap-1 border-b border-slate-800">
	{#each [['web', 'Web'], ['telnet', 'Telnet'], ['ssh', 'SSH'], ['binkp', 'BinkP']] as [tab, label] (tab)}
		<button
			type="button"
			class="border-b-2 px-3 py-2 text-sm font-medium transition {activeTab === tab
				? 'border-cyan-400 text-slate-100'
				: 'border-transparent text-slate-500 hover:text-slate-300'}"
			onclick={() => (activeTab = tab as Tab)}
		>
			{label}
		</button>
	{/each}
</div>

{#if activeTab !== 'binkp'}
	{#if loadError}
		<p class="text-sm text-red-400">{loadError}</p>
	{:else if !loaded}
		<p class="text-sm text-slate-400">Loading…</p>
	{:else if visibleEntries.length === 0}
		<p class="text-sm text-slate-500">No log entries yet.</p>
	{:else}
		<div class="overflow-hidden rounded-xl border border-line">
			<table class="w-full text-left text-sm">
				<thead class="bg-slate-900 card-label">
					<tr>
						<th class="px-3 py-2 font-medium">Time</th>
						<th class="px-3 py-2 font-medium">Level</th>
						<th class="px-3 py-2 font-medium">Message</th>
						<th class="px-3 py-2"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-slate-800">
					{#each visibleEntries.slice().reverse() as entry (entry.id)}
						<tr class="hover:bg-slate-900/60">
							<td class="px-3 py-2 font-mono text-xs text-slate-500">{formatTime(entry.logged_at)}</td>
							<td class="px-3 py-2">
								<span
									class="rounded px-1.5 py-0.5 text-[10px] tracking-wide uppercase {levelBadgeClasses[
										entry.level
									]}">{entry.level}</span
								>
							</td>
							<td class="max-w-xl truncate px-3 py-2 font-mono text-xs text-slate-300">
								{activeTab === 'telnet'
									? stripProtocolPrefix(entry.message, '[telnet]')
									: activeTab === 'ssh'
										? stripProtocolPrefix(entry.message, '[ssh]')
										: entry.message}
							</td>
							<td class="px-3 py-2 text-right">
								<button
									type="button"
									class="btn-secondary btn-xs"
									onclick={() => openLogDetail(entry)}
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
