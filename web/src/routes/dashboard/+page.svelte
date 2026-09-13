<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { getDashboard, ApiError, type Dashboard } from '$lib/api';

	const REFRESH_MS = 5000;

	let dashboard = $state<Dashboard | null>(null);
	let loadError = $state<string | null>(null);
	let now = $state(Date.now());

	let refreshTimer: ReturnType<typeof setInterval> | undefined;
	let clockTimer: ReturnType<typeof setInterval> | undefined;

	async function load() {
		if (!auth.token) return;
		try {
			dashboard = await getDashboard(auth.token);
			loadError = null;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : 'Could not load the dashboard.';
		}
	}

	function connectedFor(connectedAt: string): string {
		const seconds = Math.max(0, Math.floor((now - new Date(connectedAt).getTime()) / 1000));
		const h = Math.floor(seconds / 3600);
		const m = Math.floor((seconds % 3600) / 60);
		const s = seconds % 60;
		if (h > 0) return `${h}h ${m}m`;
		if (m > 0) return `${m}m ${s}s`;
		return `${s}s`;
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/login');
			return;
		}
		await load();
		refreshTimer = setInterval(load, REFRESH_MS);
		clockTimer = setInterval(() => (now = Date.now()), 1000);
	});

	onDestroy(() => {
		if (refreshTimer) clearInterval(refreshTimer);
		if (clockTimer) clearInterval(clockTimer);
	});
</script>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !dashboard}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	<div class="mb-6">
		<h1 class="text-xl font-semibold text-slate-100">{dashboard.bbs_name}</h1>
		<p class="font-mono text-sm text-slate-500">{dashboard.version}</p>
	</div>

	<div class="mb-8 grid grid-cols-2 gap-4 sm:grid-cols-4">
		<div class="rounded border border-slate-800 p-4">
			<div class="text-2xl font-semibold text-cyan-400">{dashboard.nodes.length}</div>
			<div class="text-xs tracking-wide text-slate-500 uppercase">Nodes Online</div>
		</div>
		<div class="rounded border border-slate-800 p-4">
			<div class="text-2xl font-semibold text-slate-100">{dashboard.user_count}</div>
			<div class="text-xs tracking-wide text-slate-500 uppercase">Users</div>
		</div>
		<div class="rounded border border-slate-800 p-4">
			<div class="text-2xl font-semibold text-slate-100">{dashboard.message_area_count}</div>
			<div class="text-xs tracking-wide text-slate-500 uppercase">Message Areas</div>
		</div>
		<div class="rounded border border-slate-800 p-4">
			<div class="text-2xl font-semibold text-slate-100">{dashboard.file_area_count}</div>
			<div class="text-xs tracking-wide text-slate-500 uppercase">File Areas</div>
		</div>
	</div>

	<section class="mb-8 rounded border border-slate-800 p-4">
		<div class="mb-4 flex items-center justify-between">
			<h2 class="text-sm font-semibold tracking-wide text-cyan-400 uppercase">BinkP</h2>
			<a href="/binkp" class="text-xs text-cyan-500 hover:text-cyan-300">Configure &rarr;</a>
		</div>
		{#if dashboard.binkp.own_ftn_addresses.length === 0}
			<p class="text-sm text-slate-500">No FTN address configured yet.</p>
		{:else}
			<p class="mb-4 text-sm text-slate-400">
				This system: <span class="font-mono text-slate-200"
					>{dashboard.binkp.own_ftn_addresses.join(', ')}</span
				>
			</p>
		{/if}
		<div class="grid grid-cols-2 gap-4 sm:grid-cols-4">
			<div class="rounded border border-slate-800 p-3">
				<div class="text-xl font-semibold text-slate-100">{dashboard.binkp.uplink_count}</div>
				<div class="text-xs tracking-wide text-slate-500 uppercase">Uplinks</div>
			</div>
			<div class="rounded border border-slate-800 p-3">
				<div class="text-xl font-semibold text-slate-100">
					{dashboard.binkp.crash_only_uplink_count}
				</div>
				<div class="text-xs tracking-wide text-slate-500 uppercase">Crash-Only</div>
			</div>
			<div class="rounded border border-slate-800 p-3">
				<div class="text-xl font-semibold text-slate-100">{dashboard.binkp.pending_outbound}</div>
				<div class="text-xs tracking-wide text-slate-500 uppercase">Pending Netmail</div>
			</div>
			<div class="rounded border border-slate-800 p-3">
				<div
					class="text-xl font-semibold {dashboard.binkp.pending_crash > 0
						? 'text-amber-400'
						: 'text-slate-100'}"
				>
					{dashboard.binkp.pending_crash}
				</div>
				<div class="text-xs tracking-wide text-slate-500 uppercase">Pending Crash</div>
			</div>
		</div>
	</section>

	<section class="rounded border border-slate-800 p-4">
		<h2 class="mb-4 text-sm font-semibold tracking-wide text-cyan-400 uppercase">Who's Online</h2>
		{#if dashboard.nodes.length === 0}
			<p class="text-sm text-slate-500">No active sessions.</p>
		{:else}
			<div class="overflow-x-auto">
				<table class="w-full text-left text-sm">
					<thead class="text-xs tracking-wide text-slate-500 uppercase">
						<tr class="border-b border-slate-800">
							<th class="py-2 pr-4">Node</th>
							<th class="py-2 pr-4">Handle</th>
							<th class="py-2 pr-4">Terminal</th>
							<th class="py-2 pr-4">Remote</th>
							<th class="py-2">Connected</th>
						</tr>
					</thead>
					<tbody>
						{#each dashboard.nodes as node (node.node)}
							<tr class="border-b border-slate-900">
								<td class="py-2 pr-4 font-mono text-yellow-400">{node.node}</td>
								<td class="py-2 pr-4">{node.username}</td>
								<td class="py-2 pr-4 text-slate-400">{node.term_type}</td>
								<td class="py-2 pr-4 font-mono text-slate-400">{node.remote_ip}</td>
								<td class="py-2 text-slate-400">{connectedFor(node.connected_at)}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>
{/if}
