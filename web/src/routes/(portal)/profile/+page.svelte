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

	async function save(changes: { real_name?: string; timezone?: string; qwk_routing?: boolean }, done: string) {
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

	let savingRouting = $state(false);

	async function saveRouting(on: boolean) {
		savingRouting = true;
		await save({ qwk_routing: on }, on ? 'QWK packets now carry SEEN-BY/PATH.' : 'QWK packets now leave SEEN-BY/PATH out.');
		savingRouting = false;
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

<div class="mb-5">
	<h1 class="page-title">Your Profile</h1>
	<p class="page-subtitle">Your account and personal settings — the same ones you can change over Telnet/SSH</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !profile}
	<p class="text-sm text-muted">Loading…</p>
{:else}
	{@const account = [
		['Handle', profile.username],
		['Real name', profile.real_name || '—'],
		['Security level', String(profile.security_level)],
		['Total calls', String(profile.total_calls)],
		['Member since', formatDate(profile.created_at)],
		['Time zone', profile.timezone || `not set (this browser: ${browserZone}; Telnet/SSH: UTC)`]
	]}
	<div class="flex flex-col gap-4">
		<section class="card">
			<h2 class="card-label mb-4">Account</h2>
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
			<h2 class="card-label mb-3.5">Real name</h2>
			<form class="flex flex-wrap gap-2.5" onsubmit={saveName}>
				<label class="sr-only" for="profile-realname">Real name</label>
				<input id="profile-realname" class="field min-w-56 flex-1" bind:value={realName} required />
				<button class="btn-primary" disabled={savingName || realName.trim() === profile.real_name}>
					{savingName ? 'Saving…' : 'Save'}
				</button>
			</form>
		</section>

		<section class="card">
			<h2 class="card-label mb-1.5">Time zone</h2>
			<p class="mb-3.5 text-[13px] text-muted">
				Dates and times are shown in this zone here and over Telnet/SSH.
			</p>
			<form class="flex flex-wrap gap-2.5" onsubmit={saveZone}>
				<label class="sr-only" for="profile-zone">Zone</label>
				<select id="profile-zone" class="field min-w-56 flex-1" bind:value={timezone}>
					<option value="">Not set</option>
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
					Use this browser's ({browserZone})
				</button>
				<button class="btn-primary" disabled={savingZone || timezone === profile.timezone}>
					{savingZone ? 'Saving…' : 'Save'}
				</button>
			</form>
		</section>

		<section class="card">
			<h2 class="card-label mb-3.5">Password</h2>
			<form class="grid gap-3 sm:grid-cols-3" onsubmit={savePassword}>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">Current password</span>
					<input
						type="password"
						autocomplete="current-password"
						class="field"
						bind:value={currentPassword}
						required
					/>
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">New password (min {MIN_PASSWORD_LENGTH})</span>
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
					<span class="text-xs text-muted">Confirm new password</span>
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
						{savingPassword ? 'Changing…' : 'Change password'}
					</button>
				</div>
			</form>
		</section>

		<section class="card">
			<div class="flex flex-wrap items-center justify-between gap-3">
				<div>
					<h2 class="card-label">QWK area selection</h2>
					<p class="mt-1.5 text-[13px] text-muted">Choose which message areas your QWK packets include.</p>
				</div>
				<a href="/qwk" class="btn-secondary hover:text-accent">Open QWK settings</a>
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
					<span class="text-ink">Include SEEN-BY/PATH lines in QWK packets</span>
					<span class="mt-0.5 block text-faint">
						The routing of echomail, for a reader that hides it and can quote it into a reply
						(NullModem Reader). Most other readers show it as text.
					</span>
				</span>
			</label>
		</section>
	</div>
{/if}
