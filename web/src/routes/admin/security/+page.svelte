<script lang="ts">
	// Login protection and new-user approval: the settings, the
	// addresses locked out now, the allow and block lists, and the
	// latest failed logins.
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		getSecurity,
		putSecuritySettings,
		unlockIP,
		addIPRule,
		deleteIPRule,
		ApiError,
		type SecuritySettings,
		type SecurityState
	} from '$lib/api';

	let sec = $state<SecurityState | null>(null);
	let settings = $state<SecuritySettings | null>(null);
	let handles = $state('');
	let loadError = $state<string | null>(null);
	let saving = $state(false);
	let rulePattern = $state('');
	let ruleKind = $state<'allow' | 'block'>('allow');
	let ruleNote = $state('');
	let timer: ReturnType<typeof setInterval> | undefined;

	async function failed(err: unknown, fallback: string) {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return;
		}
		toast.push(err instanceof ApiError ? err.message : fallback, 'error');
	}

	function apply(st: SecurityState, withSettings = true) {
		sec = st;
		if (withSettings) {
			settings = st.settings;
			handles = st.settings.blocked_handles.join(', ');
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			apply(await getSecurity(auth.token));
		} catch (err) {
			loadError = err instanceof ApiError ? err.message : 'Could not load the security settings.';
		}
		// Lockouts and failures as they happen; the form stays as edited.
		timer = setInterval(async () => {
			if (!auth.token) return;
			try {
				apply(await getSecurity(auth.token), false);
			} catch {
				// Next time.
			}
		}, 15000);
	});
	onDestroy(() => clearInterval(timer));

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!auth.token || !settings) return;
		saving = true;
		try {
			const s = $state.snapshot(settings) as SecuritySettings;
			s.blocked_handles = handles.split(/[,\n]/).map((h) => h.trim()).filter(Boolean);
			apply(await putSecuritySettings(auth.token, s));
			toast.push('Saved -- in effect within half a minute.', 'success');
		} catch (err) {
			await failed(err, 'Could not save.');
		} finally {
			saving = false;
		}
	}

	async function unlock(ip: string) {
		if (!auth.token) return;
		try {
			apply(await unlockIP(auth.token, ip), false);
			toast.push(`${ip} unlocked.`, 'success');
		} catch (err) {
			await failed(err, 'Could not unlock.');
		}
	}

	async function addRule(e?: SubmitEvent, pattern = rulePattern, kind = ruleKind, note = ruleNote) {
		e?.preventDefault();
		if (!auth.token || !pattern.trim()) return;
		try {
			apply(await addIPRule(auth.token, pattern, kind, note), false);
			rulePattern = '';
			ruleNote = '';
			toast.push(`${pattern} is on the ${kind} list.`, 'success');
		} catch (err) {
			await failed(err, 'Could not add it.');
		}
	}

	async function removeRule(pattern: string) {
		if (!auth.token || !confirm(`Take ${pattern} off the list?`)) return;
		try {
			apply(await deleteIPRule(auth.token, pattern), false);
		} catch (err) {
			await failed(err, 'Could not remove it.');
		}
	}

	const when = (iso: string) => new Date(iso).toLocaleString([], { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
	let youAllowed = $derived(sec?.rules.some((r) => r.kind === 'allow' && r.pattern === sec?.your_ip) ?? false);
</script>

<div class="mb-6">
	<h1 class="page-title">Security</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">
		Addresses that keep failing to log in -- on Telnet, SSH, the portal or here -- are locked out for a
		while, longer each time. New accounts can wait for your approval before they post.
	</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !sec || !settings}
	<p class="text-sm text-muted">Loading…</p>
{:else}
	<div class="flex flex-col gap-4">
		{#if !youAllowed}
			<p class="rounded-xl border border-line bg-sunken px-4 py-3 text-sm text-muted">
				You're connected from <span class="font-mono text-ink">{sec.your_ip}</span>.
				<button class="ml-1 text-accent hover:underline" onclick={() => addRule(undefined, sec!.your_ip, 'allow', 'sysop')}>
					Put it on the allow list
				</button>
				so you can never lock yourself out (worth it with a fixed address).
			</p>
		{/if}

		<form class="card flex flex-col gap-4" onsubmit={save}>
			<h2 class="card-label">Login protection</h2>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={settings.lockout_enabled} />
				<span class="text-ink">Lock out addresses that keep failing to log in</span>
			</label>
			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">Failed logins</span>
					<input type="number" min="1" class="field field-sm" bind:value={settings.max_failures} disabled={!settings.lockout_enabled} />
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">within minutes</span>
					<input type="number" min="1" class="field field-sm" bind:value={settings.window_minutes} disabled={!settings.lockout_enabled} />
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">Locked out for minutes</span>
					<input type="number" min="1" class="field field-sm" bind:value={settings.lockout_minutes} disabled={!settings.lockout_enabled} />
					<span class="text-[11px] text-faint">×4 each time again within a day</span>
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">at most hours</span>
					<input type="number" min="1" class="field field-sm" bind:value={settings.max_lockout_hours} disabled={!settings.lockout_enabled} />
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">Telnet/SSH connections per address</span>
					<input type="number" min="0" class="field field-sm" bind:value={settings.max_connections_per_ip} />
					<span class="text-[11px] text-faint">at the same time; 0 no limit</span>
				</label>
			</div>

			<h2 class="card-label mt-2">New users</h2>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={settings.approve_new_users} />
				<span class="text-ink">New accounts wait for my approval</span>
			</label>
			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">Waiting at SL</span>
					<input type="number" min="0" max="254" class="field field-sm" bind:value={settings.pending_sl} disabled={!settings.approve_new_users} />
					<span class="text-[11px] text-faint">They read, and write netmail to you.</span>
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">New users get SL</span>
					<input type="number" min="1" max="254" class="field field-sm" bind:value={settings.new_user_sl} />
					<span class="text-[11px] text-faint">On approval, or at once without it.</span>
				</label>
				<label class="flex flex-col gap-1 sm:col-span-2">
					<span class="text-xs text-muted">Blocked handles</span>
					<input class="field field-sm" placeholder="e.g. guest2, test" bind:value={handles} />
					<span class="text-[11px] text-faint">Comma separated, besides the built-in ones (sysop, admin, root, …).</span>
				</label>
			</div>
			<div class="flex justify-end">
				<button type="submit" class="btn-primary btn-sm" disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
			</div>
		</form>

		<section class="card">
			<h2 class="card-label mb-3">Locked out now</h2>
			{#if sec.lockouts.length === 0}
				<p class="text-sm text-muted">Nobody.</p>
			{:else}
				<table class="w-full text-left text-sm">
					<tbody class="divide-y divide-slate-800">
						{#each sec.lockouts as l (l.ip)}
							<tr>
								<td class="py-2 font-mono text-ink">{l.ip}</td>
								<td class="py-2 text-xs text-muted">
									until {when(l.until)}{l.strikes > 1 ? ` · ${l.strikes}. time` : ''}
									<span class="block text-faint">{l.reason}</span>
								</td>
								<td class="py-2 text-right whitespace-nowrap">
									<button class="btn-secondary btn-xs" onclick={() => unlock(l.ip)}>Unlock</button>
									<button class="btn-secondary btn-xs" onclick={() => addRule(undefined, l.ip, 'block', 'locked out repeatedly')}>Block for good</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		</section>

		<section class="card">
			<h2 class="card-label mb-1">Allow and block lists</h2>
			<p class="mb-3 text-xs text-muted">
				An IP or a range (203.0.113.0/24). Allowed: never locked out. Blocked: turned away at once, everywhere
				-- not BinkP, which has its own passwords.
			</p>
			<form class="mb-3 flex flex-wrap items-center gap-2" onsubmit={(e) => addRule(e)}>
				<select class="field field-sm w-28" bind:value={ruleKind}>
					<option value="allow">Allow</option>
					<option value="block">Block</option>
				</select>
				<input class="field field-sm w-48 font-mono" placeholder="IP or range" bind:value={rulePattern} />
				<input class="field field-sm min-w-0 flex-1" placeholder="Note (optional)" bind:value={ruleNote} />
				<button type="submit" class="btn-secondary btn-sm" disabled={!rulePattern.trim()}>Add</button>
			</form>
			{#if sec.rules.length}
				<table class="w-full text-left text-sm">
					<tbody class="divide-y divide-slate-800">
						{#each sec.rules as r (r.pattern)}
							<tr>
								<td class="py-2">
									<span class="rounded px-1.5 py-0.5 text-[10px] uppercase {r.kind === 'allow' ? 'bg-emerald-950 text-emerald-400' : 'bg-red-950 text-red-400'}">{r.kind}</span>
								</td>
								<td class="py-2 font-mono text-ink">{r.pattern}</td>
								<td class="py-2 text-xs text-muted">{r.note}</td>
								<td class="py-2 text-right"><button class="btn-secondary btn-xs" onclick={() => removeRule(r.pattern)}>Remove</button></td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		</section>

		<section class="card">
			<h2 class="card-label mb-3">Latest failed logins</h2>
			{#if sec.failures.length === 0}
				<p class="text-sm text-muted">None in the last day.</p>
			{:else}
				<table class="w-full text-left text-sm">
					<tbody class="divide-y divide-slate-800">
						{#each sec.failures as f, i (i)}
							<tr>
								<td class="py-1.5 text-xs whitespace-nowrap text-muted">{when(f.at)}</td>
								<td class="py-1.5 font-mono text-xs text-ink">{f.ip}</td>
								<td class="py-1.5 text-xs text-ink-soft">{f.handle}</td>
								<td class="py-1.5 text-xs text-faint">{f.source}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		</section>
	</div>
{/if}
