<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { listLogs, ApiError, type LogEntry } from '$lib/api';

	const POLL_MS = 3000;
	const MAX_ENTRIES = 500;

	let entries = $state<LogEntry[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);
	let autoFollow = $state(true);
	let levelFilter = $state<'all' | LogEntry['level']>('all');

	let pollTimer: ReturnType<typeof setInterval> | undefined;
	let logEnd = $state<HTMLDivElement | undefined>();
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
				await goto('/login');
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
				await goto('/login');
				return;
			}
			// Transient poll failures aren't worth surfacing as a hard error.
		}
	}

	$effect(() => {
		// Track entries so this reruns whenever new ones arrive.
		void entries;
		if (autoFollow) logEnd?.scrollIntoView({ block: 'end' });
	});

	onMount(async () => {
		if (!auth.token) {
			await goto('/login');
			return;
		}
		await loadInitial();
		pollTimer = setInterval(poll, POLL_MS);
	});

	onDestroy(() => {
		if (pollTimer) clearInterval(pollTimer);
	});

	function formatTime(iso: string): string {
		return new Date(iso).toLocaleTimeString();
	}

	const levelClasses: Record<LogEntry['level'], string> = {
		info: 'text-slate-300',
		warn: 'text-amber-400',
		error: 'text-red-400'
	};

	const levelBadgeClasses: Record<LogEntry['level'], string> = {
		info: 'bg-slate-800 text-slate-400',
		warn: 'bg-amber-950 text-amber-400',
		error: 'bg-red-950 text-red-400'
	};

	let visibleEntries = $derived(
		levelFilter === 'all' ? entries : entries.filter((e) => e.level === levelFilter)
	);
</script>

<div class="mb-6 flex flex-wrap items-center justify-between gap-3">
	<h1 class="text-xl font-semibold text-slate-100">Logs</h1>
	<div class="flex items-center gap-3 text-sm">
		<select
			bind:value={levelFilter}
			class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
		>
			<option value="all">All levels</option>
			<option value="info">Info</option>
			<option value="warn">Warn</option>
			<option value="error">Error</option>
		</select>
		<label class="flex items-center gap-1.5 text-slate-400">
			<input type="checkbox" bind:checked={autoFollow} class="accent-cyan-500" />
			Follow
		</label>
	</div>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else if visibleEntries.length === 0}
	<p class="text-sm text-slate-500">No log entries yet.</p>
{:else}
	<div class="max-h-[70vh] overflow-y-auto rounded border border-slate-800 bg-slate-950/50 font-mono text-xs">
		{#each visibleEntries as entry (entry.id)}
			<div class="flex gap-3 border-b border-slate-900 px-3 py-1.5 {levelClasses[entry.level]}">
				<span class="shrink-0 text-slate-600">{formatTime(entry.logged_at)}</span>
				<span class="shrink-0 rounded px-1.5 py-0.5 text-[10px] tracking-wide uppercase {levelBadgeClasses[entry.level]}"
					>{entry.level}</span
				>
				<span class="shrink-0 text-slate-500">[{entry.source}]</span>
				<span class="break-all">{entry.message}</span>
			</div>
		{/each}
		<div bind:this={logEnd}></div>
	</div>
{/if}
