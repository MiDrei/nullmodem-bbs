<script lang="ts">
	// The caller's own profile -- the same overview and settings as the
	// Telnet/SSH profile (internal/bbs/profile.go): real name, time zone,
	// password, and a way into the QWK area selection.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { formatDate, allTimezones, browserTimezone } from '$lib/datetime';
	import {
		getBBSProfile,
		updateBBSProfile,
		changeBBSPassword,
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

	const inputClass =
		'rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 text-slate-100 focus:border-cyan-400 focus:outline-none';
	const primaryButton =
		'rounded-lg bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-4 py-2 text-sm font-semibold text-white transition disabled:opacity-50';
	const secondaryButton =
		'rounded-lg border border-slate-700 px-4 py-2 text-sm font-medium text-slate-200 transition hover:bg-slate-800 disabled:opacity-50';

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
			loadError = err instanceof ApiError ? err.message : 'Could not load your profile.';
		}
	});

	async function save(changes: { real_name?: string; timezone?: string }, done: string) {
		if (!bbsAuth.token) return;
		try {
			apply(await updateBBSProfile(bbsAuth.token, changes));
			toast.push(done, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not save.', 'error');
		}
	}

	async function saveName(e: SubmitEvent) {
		e.preventDefault();
		savingName = true;
		await save({ real_name: realName }, 'Real name saved.');
		savingName = false;
	}

	async function saveZone(e: SubmitEvent) {
		e.preventDefault();
		savingZone = true;
		await save({ timezone }, timezone ? `Time zone set to ${timezone}.` : 'Time zone cleared.');
		savingZone = false;
	}

	async function savePassword(e: SubmitEvent) {
		e.preventDefault();
		if (!bbsAuth.token) return;
		if (newPassword !== confirmPassword) {
			toast.push('New passwords do not match.', 'error');
			return;
		}
		savingPassword = true;
		try {
			await changeBBSPassword(bbsAuth.token, currentPassword, newPassword);
			currentPassword = newPassword = confirmPassword = '';
			toast.push('Password changed.', 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not change password.', 'error');
		} finally {
			savingPassword = false;
		}
	}
</script>

<div class="mb-6">
	<h1 class="text-2xl font-bold tracking-tight text-slate-100">Your Profile</h1>
	<p class="mt-1 text-sm text-slate-500">
		Your account and personal settings -- the same ones you can change over Telnet/SSH.
	</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !profile}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	<div class="space-y-6">
		<section class="rounded-2xl border border-slate-800/60 bg-slate-900/40 p-5">
			<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-slate-400">Account</h2>
			<dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5 text-sm">
				<dt class="text-slate-500">Handle</dt>
				<dd class="text-slate-200">{profile.username}</dd>
				<dt class="text-slate-500">Real name</dt>
				<dd class="text-slate-200">{profile.real_name || '—'}</dd>
				<dt class="text-slate-500">Security level</dt>
				<dd class="text-slate-200">{profile.security_level}</dd>
				<dt class="text-slate-500">Total calls</dt>
				<dd class="text-slate-200">{profile.total_calls}</dd>
				<dt class="text-slate-500">Member since</dt>
				<dd class="text-slate-200">{formatDate(profile.created_at)}</dd>
				<dt class="text-slate-500">Time zone</dt>
				<dd class="text-slate-200">
					{profile.timezone || `not set (this browser: ${browserZone}; Telnet/SSH: UTC)`}
				</dd>
			</dl>
		</section>

		<section class="rounded-2xl border border-slate-800/60 bg-slate-900/40 p-5">
			<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-slate-400">Real name</h2>
			<form class="flex flex-wrap items-end gap-3" onsubmit={saveName}>
				<label class="flex min-w-64 flex-1 flex-col gap-1 text-sm">
					<span class="text-slate-400">Real name</span>
					<input class={inputClass} bind:value={realName} required />
				</label>
				<button class={primaryButton} disabled={savingName || realName.trim() === profile.real_name}>
					{savingName ? 'Saving…' : 'Save'}
				</button>
			</form>
		</section>

		<section class="rounded-2xl border border-slate-800/60 bg-slate-900/40 p-5">
			<h2 class="mb-1 text-sm font-semibold uppercase tracking-wide text-slate-400">Time zone</h2>
			<p class="mb-3 text-sm text-slate-500">
				Dates and times are shown in this zone here and over Telnet/SSH.
			</p>
			<form class="flex flex-wrap items-end gap-3" onsubmit={saveZone}>
				<label class="flex min-w-64 flex-1 flex-col gap-1 text-sm">
					<span class="text-slate-400">Zone</span>
					<select class={inputClass} bind:value={timezone}>
						<option value="">Not set</option>
						{#each zones as zone (zone)}
							<option value={zone}>{zone}</option>
						{/each}
					</select>
				</label>
				<button
					type="button"
					class={secondaryButton}
					disabled={timezone === browserZone}
					onclick={() => (timezone = browserZone)}
				>
					Use this browser's ({browserZone})
				</button>
				<button class={primaryButton} disabled={savingZone || timezone === profile.timezone}>
					{savingZone ? 'Saving…' : 'Save'}
				</button>
			</form>
		</section>

		<section class="rounded-2xl border border-slate-800/60 bg-slate-900/40 p-5">
			<h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-slate-400">Password</h2>
			<form class="grid gap-3 sm:grid-cols-3" onsubmit={savePassword}>
				<label class="flex flex-col gap-1 text-sm">
					<span class="text-slate-400">Current password</span>
					<input
						type="password"
						autocomplete="current-password"
						class={inputClass}
						bind:value={currentPassword}
						required
					/>
				</label>
				<label class="flex flex-col gap-1 text-sm">
					<span class="text-slate-400">New password (min {MIN_PASSWORD_LENGTH})</span>
					<input
						type="password"
						autocomplete="new-password"
						minlength={MIN_PASSWORD_LENGTH}
						class={inputClass}
						bind:value={newPassword}
						required
					/>
				</label>
				<label class="flex flex-col gap-1 text-sm">
					<span class="text-slate-400">Confirm new password</span>
					<input
						type="password"
						autocomplete="new-password"
						class={inputClass}
						bind:value={confirmPassword}
						required
					/>
				</label>
				<div class="sm:col-span-3 flex justify-end">
					<button class={primaryButton} disabled={savingPassword}>
						{savingPassword ? 'Changing…' : 'Change password'}
					</button>
				</div>
			</form>
		</section>

		<section
			class="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-slate-800/60 bg-slate-900/40 p-5"
		>
			<div>
				<h2 class="text-sm font-semibold uppercase tracking-wide text-slate-400">QWK area selection</h2>
				<p class="mt-1 text-sm text-slate-500">Choose which message areas your QWK packets include.</p>
			</div>
			<a href="/qwk" class={secondaryButton}>Open QWK settings</a>
		</section>
	</div>
{/if}
