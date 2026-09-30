<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsLogin, getWelcomeScreen, ApiError, type WelcomeScreen } from '$lib/api';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { site } from '$lib/site.svelte';
	import AnsiArt from '$lib/AnsiArt.svelte';

	let username = $state('');
	let password = $state('');
	let error = $state<string | null>(null);
	let submitting = $state(false);
	let welcome = $state<WelcomeScreen | null>(null);

	onMount(async () => {
		site.load();
		try {
			welcome = await getWelcomeScreen();
		} catch {
			// No welcome.ans, or it failed to load -- the login form
			// works fine without a banner, so this is never fatal.
		}
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		error = null;
		submitting = true;
		try {
			const res = await bbsLogin(username, password);
			bbsAuth.set({
				token: res.token,
				username: res.username,
				timezone: res.timezone ?? '',
				securityLevel: res.security_level
			});
			await goto('/message-areas');
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Login failed.';
		} finally {
			submitting = false;
		}
	}

	const host = typeof location === 'undefined' ? '' : location.hostname;
</script>

<div class="flex flex-col items-center gap-7 py-6">
	{#if welcome}
		<!-- The board's own welcome.ans, on D's black ANSI ground: as
		     wide as the window allows (beyond the page column, up to
		     about 1.5x its native size), never scrolling. -->
		<div class="ansi-panel w-[min(calc(100vw-2rem),64rem)] max-w-none">
			{#if welcome.preformatted && welcome.grid}
				<AnsiArt grid={welcome.grid} fit maxZoom={1.6} />
			{:else}
				<div class="inline-block font-mono text-sm leading-tight whitespace-pre">
					{@html welcome.html}
				</div>
			{/if}
		</div>
	{/if}

	<div class="text-center">
		<h1 class="text-3xl font-semibold tracking-tight text-ink-strong">Welcome back</h1>
		<p class="mt-1.5 text-[13.5px] text-muted">Sign in to continue to {site.info.name}</p>
	</div>

	<form class="flex w-full max-w-[340px] flex-col gap-2.5" onsubmit={handleSubmit}>
		<label class="sr-only" for="login-user">Username</label>
		<input
			id="login-user"
			class="field py-3 text-sm"
			bind:value={username}
			placeholder="Username"
			autocomplete="username"
			required
		/>
		<label class="sr-only" for="login-pass">Password</label>
		<input
			id="login-pass"
			type="password"
			class="field py-3 text-sm"
			bind:value={password}
			placeholder="Password"
			autocomplete="current-password"
			required
		/>
		{#if error}
			<p class="text-sm text-red-400">{error}</p>
		{/if}
		<button type="submit" disabled={submitting} class="btn-primary mt-1.5 w-full py-3 text-sm">
			{submitting ? 'Signing in…' : 'Sign in'}
		</button>
	</form>

	{#if site.info.telnet_port || site.info.ssh_port}
		<p class="max-w-[340px] text-center text-xs leading-relaxed text-faint">
			New here? Create an account over
			{#if site.info.telnet_port}
				Telnet (<span class="font-mono text-muted">{host}:{site.info.telnet_port}</span>){/if}{#if site.info.telnet_port && site.info.ssh_port}
				or{/if}
			{#if site.info.ssh_port}
				SSH (<span class="font-mono text-muted">ssh {host} -p {site.info.ssh_port}</span>){/if}
			first.
		</p>
	{/if}
</div>
