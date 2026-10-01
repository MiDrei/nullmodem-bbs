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
				await goto('/admin/login');
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
			await goto('/admin/login');
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
		<h1 class="page-title">{dashboard.bbs_name}</h1>
		<p class="font-mono text-sm text-slate-500">{dashboard.version}</p>
	</div>

	{#if dashboard.pending_message_area_count > 0 || dashboard.pending_file_area_count > 0 || dashboard.unresolved_netmail_count > 0 || dashboard.pending_user_count > 0 || dashboard.locked_out_count > 0 || dashboard.paging.length > 0}
		<section class="mb-8 rounded border border-amber-800/60 bg-amber-950/20 p-4">
			<h2 class="mb-3 text-sm font-semibold tracking-wide text-amber-400 uppercase">
				Needs Attention
			</h2>
			<div class="flex flex-col gap-2 text-sm">
				{#each dashboard.paging as room (room)}
					<a href="/admin/chat?room={room}" class="font-semibold text-fuchsia-300 hover:text-fuchsia-200">
						{room.replace(/^page-/, '')} is paging you -- answer in the chat &rarr;
					</a>
				{/each}
				{#if dashboard.pending_user_count > 0}
					<a href="/admin/users" class="text-amber-300 hover:text-amber-200">
						{dashboard.pending_user_count} new user{dashboard.pending_user_count === 1 ? '' : 's'} awaiting approval &rarr;
					</a>
				{/if}
				{#if dashboard.locked_out_count > 0}
					<a href="/admin/security" class="text-amber-300 hover:text-amber-200">
						{dashboard.locked_out_count} address{dashboard.locked_out_count === 1 ? '' : 'es'} locked out for failed logins &rarr;
					</a>
				{/if}
				{#if dashboard.pending_message_area_count > 0}
					<a href="/admin/pending-areas" class="text-amber-300 hover:text-amber-200">
						{dashboard.pending_message_area_count} new message area{dashboard.pending_message_area_count ===
						1
							? ''
							: 's'} awaiting approval &rarr;
					</a>
				{/if}
				{#if dashboard.pending_file_area_count > 0}
					<a href="/admin/pending-areas" class="text-amber-300 hover:text-amber-200">
						{dashboard.pending_file_area_count} new file area{dashboard.pending_file_area_count === 1
							? ''
							: 's'} awaiting approval &rarr;
					</a>
				{/if}
				{#if dashboard.unresolved_netmail_count > 0}
					<a href="/admin/netmail" class="text-amber-300 hover:text-amber-200">
						{dashboard.unresolved_netmail_count} undeliverable netmail message{dashboard.unresolved_netmail_count ===
						1
							? ''
							: 's'} &rarr;
					</a>
				{/if}
			</div>
		</section>
	{/if}

	<div class="mb-8 grid grid-cols-2 gap-4 sm:grid-cols-4">
		<div class="rounded-xl border border-line p-4">
			<div class="text-2xl font-semibold text-cyan-400">{dashboard.nodes.length}</div>
			<div class="card-label">Nodes Online</div>
		</div>
		<div class="rounded-xl border border-line p-4">
			<div class="text-2xl font-semibold text-slate-100">{dashboard.user_count}</div>
			<div class="card-label">Users</div>
		</div>
		<div class="rounded-xl border border-line p-4">
			<div class="text-2xl font-semibold text-slate-100">{dashboard.message_area_count}</div>
			<div class="card-label">Message Areas</div>
		</div>
		<div class="rounded-xl border border-line p-4">
			<div class="text-2xl font-semibold text-slate-100">{dashboard.file_area_count}</div>
			<div class="card-label">File Areas</div>
		</div>
	</div>

	<section class="mb-8 rounded-xl border border-line p-4">
		<div class="mb-4 flex items-center justify-between">
			<h2 class="card-label">BinkP</h2>
			<a href="/admin/binkp/uplinks" class="text-xs text-cyan-500 hover:text-cyan-300"
				>Configure &rarr;</a
			>
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
		<div class="grid grid-cols-2 gap-4 sm:grid-cols-5">
			<div class="rounded-xl border border-line p-3">
				<div class="text-lg font-semibold text-ink-strong">{dashboard.binkp.uplink_count}</div>
				<div class="card-label">Uplinks</div>
			</div>
			<div class="rounded-xl border border-line p-3">
				<div class="text-lg font-semibold text-ink-strong">
					{dashboard.binkp.crash_only_uplink_count}
				</div>
				<div class="card-label">Crash-Only</div>
			</div>
			<div class="rounded-xl border border-line p-3">
				<div class="text-lg font-semibold text-ink-strong">{dashboard.binkp.hold_uplink_count}</div>
				<div class="card-label">Hold</div>
			</div>
			<div class="rounded-xl border border-line p-3">
				<div class="text-lg font-semibold text-ink-strong">{dashboard.binkp.pending_outbound}</div>
				<div class="card-label">Pending Netmail</div>
			</div>
			<div class="rounded-xl border border-line p-3">
				<div
					class="text-xl font-semibold {dashboard.binkp.pending_crash > 0
						? 'text-amber-400'
						: 'text-slate-100'}"
				>
					{dashboard.binkp.pending_crash}
				</div>
				<div class="card-label">Pending Crash</div>
			</div>
		</div>
	</section>

	<section class="rounded-xl border border-line p-4">
		<h2 class="mb-4 card-label">Who's Online</h2>
		{#if dashboard.nodes.length === 0}
			<p class="text-sm text-slate-500">No active sessions.</p>
		{:else}
			<div class="overflow-x-auto">
				<table class="w-full text-left text-sm">
					<thead class="card-label">
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
							<tr class="border-b border-line">
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
