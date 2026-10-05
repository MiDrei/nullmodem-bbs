<script lang="ts">
	import { t } from '$lib/i18n.svelte';
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
			error = err instanceof ApiError ? err.message : t('web.common.login_failed');
		} finally {
			submitting = false;
		}
	}
</script>

<div class="flex flex-col items-center gap-7 py-14">
	<div class="text-center">
		<h1 class="text-3xl font-semibold tracking-tight text-ink-strong">{t('admin.login.sysop_login')}</h1>
		<p class="mt-1.5 text-[13.5px] text-muted">{t('admin.login.administer_name', { NAME: site.info.name })}</p>
	</div>

	<form class="flex w-full max-w-[340px] flex-col gap-2.5" onsubmit={handleSubmit}>
		<label class="sr-only" for="admin-user">{t('web.common.username')}</label>
		<input
			id="admin-user"
			class="field py-3 text-sm"
			bind:value={username}
			placeholder={t('web.common.username')}
			autocomplete="username"
			required
		/>
		<label class="sr-only" for="admin-pass">{t('web.common.password')}</label>
		<input
			id="admin-pass"
			type="password"
			class="field py-3 text-sm"
			bind:value={password}
			placeholder={t('web.common.password')}
			autocomplete="current-password"
			required
		/>
		{#if needCode}
			<label class="sr-only" for="admin-code">{t('admin.login.code')}</label>
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
			<p class="text-xs text-faint">{t('admin.login.the_code_from_your_authenticator')}</p>
		{/if}
		{#if error}
			<p class="text-sm text-red-400">{error}</p>
		{/if}
		<button type="submit" disabled={submitting} class="btn-primary mt-1.5 w-full py-3 text-sm">
			{submitting ? t('web.common.signing_in') : t('web.common.sign_in')}
		</button>
	</form>

	<p class="max-w-[340px] text-center text-xs leading-relaxed text-faint">
		{t('admin.login.only_accounts_with_sysop_level')}
	</p>
</div>
