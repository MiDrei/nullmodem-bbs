<script lang="ts">
	// Statistics: calls, messages, doors, downloads and network traffic
	// over a chosen span -- the front page shows the last 30 days of
	// the public part.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { getStats, ApiError, type StatsReport } from '$lib/api';
	import StatsBoard from '$lib/stats/StatsBoard.svelte';

	let days = $state(30);
	let report = $state<StatsReport | null>(null);
	let loadError = $state<string | null>(null);

	async function load() {
		if (!auth.token) return;
		try {
			report = await getStats(auth.token, days);
			loadError = null;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : 'Could not load the statistics.';
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		await load();
	});

	function pick(d: number) {
		days = d;
		load();
	}
</script>

<div class="mb-5 flex flex-wrap items-end justify-between gap-4">
	<div>
		<h1 class="page-title">Statistics</h1>
		<p class="page-subtitle">Calls, messages, doors and traffic. The front page shows the last 30 days, without the sysop's part.</p>
	</div>
	<div class="flex gap-1">
		{#each [7, 30, 90, 365] as d (d)}
			<button class="pill {days === d ? 'pill-active' : ''}" onclick={() => pick(d)}>{d === 365 ? '1 year' : `${d} days`}</button>
		{/each}
	</div>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !report}
	<p class="text-sm text-muted">Loading…</p>
{:else}
	<StatsBoard r={report} full />
	<p class="mt-4 text-xs text-faint">
		Calls are counted since this version (and from the login lines still in the log); a web or reader login counts once per half hour.
	</p>
{/if}
