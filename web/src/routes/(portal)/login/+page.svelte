<script lang="ts">
	import { goto } from '$app/navigation';
	import { bbsLogin, ApiError } from '$lib/api';
	import { bbsAuth } from '$lib/bbs-auth.svelte';

	let username = $state('');
	let password = $state('');
	let error = $state<string | null>(null);
	let submitting = $state(false);

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

<div class="mx-auto mt-16 max-w-sm">
	<h1
		class="mb-6 bg-gradient-to-r from-cyan-400 to-fuchsia-500 bg-clip-text text-2xl font-bold tracking-tight text-transparent"
	>
		Welcome back
	</h1>
	<form
		class="flex flex-col gap-4 rounded-2xl border border-slate-800/60 bg-slate-900/40 p-6"
		onsubmit={handleSubmit}
	>
		<label class="flex flex-col gap-1 text-sm">
			<span class="text-slate-400">Username</span>
			<input
				class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-slate-100 focus:border-cyan-400 focus:outline-none"
				bind:value={username}
				autocomplete="username"
				required
			/>
		</label>
		<label class="flex flex-col gap-1 text-sm">
			<span class="text-slate-400">Password</span>
			<input
				type="password"
				class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-slate-100 focus:border-cyan-400 focus:outline-none"
				bind:value={password}
				autocomplete="current-password"
				required
			/>
		</label>

		{#if error}
			<p class="text-sm text-red-400">{error}</p>
		{/if}

		<button
			type="submit"
			disabled={submitting}
			class="mt-2 rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-4 py-2 font-semibold text-white shadow-lg shadow-fuchsia-500/20 transition hover:shadow-fuchsia-500/40 disabled:opacity-40"
		>
			{submitting ? 'Signing in…' : 'Sign in'}
		</button>
		<p class="text-xs text-slate-500">
			New here? Connect via Telnet or SSH first to create an account.
		</p>
	</form>
</div>
