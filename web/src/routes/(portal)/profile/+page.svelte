<script lang="ts">
	// The caller's own profile -- the same overview and settings as the
	// Telnet/SSH profile (internal/bbs/profile.go): real name, time zone,
	// password, and a way into the QWK area selection.
	import { t, i18n } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { formatDate, allTimezones, browserTimezone } from '$lib/datetime';
	import { setLang } from '$lib/i18n.svelte';
	import {
		getBBSProfile,
		updateBBSProfile,
		changeBBSPassword,
		requestNetmailForward,
		confirmNetmailForward,
		setNetmailForwardRead,
		removeNetmailForward,
		ApiError,
		type BBSProfile
	} from '$lib/api';

	const MIN_PASSWORD_LENGTH = 6;

	let profile = $state<BBSProfile | null>(null);
	let loadError = $state<string | null>(null);

	let realName = $state('');
	let savingName = $state(false);

	let timezone = $state('');
	let savingZone = $state(false);

	let place = $state('');
	let savingPlace = $state(false);
	const browserZone = browserTimezone();
	// The stored zone stays selectable even if this browser's own list
	// doesn't happen to include it.
	let zones = $derived(
		profile?.timezone && !allTimezones().includes(profile.timezone)
			? [profile.timezone, ...allTimezones()]
			: allTimezones()
	);

	let currentPassword = $state('');
	let newPassword = $state('');
	let confirmPassword = $state('');
	let savingPassword = $state(false);

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			bbsAuth.clear();
			await goto('/login');
			return true;
		}
		return false;
	}

	function apply(p: BBSProfile) {
		profile = p;
		realName = p.real_name;
		timezone = p.timezone;
		place = p.location;
		bbsAuth.setTimezone(p.timezone);
	}

	onMount(async () => {
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		try {
			apply(await getBBSProfile(bbsAuth.token));
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : t('web.profile.load_failed');
		}
	});

	async function save(
		changes: { real_name?: string; timezone?: string; qwk_routing?: boolean; location?: string; language?: string },
		done: string
	) {
		if (!bbsAuth.token) return;
		try {
			apply(await updateBBSProfile(bbsAuth.token, changes));
			toast.push(done, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('web.common.save_failed'), 'error');
		}
	}

	async function saveName(e: SubmitEvent) {
		e.preventDefault();
		savingName = true;
		await save({ real_name: realName }, t('common.real_name_saved'));
		savingName = false;
	}

	async function saveZone(e: SubmitEvent) {
		e.preventDefault();
		savingZone = true;
		await save({ timezone }, timezone ? t('web.profile.zone_saved', { ZONE: timezone }) : t('web.profile.zone_cleared'));
		savingZone = false;
	}

	async function savePlace(e: SubmitEvent) {
		e.preventDefault();
		savingPlace = true;
		await save({ location: place }, place.trim() ? t('common.location_saved') : t('web.profile.place_cleared'));
		savingPlace = false;
	}

	let savingRouting = $state(false);

	async function saveRouting(on: boolean) {
		savingRouting = true;
		await save({ qwk_routing: on }, on ? t('web.profile.seenby_on') : t('common.qwk_packets_now_leave_seen'));
		savingRouting = false;
	}

	// Netmail forwarding: an address, confirmed with a code mailed there.
	let fwAddress = $state('');
	let fwCode = $state('');
	let fwBusy = $state(false);
	let fwChanging = $state(false);

	async function forward(call: () => Promise<BBSProfile>, done: string) {
		if (!bbsAuth.token) return;
		fwBusy = true;
		try {
			apply(await call());
			if (done) toast.push(done, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('web.common.save_failed'), 'error');
		} finally {
			fwBusy = false;
		}
	}

	async function sendForwardCode(e?: SubmitEvent) {
		e?.preventDefault();
		const addr = fwAddress.trim() || profile?.forward?.address || '';
		await forward(() => requestNetmailForward(bbsAuth.token!, addr), t('web.profile.forward_code_sent_toast'));
		if (profile?.forward?.pending) {
			fwChanging = false;
			fwCode = '';
		}
	}

	async function confirmForward(e: SubmitEvent) {
		e.preventDefault();
		await forward(() => confirmNetmailForward(bbsAuth.token!, fwCode), t('web.profile.forward_on'));
		fwCode = '';
	}

	async function stopForward() {
		await forward(() => removeNetmailForward(bbsAuth.token!), t('web.profile.forward_off'));
		fwAddress = '';
		fwChanging = false;
	}

	async function savePassword(e: SubmitEvent) {
		e.preventDefault();
		if (!bbsAuth.token) return;
		if (newPassword !== confirmPassword) {
			toast.push(t('web.profile.pw_mismatch'), 'error');
			return;
		}
		savingPassword = true;
		try {
			await changeBBSPassword(bbsAuth.token, currentPassword, newPassword);
			currentPassword = newPassword = confirmPassword = '';
			toast.push(t('common.password_changed'), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('web.profile.pw_failed'), 'error');
		} finally {
			savingPassword = false;
		}
	}
</script>

<div class="mb-5">
	<h1 class="page-title">{t('common.your_profile')}</h1>
	<p class="page-subtitle">{t('web.profile.subtitle')}</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !profile}
	<p class="text-sm text-muted">{t('web.common.loading')}</p>
{:else}
	{@const account = [
		[t('common.handle_2'), profile.username],
		[t('common.real_name'), profile.real_name || '—'],
		[t('common.security_level'), String(profile.security_level)],
		[t('common.total_calls'), String(profile.total_calls)],
		[t('common.member_since'), formatDate(profile.created_at)],
		[t('common.time_zone'), profile.timezone || t('web.profile.zone_unset', { ZONE: browserZone })],
		[t('common.location'), profile.location || '—'],
		...(profile.email ? [[t('web.profile.email'), profile.email]] : [])
	]}
	<div class="flex flex-col gap-4">
		<section class="card">
			<h2 class="card-label mb-4">{t('web.profile.account')}</h2>
			<dl class="flex flex-col gap-3 text-[13.5px]">
				{#each account as [label, value] (label)}
					<div class="flex justify-between gap-6">
						<dt class="text-[#7a786f]">{label}</dt>
						<dd class="text-right text-ink">{value}</dd>
					</div>
				{/each}
			</dl>
		</section>

		<section class="card">
			<h2 class="card-label mb-1.5">{t('common.language')}</h2>
			<p class="mb-3.5 text-[13px] text-muted">{t('web.profile.language_hint')}</p>
			<select
				class="field min-w-56"
				value={profile.language || i18n.lang}
				onchange={async (e) => {
					const code = e.currentTarget.value;
					await save({ language: code }, t('web.profile.language_saved'));
					await setLang(code);
				}}
			>
				{#each i18n.languages as l (l.code)}<option value={l.code}>{l.name}</option>{/each}
			</select>
		</section>

		<section class="card">
			<h2 class="card-label mb-3.5">{t('common.real_name')}</h2>
			<form class="flex flex-wrap gap-2.5" onsubmit={saveName}>
				<label class="sr-only" for="profile-realname">{t('common.real_name')}</label>
				<input id="profile-realname" class="field min-w-56 flex-1" bind:value={realName} required />
				<button class="btn-primary" disabled={savingName || realName.trim() === profile.real_name}>
					{savingName ? t('web.common.saving') : t('web.common.save')}
				</button>
			</form>
		</section>

		<section class="card">
			<h2 class="card-label mb-1.5">{t('common.time_zone')}</h2>
			<p class="mb-3.5 text-[13px] text-muted">
				{t('web.profile.zone_hint')}
			</p>
			<form class="flex flex-wrap gap-2.5" onsubmit={saveZone}>
				<label class="sr-only" for="profile-zone">{t('common.time_zone')}</label>
				<select id="profile-zone" class="field min-w-56 flex-1" bind:value={timezone}>
					<option value="">{t('web.profile.not_set')}</option>
					{#each zones as zone (zone)}
						<option value={zone}>{zone}</option>
					{/each}
				</select>
				<button
					type="button"
					class="btn-secondary"
					disabled={timezone === browserZone}
					onclick={() => (timezone = browserZone)}
				>
					{t('web.profile.use_browser', { ZONE: browserZone })}
				</button>
				<button class="btn-primary" disabled={savingZone || timezone === profile.timezone}>
					{savingZone ? t('web.common.saving') : t('web.common.save')}
				</button>
			</form>
		</section>

		<section class="card">
			<h2 class="card-label mb-1.5">{t('common.location')}</h2>
			<p class="mb-3.5 text-[13px] text-muted">
				{t('web.profile.place_hint')}
			</p>
			<form class="flex flex-wrap gap-2.5" onsubmit={savePlace}>
				<label class="sr-only" for="profile-place">{t('common.location')}</label>
				<input
					id="profile-place"
					class="field min-w-56 flex-1"
					bind:value={place}
					maxlength="40"
					placeholder={t('web.profile.place_placeholder')}
				/>
				<button class="btn-primary" disabled={savingPlace || place.trim() === profile.location}>
					{savingPlace ? t('web.common.saving') : t('web.common.save')}
				</button>
			</form>
		</section>

		{#if profile.email}
			{@const fw = profile.forward}
			<section class="card">
				<h2 class="card-label mb-1.5">{t('web.profile.forward_title')}</h2>
				<p class="mb-3.5 text-[13px] text-muted">{t('web.profile.forward_hint')}</p>
				{#if fw?.pending && !fwChanging}
					<p class="mb-3 text-[13px] text-ink">{t('web.profile.forward_code_sent', { ADDRESS: fw.address })}</p>
					<form class="flex flex-wrap gap-2.5" onsubmit={confirmForward}>
						<label class="sr-only" for="profile-fwcode">{t('web.profile.forward_code')}</label>
						<input
							id="profile-fwcode"
							class="field w-40 font-mono tracking-widest"
							bind:value={fwCode}
							inputmode="numeric"
							autocomplete="one-time-code"
							maxlength="7"
							placeholder="123456"
							required
						/>
						<button class="btn-primary" disabled={fwBusy || fwCode.trim().length < 6}>{t('web.profile.forward_confirm')}</button>
						<button type="button" class="btn-secondary" disabled={fwBusy} onclick={() => sendForwardCode()}>
							{t('web.profile.forward_resend')}
						</button>
						<button type="button" class="btn-secondary" disabled={fwBusy} onclick={stopForward}>{t('web.common.cancel')}</button>
					</form>
				{:else if fw?.verified && !fwChanging}
					<p class="mb-3 text-[13px] text-ink">{t('web.profile.forward_active', { ADDRESS: fw.address })}</p>
					<label class="mb-4 flex cursor-pointer items-start gap-3 text-[13px]">
						<input
							type="checkbox"
							class="check mt-0.5"
							checked={fw.mark_read}
							disabled={fwBusy}
							onchange={(e) => {
								const on = (e.currentTarget as HTMLInputElement).checked;
								forward(() => setNetmailForwardRead(bbsAuth.token!, on), t('web.profile.forward_saved'));
							}}
						/>
						<span>
							<span class="text-ink">{t('web.profile.forward_mark_read')}</span>
							<span class="mt-0.5 block text-faint">{t('web.profile.forward_mark_read_hint')}</span>
						</span>
					</label>
					<div class="flex flex-wrap gap-2.5">
						<button type="button" class="btn-secondary" disabled={fwBusy} onclick={() => (fwChanging = true)}>
							{t('web.profile.forward_change')}
						</button>
						<button type="button" class="btn-secondary" disabled={fwBusy} onclick={stopForward}>{t('web.profile.forward_stop')}</button>
					</div>
				{:else}
					<form class="flex flex-wrap gap-2.5" onsubmit={sendForwardCode}>
						<label class="sr-only" for="profile-fwaddr">{t('web.profile.forward_address')}</label>
						<input
							id="profile-fwaddr"
							type="email"
							class="field min-w-56 flex-1"
							bind:value={fwAddress}
							placeholder={t('web.profile.forward_address')}
							autocomplete="email"
							required
						/>
						<button class="btn-primary" disabled={fwBusy || !fwAddress.trim()}>{t('web.profile.forward_send_code')}</button>
						{#if fwChanging}
							<button type="button" class="btn-secondary" disabled={fwBusy} onclick={() => (fwChanging = false)}>
								{t('web.common.cancel')}
							</button>
						{/if}
					</form>
				{/if}
			</section>
		{/if}

		<section class="card">
			<h2 class="card-label mb-3.5">{t('web.common.password')}</h2>
			<form class="grid gap-3 sm:grid-cols-3" onsubmit={savePassword}>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('web.profile.pw_current')}</span>
					<input
						type="password"
						autocomplete="current-password"
						class="field"
						bind:value={currentPassword}
						required
					/>
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('web.profile.pw_new', { MIN: MIN_PASSWORD_LENGTH })}</span>
					<input
						type="password"
						autocomplete="new-password"
						minlength={MIN_PASSWORD_LENGTH}
						class="field"
						bind:value={newPassword}
						required
					/>
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('web.profile.pw_confirm')}</span>
					<input
						type="password"
						autocomplete="new-password"
						class="field"
						bind:value={confirmPassword}
						required
					/>
				</label>
				<div class="flex justify-end sm:col-span-3">
					<button class="btn-primary" disabled={savingPassword}>
						{savingPassword ? t('web.profile.pw_changing') : t('common.change_password')}
					</button>
				</div>
			</form>
		</section>

		<section class="card">
			<div class="flex flex-wrap items-center justify-between gap-3">
				<div>
					<h2 class="card-label">{t('common.qwk_area_selection')}</h2>
					<p class="mt-1.5 text-[13px] text-muted">{t('web.profile.qwk_areas_hint')}</p>
				</div>
				<a href="/qwk" class="btn-secondary hover:text-accent">{t('web.profile.qwk_open')}</a>
			</div>
			<label class="mt-4 flex cursor-pointer items-start gap-3 text-[13px]">
				<input
					type="checkbox"
					class="check mt-0.5"
					checked={profile.qwk_routing}
					disabled={savingRouting}
					onchange={(e) => saveRouting((e.currentTarget as HTMLInputElement).checked)}
				/>
				<span>
					<span class="text-ink">{t('web.profile.seenby')}</span>
					<span class="mt-0.5 block text-faint">
						{t('web.profile.seenby_hint')}
					</span>
				</span>
			</label>
		</section>
	</div>
{/if}
