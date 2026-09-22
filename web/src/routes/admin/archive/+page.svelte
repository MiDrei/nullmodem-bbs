<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listArchive,
		downloadArchiveEntry,
		previewArchiveEntry,
		deleteArchiveEntry,
		retossArchiveEntries,
		ApiError,
		type ArchiveEntry
	} from '$lib/api';

	const PAGE_SIZE = 50;

	let entries = $state<ArchiveEntry[]>([]);
	let total = $state(0);
	let offset = $state(0);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let selected = $state<Set<number>>(new Set());
	let expandedID = $state<number | null>(null);
	let previewText = $state<string | null>(null);
	let previewError = $state<string | null>(null);
	let busyID = $state<number | null>(null);
	let retossing = $state(false);

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return true;
		}
		return false;
	}

	async function load() {
		if (!auth.token) return;
		try {
			const res = await listArchive(auth.token, PAGE_SIZE, offset);
			entries = res.entries;
			total = res.total;
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load the archive.';
		} finally {
			loaded = true;
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		await load();
	});

	function toggleSelected(id: number) {
		const next = new Set(selected);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		selected = next;
	}

	async function toggleExpand(entry: ArchiveEntry) {
		if (expandedID === entry.id) {
			expandedID = null;
			return;
		}
		if (!auth.token) return;
		expandedID = entry.id;
		previewText = null;
		previewError = null;
		try {
			previewText = await previewArchiveEntry(auth.token, entry.id);
		} catch (err) {
			if (await handleAuthError(err)) return;
			previewError = err instanceof ApiError ? err.message : 'Could not load preview.';
		}
	}

	async function download(entry: ArchiveEntry) {
		if (!auth.token) return;
		try {
			await downloadArchiveEntry(auth.token, entry.id, entry.filename);
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Download failed.', 'error');
		}
	}

	async function remove(entry: ArchiveEntry) {
		if (!auth.token) return;
		if (!confirm(`Delete archived "${entry.filename}"? This can't be undone.`)) return;
		busyID = entry.id;
		try {
			await deleteArchiveEntry(auth.token, entry.id);
			entries = entries.filter((e) => e.id !== entry.id);
			total -= 1;
			const next = new Set(selected);
			next.delete(entry.id);
			selected = next;
			if (expandedID === entry.id) expandedID = null;
			toast.push('Entry deleted.', 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not delete entry.', 'error');
		} finally {
			busyID = null;
		}
	}

	async function retossSelected() {
		if (!auth.token || selected.size === 0) return;
		retossing = true;
		try {
			const res = await retossArchiveEntries(auth.token, [...selected]);
			const skipped = res.skipped_files?.length
				? `, skipped: ${res.skipped_files.join(', ')}`
				: '';
			toast.push(
				`Re-toss done: ${res.received} netmail, ${res.received_echo} echomail, ${res.received_files} file(s)${skipped}`,
				'success'
			);
			selected = new Set();
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Re-toss failed.', 'error');
		} finally {
			retossing = false;
		}
	}

	function outcomeClass(outcome: string): string {
		if (outcome === 'error') return 'bg-red-950 text-red-400';
		if (outcome === 'skipped') return 'bg-amber-950 text-amber-400';
		return 'bg-emerald-950 text-emerald-400';
	}

	async function prevPage() {
		offset = Math.max(0, offset - PAGE_SIZE);
		await load();
	}
	async function nextPage() {
		offset += PAGE_SIZE;
		await load();
	}
</script>

<h1 class="mb-2 text-xl font-semibold text-slate-100">Packet Analyzer</h1>
<p class="mb-6 text-sm text-slate-400">
	Every inbound BinkP file (a packet, a TIC descriptor, a file-echo payload, or anything
	unsupported) this system has received recently, kept for a few days regardless of whether it
	tossed successfully -- for inspecting something unclear, and re-tossing it if needed. Select a
	TIC descriptor together with its payload to re-toss them as a correlated pair, the same as their
	original session would have.
</p>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	<div class="mb-4 flex items-center justify-between">
		<button
			class="rounded bg-cyan-600 px-3 py-1.5 text-sm text-white hover:bg-cyan-500 disabled:opacity-50"
			disabled={selected.size === 0 || retossing}
			onclick={retossSelected}
		>
			{retossing ? 'Re-tossing…' : `Re-toss selected (${selected.size})`}
		</button>
		<div class="flex items-center gap-2 text-sm text-slate-400">
			<span>{total === 0 ? 0 : offset + 1}-{Math.min(offset + PAGE_SIZE, total)} of {total}</span>
			<button
				class="rounded border border-slate-700 px-2 py-1 hover:bg-slate-800 disabled:opacity-40"
				disabled={offset === 0}
				onclick={prevPage}
			>
				&larr; Prev
			</button>
			<button
				class="rounded border border-slate-700 px-2 py-1 hover:bg-slate-800 disabled:opacity-40"
				disabled={offset + PAGE_SIZE >= total}
				onclick={nextPage}
			>
				Next &rarr;
			</button>
		</div>
	</div>

	{#if entries.length === 0}
		<p class="text-sm text-slate-500">Nothing captured yet.</p>
	{:else}
		<div class="flex flex-col gap-2">
			{#each entries as entry (entry.id)}
				<div class="rounded border border-slate-800 p-3">
					<div class="flex items-start gap-3">
						<input
							type="checkbox"
							class="mt-1"
							checked={selected.has(entry.id)}
							onchange={() => toggleSelected(entry.id)}
						/>
						<button
							type="button"
							class="min-w-0 flex-1 text-left"
							onclick={() => toggleExpand(entry)}
						>
							<div class="flex flex-wrap items-center gap-2">
								<span class="truncate font-mono text-sm text-slate-100">{entry.filename}</span>
								<span
									class="rounded px-1.5 py-0.5 text-xs font-semibold uppercase {outcomeClass(
										entry.outcome
									)}"
								>
									{entry.outcome}
								</span>
							</div>
							<div class="mt-0.5 text-xs text-slate-500">
								{entry.uplink_address}
								{#if entry.uplink_host}
									<span class="font-mono">({entry.uplink_host})</span>
								{/if}
								&middot; {entry.size_human} &middot; {new Date(entry.received_at).toLocaleString()}
								{#if entry.detail}
									&middot; <span class="text-red-400">{entry.detail}</span>
								{/if}
							</div>
						</button>
						<div class="flex shrink-0 gap-2">
							<button
								class="rounded border border-slate-700 px-2 py-1 text-xs hover:bg-slate-800"
								onclick={() => download(entry)}
							>
								Download
							</button>
							<button
								class="rounded border border-red-800 px-2 py-1 text-xs text-red-400 hover:bg-red-950 disabled:opacity-50"
								disabled={busyID === entry.id}
								onclick={() => remove(entry)}
							>
								Delete
							</button>
						</div>
					</div>

					{#if expandedID === entry.id}
						<div class="mt-3 border-t border-slate-800 pt-3">
							{#if previewError}
								<p class="text-sm text-red-400">{previewError}</p>
							{:else if previewText === null}
								<p class="text-sm text-slate-400">Loading…</p>
							{:else}
								<pre class="max-h-96 overflow-auto rounded bg-slate-950 p-3 font-mono text-xs break-all whitespace-pre-wrap text-slate-300">{previewText}</pre>
							{/if}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
{/if}
