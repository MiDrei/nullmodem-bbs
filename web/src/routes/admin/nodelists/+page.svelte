<script lang="ts">
	// The FTN nodelists, as last imported from the networks' file
	// echoes (the mailer checks every 10 minutes for a new one).
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { getNodelistStatus, syncNodelists, ApiError, type NodelistImport } from '$lib/api';

	let imports = $state<NodelistImport[]>([]);
	let loaded = $state(false);
	let syncing = $state(false);

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			imports = await getNodelistStatus(auth.token);
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : 'Could not load the nodelists.', 'error');
		} finally {
			loaded = true;
		}
	});

	async function sync() {
		if (!auth.token) return;
		syncing = true;
		try {
			const res = await syncNodelists(auth.token);
			imports = res.imports;
			toast.push(`${res.imported} nodelist${res.imported === 1 ? '' : 's'} imported.`, 'success');
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : 'The import failed.', 'error');
		} finally {
			syncing = false;
		}
	}
</script>

<div class="mb-6 flex items-start justify-between gap-4">
	<div>
		<h1 class="page-title">Nodelists</h1>
		<p class="page-subtitle max-w-2xl leading-relaxed">
			Each network's directory of systems, taken from its newest nodelist file in the network's file
			areas (FSXNET.Z75 in FSX_NODE, …) -- the mailer looks for a new one every 10 minutes. Used to check
			netmail addresses and for callers to look systems up.
		</p>
	</div>
	<button class="btn-secondary btn-sm shrink-0" disabled={syncing} onclick={sync}>{syncing ? 'Importing…' : 'Import again'}</button>
</div>

{#if loaded && imports.length === 0}
	<p class="text-sm text-muted">None yet -- a nodelist file echo (e.g. FSX_NODE) has to deliver one first.</p>
{:else}
	<div class="overflow-hidden rounded-xl border border-line">
		<table class="w-full text-left text-sm">
			<thead class="card-label">
				<tr class="border-b border-line">
					<th class="p-3">Network</th>
					<th class="p-3">File</th>
					<th class="p-3">Systems</th>
					<th class="p-3">Imported</th>
				</tr>
			</thead>
			<tbody class="divide-y divide-slate-800">
				{#each imports as i (i.network)}
					<tr>
						<td class="p-3 text-ink-strong">{i.network}</td>
						<td class="p-3 font-mono text-xs text-muted">{i.filename}</td>
						<td class="p-3 text-ink">{i.entries}</td>
						<td class="p-3 text-xs text-muted">{new Date(i.imported_at).toLocaleString()}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
