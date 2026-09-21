<script lang="ts">
	import { goto } from '$app/navigation';
	import { login, ApiError } from '$lib/api';
	import { auth } from '$lib/auth.svelte';

	let username = $state('');
	let password = $state('');
	let error = $state<string | null>(null);
	let submitting = $state(false);

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		error = null;
		submitting = true;
		try {
			const res = await login(username, password);
			auth.set({ token: res.token, username: res.username });
			await goto('/admin/dashboard');
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Login failed.';
		} finally {
			submitting = false;
		}
	}
</script>

<div class="mx-auto mt-16 max-w-sm">
	<h1 class="mb-6 text-xl font-semibold text-slate-100">Sysop Login</h1>
	<form class="flex flex-col gap-4" onsubmit={handleSubmit}>
		<label class="flex flex-col gap-1 text-sm">
			<span class="text-slate-400">Username</span>
			<input
				class="rounded border border-slate-700 bg-slate-900 px-3 py-2 text-slate-100 focus:border-cyan-500 focus:outline-none"
				bind:value={username}
				autocomplete="username"
				required
			/>
		</label>
		<label class="flex flex-col gap-1 text-sm">
			<span class="text-slate-400">Password</span>
			<input
				type="password"
				class="rounded border border-slate-700 bg-slate-900 px-3 py-2 text-slate-100 focus:border-cyan-500 focus:outline-none"
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
			class="mt-2 rounded bg-cyan-600 px-4 py-2 font-medium text-white hover:bg-cyan-500 disabled:opacity-50"
		>
			{submitting ? 'Signing in…' : 'Sign in'}
		</button>
		<p class="text-xs text-slate-500">Only accounts with sysop-level security may sign in here.</p>
	</form>
</div>
