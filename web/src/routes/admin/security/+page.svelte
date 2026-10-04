<script lang="ts">
	import { t } from '$lib/i18n.svelte';
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
		getTOTP,
		ApiError,
		type SecuritySettings,
		type SecurityState
	} from '$lib/api';
	import TwoFactorCard from '$lib/TwoFactorCard.svelte';

	let sec = $state<SecurityState | null>(null);
	let settings = $state<SecuritySettings | null>(null);
	let handles = $state('');
	let loadError = $state<string | null>(null);
	let saving = $state(false);
	let rulePattern = $state('');
	let ruleKind = $state<'allow' | 'block'>('allow');
	let ruleNote = $state('');
	let timer: ReturnType<typeof setInterval> | undefined;
	let myTwoFactor = $state(false);

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
			getTOTP(auth.token).then((r) => (myTwoFactor = r.enabled)).catch(() => {});
		} catch (err) {
			loadError = err instanceof ApiError ? err.message : t('admin.security.could_not_load_the_security');
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
			toast.push(t('admin.security.saved_in_effect_within_half'), 'success');
		} catch (err) {
			await failed(err, t('admin.security.could_not_save'));
		} finally {
			saving = false;
		}
	}

	async function unlock(ip: string) {
		if (!auth.token) return;
		try {
			apply(await unlockIP(auth.token, ip), false);
			toast.push(t('admin.security.ip_unlocked', { IP: ip }), 'success');
		} catch (err) {
			await failed(err, t('admin.security.could_not_unlock'));
		}
	}

	async function addRule(e?: SubmitEvent, pattern = rulePattern, kind = ruleKind, note = ruleNote) {
		e?.preventDefault();
		if (!auth.token || !pattern.trim()) return;
		try {
			apply(await addIPRule(auth.token, pattern, kind, note), false);
			rulePattern = '';
			ruleNote = '';
			toast.push(t('admin.security.pattern_is_on_the_kind', { PATTERN: pattern, KIND: kind }), 'success');
		} catch (err) {
			await failed(err, t('admin.security.could_not_add_it'));
		}
	}

	async function removeRule(pattern: string) {
		if (!auth.token || !confirm(t('admin.security.take_pattern_off_the_list', { PATTERN: pattern }))) return;
		try {
			apply(await deleteIPRule(auth.token, pattern), false);
		} catch (err) {
			await failed(err, t('admin.security.could_not_remove_it'));
		}
	}

	const when = (iso: string) => new Date(iso).toLocaleString([], { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
	let youAllowed = $derived(sec?.rules.some((r) => r.kind === 'allow' && r.pattern === sec?.your_ip) ?? false);
</script>

<div class="mb-6">
	<h1 class="page-title">{t('admin.security.security')}</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">
		{t('admin.security.addresses_that_keep_failing_to')}
	</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !sec || !settings}
	<p class="text-sm text-muted">{t('admin.common.loading')}</p>
{:else}
	<div class="flex flex-col gap-4">
		<TwoFactorCard onChange={(on) => (myTwoFactor = on)} />
		{#if !youAllowed}
			<p class="rounded-xl border border-line bg-sunken px-4 py-3 text-sm text-muted">
				{t('admin.security.you_re_connected_from')} <span class="font-mono text-ink">{sec.your_ip}</span>.
				<button class="ml-1 text-accent hover:underline" onclick={() => addRule(undefined, sec!.your_ip, 'allow', 'sysop')}>
					{t('admin.security.put_it_on_the_allow')}
				</button>
				{t('admin.security.so_you_can_never_lock')}
			</p>
		{/if}

		<form class="card flex flex-col gap-4" onsubmit={save}>
			<h2 class="card-label">{t('admin.security.login_protection')}</h2>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={settings.lockout_enabled} />
				<span class="text-ink">{t('admin.security.lock_out_addresses_that_keep')}</span>
			</label>
			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.security.failed_logins')}</span>
					<input type="number" min="1" class="field field-sm" bind:value={settings.max_failures} disabled={!settings.lockout_enabled} />
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.security.within_minutes')}</span>
					<input type="number" min="1" class="field field-sm" bind:value={settings.window_minutes} disabled={!settings.lockout_enabled} />
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.security.locked_out_for_minutes')}</span>
					<input type="number" min="1" class="field field-sm" bind:value={settings.lockout_minutes} disabled={!settings.lockout_enabled} />
					<span class="text-[11px] text-faint">{t('admin.security.4_each_time_again_within')}</span>
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.security.at_most_hours')}</span>
					<input type="number" min="1" class="field field-sm" bind:value={settings.max_lockout_hours} disabled={!settings.lockout_enabled} />
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.security.telnet_ssh_connections_per_address')}</span>
					<input type="number" min="0" class="field field-sm" bind:value={settings.max_connections_per_ip} />
					<span class="text-[11px] text-faint">{t('admin.security.at_the_same_time_0')}</span>
				</label>
			</div>

			<label class="flex items-start gap-2 text-sm">
				<input type="checkbox" class="check mt-0.5" bind:checked={settings.require_admin_totp} disabled={!myTwoFactor && !settings.require_admin_totp} />
				<span>
					<span class="text-ink">{t('admin.security.require_two_factor_login_for')}</span>
					<span class="block text-xs text-faint">
						{t('admin.security.sysop_accounts_without_it_can', { V: myTwoFactor ? '' : t('admin.security.above') })}
					</span>
				</span>
			</label>

			<h2 class="card-label mt-2">{t('admin.security.new_users')}</h2>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={settings.approve_new_users} />
				<span class="text-ink">{t('admin.security.new_accounts_wait_for_my')}</span>
			</label>
			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.security.waiting_at_sl')}</span>
					<input type="number" min="0" max="254" class="field field-sm" bind:value={settings.pending_sl} disabled={!settings.approve_new_users} />
					<span class="text-[11px] text-faint">{t('admin.security.they_read_and_write_netmail')}</span>
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.security.new_users_get_sl')}</span>
					<input type="number" min="1" max="254" class="field field-sm" bind:value={settings.new_user_sl} />
					<span class="text-[11px] text-faint">{t('admin.security.on_approval_or_at_once')}</span>
				</label>
				<label class="flex flex-col gap-1 sm:col-span-2">
					<span class="text-xs text-muted">{t('admin.security.blocked_handles')}</span>
					<input class="field field-sm" placeholder={t('admin.security.e_g_guest2_test')} bind:value={handles} />
					<span class="text-[11px] text-faint">{t('admin.security.comma_separated_besides_the_built')}</span>
				</label>
			</div>
			<div class="flex justify-end">
				<button type="submit" class="btn-primary btn-sm" disabled={saving}>{saving ? t('admin.common.saving') : t('admin.common.save')}</button>
			</div>
		</form>

		<section class="card">
			<h2 class="card-label mb-3">{t('admin.security.locked_out_now')}</h2>
			{#if sec.lockouts.length === 0}
				<p class="text-sm text-muted">{t('admin.security.nobody')}</p>
			{:else}
				<table class="w-full text-left text-sm">
					<tbody class="divide-y divide-slate-800">
						{#each sec.lockouts as l (l.ip)}
							<tr>
								<td class="py-2 font-mono text-ink">{l.ip}</td>
								<td class="py-2 text-xs text-muted">
									{t('admin.security.until_v_v2', { V: when(l.until), V2: l.strikes > 1 ? t('admin.security.strikes_time', { STRIKES: l.strikes }) : '' })}
									<span class="block text-faint">{l.reason}</span>
								</td>
								<td class="py-2 text-right whitespace-nowrap">
									<button class="btn-secondary btn-xs" onclick={() => unlock(l.ip)}>{t('admin.security.unlock')}</button>
									<button class="btn-secondary btn-xs" onclick={() => addRule(undefined, l.ip, 'block', 'locked out repeatedly')}>{t('admin.security.block_for_good')}</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		</section>

		<section class="card">
			<h2 class="card-label mb-1">{t('admin.security.allow_and_block_lists')}</h2>
			<p class="mb-3 text-xs text-muted">
				{t('admin.security.an_ip_or_a_range')}
			</p>
			<form class="mb-3 flex flex-wrap items-center gap-2" onsubmit={(e) => addRule(e)}>
				<select class="field field-sm w-28" bind:value={ruleKind}>
					<option value="allow">{t('admin.security.allow')}</option>
					<option value="block">{t('admin.security.block')}</option>
				</select>
				<input class="field field-sm w-48 font-mono" placeholder={t('admin.security.ip_or_range')} bind:value={rulePattern} />
				<input class="field field-sm min-w-0 flex-1" placeholder={t('admin.security.note_optional')} bind:value={ruleNote} />
				<button type="submit" class="btn-secondary btn-sm" disabled={!rulePattern.trim()}>{t('admin.common.add')}</button>
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
								<td class="py-2 text-right"><button class="btn-secondary btn-xs" onclick={() => removeRule(r.pattern)}>{t('admin.common.remove')}</button></td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		</section>

		<section class="card">
			<h2 class="card-label mb-3">{t('admin.security.latest_failed_logins')}</h2>
			{#if sec.failures.length === 0}
				<p class="text-sm text-muted">{t('admin.security.none_in_the_last_day')}</p>
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
