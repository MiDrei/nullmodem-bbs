<script lang="ts">
	// One chat room, full screen.
	import { t } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { listBBSChatRooms, type BBSChatRoom } from '$lib/api';
	import { readerToken, readerAuthFailed } from '$lib/reader/session';
	import ChatView from '$lib/chat/ChatView.svelte';

	const room = page.params.room ?? '';
	let info = $state<BBSChatRoom | null>(null);

	onMount(async () => {
		const token = await readerToken();
		if (!token) return;
		try {
			info = (await listBBSChatRooms(token)).find((r) => r.name === room) ?? null;
		} catch (err) {
			await readerAuthFailed(err);
		}
	});
</script>

<div class="r-full flex h-dvh flex-col">
	<header class="r-bar">
		<button class="r-btn text-3xl leading-none" onclick={() => goto('/reader/chat')} aria-label={t('common.back')}>‹</button>
		<span class="r-title">{info?.title ?? room}</span>
		{#if info?.bridges.length}<span class="text-xs text-indigo-400">↔ {info.bridges.join(', ')}</span>{/if}
	</header>
	<ChatView {room} token={readerToken} me={bbsAuth.username ?? ''} compact onFailed={(err) => readerAuthFailed(err)} />
</div>
