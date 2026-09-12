<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { listUsers, setUserSecurityLevel, ApiError, type BBSUser } from '$lib/api';

	interface Row {
		user: BBSUser;
		level: number;
		saving: boolean;
	}

	let rows = $state<Row[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	function toRows(users: BBSUser[]): Row[] {
		return users.map((user) => ({ user, level: user.security_level, saving: false }));
	}

	async function load() {
		if (!auth.token) return;
		try {
			rows = toRows(await listUsers(auth.token));
			loadError = null;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : 'Could not load users.';
		} finally {
			loaded = true;
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/login');
			return;
		}
		await load();
	});

	async function save(row: Row) {
		if (!auth.token) return;
		row.saving = true;
		try {
			const updated = await setUserSecurityLevel(auth.token, row.user.id, row.level);
			row.user = updated;
			row.level = updated.security_level;
			toast.push(`Saved ${updated.username}'s security level.`, 'success');
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/login');
				return;
			}
			toast.push(err instanceof ApiError ? err.message : 'Could not save.', 'error');
		} finally {
			row.saving = false;
		}
	}

	function formatDate(iso: string | null): string {
		if (!iso) return 'never';
		return new Date(iso).toLocaleString();
	}
</script>

<h1 class="mb-6 text-xl font-semibold text-slate-100">Users</h1>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	<div class="overflow-x-auto rounded border border-slate-800">
		<table class="w-full text-left text-sm">
			<thead class="text-xs tracking-wide text-slate-500 uppercase">
				<tr class="border-b border-slate-800">
					<th class="p-3">Username</th>
					<th class="p-3">Security Level</th>
					<th class="p-3">Total Calls</th>
					<th class="p-3">Last Login</th>
					<th class="p-3">Member Since</th>
					<th class="p-3"></th>
				</tr>
			</thead>
			<tbody>
				{#each rows as row (row.user.id)}
					<tr class="border-b border-slate-900 align-top">
						<td class="p-3 text-slate-100">{row.user.username}</td>
						<td class="p-3">
							<input
								type="number"
								min="0"
								max="255"
								class="w-20 rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
								bind:value={row.level}
							/>
						</td>
						<td class="p-3 text-slate-400">{row.user.total_calls}</td>
						<td class="p-3 text-slate-400">{formatDate(row.user.last_login_at)}</td>
						<td class="p-3 text-slate-400">{formatDate(row.user.created_at)}</td>
						<td class="p-3">
							<button
								class="rounded bg-cyan-600 px-3 py-1 text-white hover:bg-cyan-500 disabled:opacity-50"
								disabled={row.saving || row.level === row.user.security_level}
								onclick={() => save(row)}
							>
								{row.saving ? 'Saving…' : 'Save'}
							</button>
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
