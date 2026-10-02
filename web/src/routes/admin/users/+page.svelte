<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listUsers,
		setUserSecurityLevel,
		approveUser,
		deletePendingUser,
		setUserPassword,
		resetUserTOTP,
		ApiError,
		type BBSUser
	} from '$lib/api';

	interface Row {
		user: BBSUser;
		level: number;
		realName: string;
		saving: boolean;
	}

	let rows = $state<Row[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	function toRows(users: BBSUser[]): Row[] {
		return users.map((user) => ({ user, level: user.security_level, realName: user.real_name, saving: false }));
	}

	async function load() {
		if (!auth.token) return;
		try {
			rows = toRows(await listUsers(auth.token));
			loadError = null;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : 'Could not load users.';
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

	async function save(row: Row) {
		if (!auth.token) return;
		row.saving = true;
		try {
			const updated = await setUserSecurityLevel(auth.token, row.user.id, row.level, row.realName);
			row.user = updated;
			row.level = updated.security_level;
			row.realName = updated.real_name;
			toast.push(`Saved ${updated.username}.`, 'success');
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			toast.push(err instanceof ApiError ? err.message : 'Could not save.', 'error');
		} finally {
			row.saving = false;
		}
	}

	let pending = $derived(rows.filter((r) => !r.user.validated));

	// A new password for a caller who forgot theirs: typed here, told
	// to them however you reach them.
	async function newPassword(row: Row) {
		if (!auth.token) return;
		const pw = prompt(`New password for ${row.user.username} (at least 6 characters):`);
		if (!pw) return;
		try {
			await setUserPassword(auth.token, row.user.id, pw);
			toast.push(`New password set for ${row.user.username}.`, 'success');
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : 'Could not set it.', 'error');
		}
	}

	async function resetTwoFactor(row: Row) {
		if (!auth.token || !confirm(`Turn off two-factor login for ${row.user.username}? They can set it up again afterwards.`)) return;
		try {
			await resetUserTOTP(auth.token, row.user.id);
			row.user = { ...row.user, two_factor: false };
			toast.push(`Two-factor login is off for ${row.user.username}.`, 'success');
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : 'Could not reset it.', 'error');
		}
	}

	async function approve(row: Row) {
		if (!auth.token) return;
		row.saving = true;
		try {
			const updated = await approveUser(auth.token, row.user.id);
			row.user = updated;
			row.level = updated.security_level;
			toast.push(`${updated.username} approved (SL ${updated.security_level}).`, 'success');
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : 'Could not approve.', 'error');
		} finally {
			row.saving = false;
		}
	}

	async function turnDown(row: Row) {
		if (!auth.token || !confirm(`Turn down and delete the account ${row.user.username}?`)) return;
		try {
			await deletePendingUser(auth.token, row.user.id);
			rows = rows.filter((r) => r.user.id !== row.user.id);
			toast.push(`${row.user.username} deleted.`, 'success');
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : 'Could not delete.', 'error');
		}
	}

	function formatDate(iso: string | null): string {
		if (!iso) return 'never';
		return new Date(iso).toLocaleString();
	}

	// Mirrors the backend's user.SLSysop constant (internal/user/user.go).
	const SL_SYSOP = 255;
	function isSysop(level: number): boolean {
		return level >= SL_SYSOP;
	}
</script>

<h1 class="mb-6 page-title">Users</h1>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	{#if pending.length}
		<section class="mb-6 rounded-xl border border-amber-800/60 bg-amber-950/20 p-4">
			<h2 class="mb-1 text-sm font-semibold tracking-wide text-amber-400 uppercase">Awaiting approval</h2>
			<p class="mb-3 text-xs text-muted">
				They can read and write netmail to you; approving gives them the new-user level and lets them post,
				use doors and upload.
			</p>
			<div class="flex flex-col divide-y divide-amber-900/40">
				{#each pending as row (row.user.id)}
					<div class="flex flex-wrap items-center gap-3 py-2">
						<span class="min-w-0 flex-1">
							<span class="text-ink-strong">{row.user.username}</span>
							{#if row.user.real_name}<span class="text-muted"> · {row.user.real_name}</span>{/if}
							<span class="block text-xs text-faint">signed up {formatDate(row.user.created_at)} · {row.user.total_calls} call(s)</span>
						</span>
						<button class="btn-primary btn-sm" disabled={row.saving} onclick={() => approve(row)}>Approve</button>
						<button class="btn-secondary btn-sm" disabled={row.saving} onclick={() => turnDown(row)}>Turn down</button>
					</div>
				{/each}
			</div>
		</section>
	{/if}
	<div class="overflow-x-auto rounded-xl border border-line">
		<table class="w-full text-left text-sm">
			<thead class="card-label">
				<tr class="border-b border-slate-800">
					<th class="p-3">Username</th>
					<th class="p-3">Real Name</th>
					<th class="p-3">Security Level</th>
					<th class="p-3">Total Calls</th>
					<th class="p-3">Last Login</th>
					<th class="p-3">Member Since</th>
					<th class="p-3"></th>
				</tr>
			</thead>
			<tbody>
				{#each rows as row (row.user.id)}
					<tr class="border-b border-line align-top">
						<td class="p-3 text-slate-100">
							<div class="flex items-center gap-1.5">
								{#if isSysop(row.user.security_level)}
									<svg
										viewBox="0 0 20 20"
										fill="currentColor"
										class="h-4 w-4 shrink-0 text-amber-400"
										aria-hidden="true"
									>
										<title>Sysop</title>
										<path
											d="M10 1.6l2.1 4.3 4.7.7-3.4 3.3.8 4.7L10 12.2l-4.2 2.4.8-4.7-3.4-3.3 4.7-.7L10 1.6z"
										/>
									</svg>
								{/if}
								{row.user.username}
								{#if !row.user.validated}<span class="rounded bg-amber-950 px-1.5 py-0.5 text-[10px] text-amber-400 uppercase">waiting</span>{/if}
								{#if row.user.two_factor}<span class="rounded bg-emerald-950 px-1.5 py-0.5 text-[10px] text-emerald-400 uppercase" title="Two-factor login">2FA</span>{/if}
							</div>
						</td>
						<td class="p-3">
							<input
								class="w-40 field field-sm"
								placeholder="—"
								bind:value={row.realName}
							/>
						</td>
						<td class="p-3">
							<input
								type="number"
								min="0"
								max="255"
								class="w-20 rounded border px-2 py-1 focus:outline-none {isSysop(row.level)
									? 'border-amber-700 bg-slate-900 text-amber-400 focus:border-amber-500'
									: 'border-slate-700 bg-slate-900 text-slate-100 focus:border-cyan-500'}"
								bind:value={row.level}
							/>
						</td>
						<td class="p-3 text-slate-400">{row.user.total_calls}</td>
						<td class="p-3 text-slate-400">{formatDate(row.user.last_login_at)}</td>
						<td class="p-3 text-slate-400">{formatDate(row.user.created_at)}</td>
						<td class="flex flex-wrap gap-1.5 p-3">
							<button class="btn-secondary btn-sm" onclick={() => newPassword(row)}>Password…</button>
							{#if row.user.two_factor && row.user.username !== auth.username}
								<button class="btn-secondary btn-sm" onclick={() => resetTwoFactor(row)}>Reset 2FA</button>
							{/if}
							<button
								class="btn-primary btn-sm"
								disabled={row.saving ||
									(row.level === row.user.security_level && row.realName === row.user.real_name)}
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
