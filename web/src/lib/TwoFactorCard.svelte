<script lang="ts">
	import { t, tn } from '$lib/i18n.svelte';
	// The signed-in sysop's own two-factor login: set it up (scan the
	// QR code, enter the first code, keep the recovery codes), make new
	// recovery codes, or turn it off -- each with a current code.
	import { onMount } from 'svelte';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		getTOTP,
		startTOTP,
		confirmTOTP,
		disableTOTP,
		newRecoveryCodes,
		ApiError,
		type TOTPStatus,
		type TOTPSetup
	} from '$lib/api';

	let { onChange }: { onChange?: (enabled: boolean) => void } = $props();

	let status = $state<TOTPStatus | null>(null);
	let setup = $state<TOTPSetup | null>(null);
	let codes = $state<string[] | null>(null);
	let code = $state('');
	let busy = $state(false);

	onMount(async () => {
		if (!auth.token) return;
		try {
			status = await getTOTP(auth.token);
		} catch {
			// Shown as unavailable.
		}
	});

	async function act(fn: () => Promise<void>) {
		busy = true;
		try {
			await fn();
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : t('admin.twofactor.that_did_not_work'), 'error');
		} finally {
			busy = false;
		}
	}

	const begin = () =>
		act(async () => {
			setup = await startTOTP(auth.token!);
			code = '';
		});

	const confirm = () =>
		act(async () => {
			const res = await confirmTOTP(auth.token!, code.trim());
			codes = res.recovery_codes;
			setup = null;
			code = '';
			status = { enabled: true, recovery_codes_left: codes.length };
			onChange?.(true);
		});

	const renew = () =>
		act(async () => {
			const res = await newRecoveryCodes(auth.token!, code.trim());
			codes = res.recovery_codes;
			code = '';
			status = { enabled: true, recovery_codes_left: codes.length };
		});

	const turnOff = () =>
		act(async () => {
			status = await disableTOTP(auth.token!, code.trim());
			code = '';
			codes = null;
			onChange?.(false);
			toast.push(t('admin.twofactor.two_factor_login_is_off'), 'success');
		});
</script>

<section class="card flex flex-col gap-3">
	<h2 class="card-label">{t('admin.twofactor.your_account_two_factor_login')}</h2>
	{#if !status}
		<p class="text-sm text-muted">{t('web.common.loading')}</p>
	{:else if codes}
		<p class="text-sm text-ink">
			{t('admin.twofactor.two_factor_login_is_on')} <b>{t('admin.twofactor.recovery_codes')}</b> {t('admin.twofactor.each_works_once_in_place')}
		</p>
		<div class="grid grid-cols-2 gap-2 rounded-xl border border-line bg-sunken p-4 font-mono text-sm text-ink-strong sm:grid-cols-4">
			{#each codes as c (c)}<span>{c}</span>{/each}
		</div>
		<div class="flex justify-end">
			<button class="btn-primary btn-sm" onclick={() => (codes = null)}>{t('admin.twofactor.i_ve_kept_them')}</button>
		</div>
	{:else if setup}
		<p class="text-sm text-muted">
			{t('admin.twofactor.scan_this_with_your_authenticator')}
		</p>
		<div class="flex flex-wrap items-center gap-5">
			<img src={setup.qr} alt={t('admin.twofactor.qr_code_for_the_authenticator')} class="h-48 w-48 rounded-lg bg-white p-2" />
			<div class="flex min-w-0 flex-1 flex-col gap-2">
				<span class="text-xs text-faint">{t('admin.twofactor.or_type_the_key_in')}</span>
				<code class="font-mono text-sm break-all text-ink">{setup.secret}</code>
				<form class="mt-2 flex gap-2" onsubmit={(e) => { e.preventDefault(); confirm(); }}>
					<input class="field w-36 text-center font-mono tracking-widest" bind:value={code} placeholder="123456" inputmode="numeric" autocomplete="one-time-code" />
					<button type="submit" class="btn-primary btn-sm" disabled={busy || code.trim().length !== 6}>{t('admin.common.turn_on')}</button>
					<button type="button" class="btn-secondary btn-sm" onclick={() => (setup = null)}>{t('web.common.cancel')}</button>
				</form>
			</div>
		</div>
	{:else if status.enabled}
		<p class="text-sm text-ink">
			<span class="text-emerald-400">{t('admin.twofactor.on')}</span> {tn('admin.twofactor.recovery_left', status.recovery_codes_left)}
		</p>
		<form class="flex flex-wrap items-center gap-2" onsubmit={(e) => e.preventDefault()}>
			<input class="field w-36 text-center font-mono tracking-widest" bind:value={code} placeholder={t('admin.twofactor.code')} autocomplete="one-time-code" />
			<button class="btn-secondary btn-sm" disabled={busy || !code.trim()} onclick={renew}>{t('admin.twofactor.new_recovery_codes')}</button>
			<button class="btn-secondary btn-sm" disabled={busy || !code.trim()} onclick={turnOff}>{t('admin.common.turn_off')}</button>
		</form>
	{:else}
		<p class="text-sm text-muted">
			{t('admin.twofactor.off_with_it_on_signing')}
		</p>
		<div>
			<button class="btn-primary btn-sm" disabled={busy} onclick={begin}>{t('admin.twofactor.set_up_two_factor_login')}</button>
		</div>
	{/if}
</section>
