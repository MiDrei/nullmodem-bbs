<script lang="ts">
	// Picks the language the portal, the reader and the front page are
	// shown in. Logged in, it's the account's language -- Telnet too.
	import { i18n, setLang, t } from '$lib/i18n.svelte';
	import { updateBBSProfile } from '$lib/api';
	import { bbsAuth } from '$lib/bbs-auth.svelte';

	let { class: cls = '' }: { class?: string } = $props();

	async function change(code: string) {
		await setLang(code);
		if (bbsAuth.token) {
			try {
				await updateBBSProfile(bbsAuth.token, { language: code });
			} catch {
				// The page is switched; the account keeps its language.
			}
		}
	}
</script>

{#if i18n.languages.length > 1}
	<select
		class="cursor-pointer bg-transparent text-inherit outline-none hover:text-accent {cls}"
		value={i18n.lang}
		onchange={(e) => change(e.currentTarget.value)}
		aria-label={t('web.common.language')}
	>
		{#each i18n.languages as l (l.code)}<option value={l.code} class="bg-ground text-ink">{l.name}</option>{/each}
	</select>
{/if}
