<script lang="ts">
	// Searching messages from the reader: subject, text, from and to,
	// in every area you can read; a result opens in the reader.
	import { t } from '$lib/i18n.svelte';
	import { goto } from '$app/navigation';
	import { searchMessages, type SearchHit } from '$lib/api';
	import { readerToken, readerAuthFailed, errorText, shortDate } from '$lib/reader/session';

	let q = $state('');
	let hits = $state<SearchHit[] | null>(null);
	let error = $state<string | null>(null);
	let busy = $state(false);

	async function run(e: SubmitEvent) {
		e.preventDefault();
		const token = await readerToken();
		if (!token || q.trim().length < 2) return;
		busy = true;
		try {
			hits = await searchMessages(token, q.trim());
			error = null;
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			error = errorText(err, t('web.common.search_failed'));
		} finally {
			busy = false;
		}
	}
</script>

<div class="r-full">
	<header class="r-bar">
		<button class="r-btn text-3xl leading-none" onclick={() => goto('/reader')} aria-label={t('common.back')}>‹</button>
		<span class="r-title">{t('web.common.search')}</span>
	</header>
	<form class="flex gap-2 p-3" onsubmit={run}>
		<!-- svelte-ignore a11y_autofocus -->
		<input class="field min-w-0 flex-1 py-2.5" type="search" placeholder={t('web.reader.search_placeholder')} bind:value={q} autofocus />
		<button type="submit" class="r-btn font-semibold" disabled={busy || q.trim().length < 2}>{busy ? '…' : 'Go'}</button>
	</form>
	{#if error}
		<p class="r-note text-red-400">{error}</p>
	{:else if hits && hits.length === 0}
		<p class="r-note">{t('common.nothing_found')}</p>
	{:else if hits}
		{#each hits as h (h.id)}
			<button class="r-row" onclick={() => goto(`/reader/m/${h.id}`)}>
				<span class="min-w-0 flex-1">
					<span class="block truncate font-medium text-ink-strong">{h.subject}</span>
					<span class="block truncate text-xs text-muted">{h.from_name} · {h.area_tag} · {shortDate(h.posted_at)}</span>
					<span class="mt-0.5 block text-[13px] text-ink-soft">{h.snippet}</span>
				</span>
			</button>
		{/each}
	{/if}
</div>
