<script lang="ts">
	// Who was on which board lately: the InterBBS Last Callers list the
	// network's boards post to their data echo (see internal/lastcallers).
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { listLastCallers, ApiError, type LastCaller } from '$lib/api';

	let callers = $state<LastCaller[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	onMount(async () => {
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		try {
			callers = await listLastCallers(bbsAuth.token);
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				bbsAuth.clear();
				await goto('/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : 'Could not load the last callers.';
		} finally {
			loaded = true;
		}
	});
</script>

<div class="mb-6">
	<h1 class="page-title">Last Callers</h1>
	<p class="page-subtitle">Who was on which board of the network lately, newest first.</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-muted">Loading…</p>
{:else if callers.length === 0}
	<p class="text-sm text-muted">No calls yet.</p>
{:else}
	<div class="overflow-x-auto">
		<table class="w-full min-w-[40rem] text-left text-[13px]">
			<thead class="card-label">
				<tr>
					<th class="pr-3 pb-2 font-normal">Caller</th>
					<th class="pr-3 pb-2 font-normal">BBS</th>
					<th class="pr-3 pb-2 font-normal">When</th>
					<th class="pr-3 pb-2 font-normal">From</th>
					<th class="pb-2 font-normal">Address</th>
				</tr>
			</thead>
			<tbody>
				{#each callers as c, i (i)}
					<tr class="border-t border-line">
						<td class="py-2 pr-3 font-medium text-ink-strong">{c.alias}</td>
						<td class="py-2 pr-3 text-ink">{c.bbs}</td>
						<td class="py-2 pr-3 font-mono text-xs whitespace-nowrap text-muted">{c.date} {c.time}</td>
						<td class="py-2 pr-3 text-muted">{c.location}</td>
						<td class="py-2 font-mono text-xs text-faint">{c.address}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
	<p class="mt-4 text-xs text-faint">Times are each board's own clock.</p>
{/if}
