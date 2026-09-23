<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsLogin, getWelcomeScreen, ApiError, type WelcomeScreen } from '$lib/api';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import AnsiArt from '$lib/AnsiArt.svelte';

	let username = $state('');
	let password = $state('');
	let error = $state<string | null>(null);
	let submitting = $state(false);
	let welcome = $state<WelcomeScreen | null>(null);

	onMount(async () => {
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
			bbsAuth.set({ token: res.token, username: res.username });
			await goto('/message-areas');
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Login failed.';
		} finally {
			submitting = false;
		}
	}
</script>

{#if welcome}
	<div class="mx-auto mt-16 w-fit max-w-full overflow-x-auto rounded-xl border border-slate-800/60 bg-black p-4">
		{#if welcome.preformatted && welcome.grid}
			<AnsiArt grid={welcome.grid} />
		{:else}
			<div class="inline-block font-mono text-sm leading-tight whitespace-pre">
				{@html welcome.html}
			</div>
		{/if}
	</div>
{/if}

<div class="mx-auto max-w-md {welcome ? 'mt-12' : 'mt-16'}">
	<h1 class="mb-3 text-lg font-semibold text-slate-100">Welcome back</h1>
	<form
		class="flex flex-col gap-3 rounded-xl border border-slate-800/60 bg-slate-900/40 p-4"
		onsubmit={handleSubmit}
	>
		<div class="grid grid-cols-2 gap-3">
			<input
				class="min-w-0 rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 text-sm text-slate-100 focus:border-cyan-400 focus:outline-none"
				bind:value={username}
				placeholder="Username"
				autocomplete="username"
				required
			/>
			<input
				type="password"
				class="min-w-0 rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 text-sm text-slate-100 focus:border-cyan-400 focus:outline-none"
				bind:value={password}
				placeholder="Password"
				autocomplete="current-password"
				required
			/>
		</div>

		{#if error}
			<p class="text-sm text-red-400">{error}</p>
		{/if}

		<button
			type="submit"
			disabled={submitting}
			class="rounded-lg bg-cyan-600 px-4 py-1.5 text-sm font-medium text-white hover:bg-cyan-500 disabled:opacity-40"
		>
			{submitting ? 'Signing in…' : 'Sign in'}
		</button>
		<p class="text-xs text-slate-500">
			New here? Connect via Telnet (<span class="font-mono text-slate-400">bbs.maik.ch:2323</span>) or SSH
			(<span class="font-mono text-slate-400">ssh bbs.maik.ch -p 2222</span>) first to create an account.
		</p>
	</form>
</div>
