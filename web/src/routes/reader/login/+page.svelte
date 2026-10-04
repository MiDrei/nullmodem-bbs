<script lang="ts">
	import { t, useAccountLang } from '$lib/i18n.svelte';
	import { goto } from '$app/navigation';
	import { bbsLogin } from '$lib/api';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { errorText } from '$lib/reader/session';

	let username = $state('');
	let password = $state('');
	let error = $state<string | null>(null);
	let busy = $state(false);

	async function login(e: SubmitEvent) {
		e.preventDefault();
		error = null;
		busy = true;
		try {
			const res = await bbsLogin(username, password);
			bbsAuth.set({
				token: res.token,
				username: res.username,
				timezone: res.timezone ?? '',
				securityLevel: res.security_level
			});
			await useAccountLang(res.language);
			await goto('/reader', { replaceState: true });
		} catch (err) {
			error = errorText(err, t('web.login.failed'));
		} finally {
			busy = false;
		}
	}
</script>

<form class="mx-auto flex max-w-sm flex-col gap-3 px-6 pt-[18vh]" onsubmit={login}>
	<img src="/reader/icon-192.png" alt="" class="mx-auto mb-4 h-16 w-16 rounded-2xl" />
	<h1 class="mb-3 text-center text-2xl font-semibold text-ink-strong">{t('web.home.reader')}</h1>
	<input
		class="field py-3 text-base"
		bind:value={username}
		placeholder={t('web.login.username')}
		autocomplete="username"
		autocapitalize="off"
		required
	/>
	<input
		class="field py-3 text-base"
		type="password"
		bind:value={password}
		placeholder={t('web.login.password')}
		autocomplete="current-password"
		required
	/>
	{#if error}
		<p class="text-sm text-red-400">{error}</p>
	{/if}
	<button class="btn-primary mt-2 py-3 text-base" disabled={busy}>{busy ? t('web.login.signing_in') : t('web.login.sign_in')}</button>
</form>
