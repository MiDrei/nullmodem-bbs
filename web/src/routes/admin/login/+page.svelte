<script lang="ts">
	import { goto } from '$app/navigation';
	import { login, ApiError, TwoFactorRequired } from '$lib/api';
	import { auth } from '$lib/auth.svelte';
	import { site } from '$lib/site.svelte';

	let username = $state('');
	let password = $state('');
	let code = $state('');
	let needCode = $state(false);
	let codeField = $state<HTMLInputElement | undefined>();
	let error = $state<string | null>(null);
	let submitting = $state(false);

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		error = null;
		submitting = true;
		try {
			const res = await login(username, password, code.trim());
			auth.set({ token: res.token, username: res.username });
			await goto('/admin/dashboard');
		} catch (err) {
			if (err instanceof TwoFactorRequired) {
				// First time asked: no complaint, just the field.
				error = needCode ? err.message : null;
				needCode = true;
				code = '';
				setTimeout(() => codeField?.focus(), 0);
				return;
			}
			error = err instanceof ApiError ? err.message : 'Login failed.';
		} finally {
			submitting = false;
		}
	}
</script>

<div class="flex flex-col items-center gap-7 py-14">
	<div class="text-center">
		<h1 class="text-3xl font-semibold tracking-tight text-ink-strong">Sysop Login</h1>
		<p class="mt-1.5 text-[13.5px] text-muted">Administer {site.info.name}</p>
	</div>

	<form class="flex w-full max-w-[340px] flex-col gap-2.5" onsubmit={handleSubmit}>
		<label class="sr-only" for="admin-user">Username</label>
		<input
			id="admin-user"
			class="field py-3 text-sm"
			bind:value={username}
			placeholder="Username"
			autocomplete="username"
			required
		/>
		<label class="sr-only" for="admin-pass">Password</label>
		<input
			id="admin-pass"
			type="password"
			class="field py-3 text-sm"
			bind:value={password}
			placeholder="Password"
			autocomplete="current-password"
			required
		/>
		{#if needCode}
			<label class="sr-only" for="admin-code">Code</label>
			<input
				id="admin-code"
				bind:this={codeField}
				class="field py-3 text-center font-mono text-lg tracking-[0.3em]"
				bind:value={code}
				placeholder="123456"
				inputmode="numeric"
				autocomplete="one-time-code"
				required
			/>
			<p class="text-xs text-faint">The code from your authenticator app -- or one of your recovery codes.</p>
		{/if}
		{#if error}
			<p class="text-sm text-red-400">{error}</p>
		{/if}
		<button type="submit" disabled={submitting} class="btn-primary mt-1.5 w-full py-3 text-sm">
			{submitting ? 'Signing in…' : 'Sign in'}
		</button>
	</form>

	<p class="max-w-[340px] text-center text-xs leading-relaxed text-faint">
		Only accounts with sysop-level security may sign in here.
	</p>
</div>
