<script lang="ts">
	// The chat rooms in the reader app, with who's in them.
	import { t } from '$lib/i18n.svelte';
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { listBBSChatRooms, type BBSChatRoom } from '$lib/api';
	import { readerToken, readerAuthFailed, errorText } from '$lib/reader/session';

	let rooms = $state<BBSChatRoom[] | null>(null);
	let error = $state<string | null>(null);
	let timer: ReturnType<typeof setInterval> | undefined;

	async function load() {
		const token = await readerToken();
		if (!token) return;
		try {
			rooms = await listBBSChatRooms(token);
			error = null;
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			error = errorText(err, t('web.common.could_not_load_the_rooms'));
		}
	}

	onMount(() => {
		load();
		timer = setInterval(load, 10000);
	});
	onDestroy(() => clearInterval(timer));
</script>

<div class="r-full">
	<header class="r-bar">
		<button class="r-btn text-3xl leading-none" onclick={() => goto('/reader')} aria-label={t('common.back')}>‹</button>
		<span class="r-title">{t('common.chat')}</span>
	</header>
	{#if error}
		<p class="r-note text-red-400">{error}</p>
	{:else if rooms}
		{#each rooms as r (r.name)}
			<button class="r-row" onclick={() => goto(`/reader/chat/${encodeURIComponent(r.name)}`)}>
				<span class="min-w-0 flex-1">
					<span class="block truncate font-medium text-ink-strong">{r.title}</span>
					<span class="block truncate text-xs text-faint">
						{#if r.present.length}{r.present.map((p) => p.username).join(', ')}{:else if r.last_line && r.last_line.kind === 'say'}{r.last_line.username}: {r.last_line.text}{:else}{r.topic || 'quiet'}{/if}
					</span>
					{#if r.bridges.length}<span class="block text-[11px] text-indigo-400">↔ {r.bridges.join(', ')}</span>{/if}
				</span>
				{#if r.present.length}<span class="r-badge">{r.present.length}</span>{/if}
				<span class="text-faint">›</span>
			</button>
		{:else}
			<p class="r-note">{t('web.chat.no_rooms')}</p>
		{/each}
	{/if}
</div>
