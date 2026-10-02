<script lang="ts">
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
			toast.push(err instanceof ApiError ? err.message : 'That did not work.', 'error');
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
			toast.push('Two-factor login is off.', 'success');
		});
</script>

<section class="card flex flex-col gap-3">
	<h2 class="card-label">Your account: two-factor login</h2>
	{#if !status}
		<p class="text-sm text-muted">Loading…</p>
	{:else if codes}
		<p class="text-sm text-ink">
			Two-factor login is on. These are your <b>recovery codes</b> -- each works once in place of a code, if
			your phone is lost. Keep them somewhere safe (a password manager, on paper); they're not shown again.
		</p>
		<div class="grid grid-cols-2 gap-2 rounded-xl border border-line bg-sunken p-4 font-mono text-sm text-ink-strong sm:grid-cols-4">
			{#each codes as c (c)}<span>{c}</span>{/each}
		</div>
		<div class="flex justify-end">
			<button class="btn-primary btn-sm" onclick={() => (codes = null)}>I've kept them</button>
		</div>
	{:else if setup}
		<p class="text-sm text-muted">
			Scan this with your authenticator app (Aegis, Google Authenticator, 1Password, …), then enter the code
			it shows.
		</p>
		<div class="flex flex-wrap items-center gap-5">
			<img src={setup.qr} alt="QR code for the authenticator app" class="h-48 w-48 rounded-lg bg-white p-2" />
			<div class="flex min-w-0 flex-1 flex-col gap-2">
				<span class="text-xs text-faint">Or type the key in:</span>
				<code class="font-mono text-sm break-all text-ink">{setup.secret}</code>
				<form class="mt-2 flex gap-2" onsubmit={(e) => { e.preventDefault(); confirm(); }}>
					<input class="field w-36 text-center font-mono tracking-widest" bind:value={code} placeholder="123456" inputmode="numeric" autocomplete="one-time-code" />
					<button type="submit" class="btn-primary btn-sm" disabled={busy || code.trim().length !== 6}>Turn on</button>
					<button type="button" class="btn-secondary btn-sm" onclick={() => (setup = null)}>Cancel</button>
				</form>
			</div>
		</div>
	{:else if status.enabled}
		<p class="text-sm text-ink">
			<span class="text-emerald-400">On.</span> Signing in here and the sysop menu over Telnet/SSH ask for the
			code from your app. {status.recovery_codes_left} recovery code{status.recovery_codes_left === 1 ? '' : 's'} left.
		</p>
		<form class="flex flex-wrap items-center gap-2" onsubmit={(e) => e.preventDefault()}>
			<input class="field w-36 text-center font-mono tracking-widest" bind:value={code} placeholder="code" autocomplete="one-time-code" />
			<button class="btn-secondary btn-sm" disabled={busy || !code.trim()} onclick={renew}>New recovery codes</button>
			<button class="btn-secondary btn-sm" disabled={busy || !code.trim()} onclick={turnOff}>Turn off</button>
		</form>
	{:else}
		<p class="text-sm text-muted">
			Off. With it on, signing in here needs a code from an app on your phone as well as the password -- a
			stolen password alone isn't enough. The portal, the reader, QWK and normal Telnet logins stay as they are.
		</p>
		<div>
			<button class="btn-primary btn-sm" disabled={busy} onclick={begin}>Set up two-factor login</button>
		</div>
	{/if}
</section>
