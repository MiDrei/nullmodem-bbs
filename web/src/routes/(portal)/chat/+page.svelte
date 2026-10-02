<script lang="ts">
	// The chat rooms in the portal: the teleconference and the sysop's
	// rooms -- the same ones Telnet callers (and, where bridged, Discord
	// and Matrix) are in.
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { listBBSChatRooms, ApiError, type BBSChatRoom } from '$lib/api';
	import ChatView from '$lib/chat/ChatView.svelte';

	let rooms = $state<BBSChatRoom[]>([]);
	let current = $state<string | null>(page.url.searchParams.get('room'));
	let error = $state<string | null>(null);
	let timer: ReturnType<typeof setInterval> | undefined;

	async function failed(err: unknown) {
		if (err instanceof ApiError && err.status === 401) {
			bbsAuth.clear();
			await goto('/login');
		}
	}

	async function load() {
		if (!bbsAuth.token) return;
		try {
			rooms = await listBBSChatRooms(bbsAuth.token);
			error = null;
			if (!current && rooms.length) open(rooms[0].name);
		} catch (err) {
			await failed(err);
			error = err instanceof ApiError ? err.message : 'Could not load the rooms.';
		}
	}

	function open(name: string) {
		current = name;
		history.replaceState(history.state, '', `?room=${encodeURIComponent(name)}`);
	}

	onMount(async () => {
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		await load();
		timer = setInterval(load, 10000);
	});
	onDestroy(() => clearInterval(timer));

	const info = $derived(rooms.find((r) => r.name === current));
</script>

<div class="mb-5">
	<h1 class="page-title">Chat</h1>
	<p class="page-subtitle">Talk with whoever is on -- on Telnet, here, in the reader app{rooms.some((r) => r.bridges.length) ? ', and on Discord or Matrix where a room is bridged' : ''}.</p>
</div>

{#if error}
	<p class="text-sm text-red-400">{error}</p>
{:else}
	<div class="grid gap-4 lg:grid-cols-[15rem_1fr]">
		<section class="card self-start p-0">
			<h2 class="card-label px-4 pt-4 pb-2">Rooms</h2>
			{#each rooms as r (r.name)}
				<button
					class="flex w-full flex-col items-start gap-0.5 border-t border-line px-4 py-2.5 text-left hover:bg-surface {current === r.name ? 'bg-surface' : ''}"
					onclick={() => open(r.name)}
				>
					<span class="flex w-full items-center gap-2">
						<span class="flex-1 truncate {current === r.name ? 'text-accent' : 'text-ink-strong'}">{r.title}</span>
						{#if r.present.length}<span class="rounded-full bg-accent px-1.5 text-[11px] text-black">{r.present.length}</span>{/if}
					</span>
					{#if r.bridges.length}<span class="text-[11px] text-indigo-400">↔ {r.bridges.join(', ')}</span>{/if}
					{#if r.last_line && r.last_line.kind === 'say'}
						<span class="w-full truncate text-xs text-faint">{r.last_line.username}: {r.last_line.text}</span>
					{/if}
				</button>
			{/each}
		</section>

		<section class="card flex h-[32rem] flex-col p-0">
			{#if current && info}
				<div class="border-b border-line px-4 py-3">
					<h2 class="font-semibold text-ink-strong">{info.title}</h2>
					{#if info.topic}<p class="text-xs text-muted">{info.topic}</p>{/if}
				</div>
				{#key current}
					<ChatView room={current} token={() => bbsAuth.token} me={bbsAuth.username ?? ''} onFailed={failed} />
				{/key}
			{:else}
				<p class="m-auto text-sm text-muted">Pick a room.</p>
			{/if}
		</section>
	</div>
{/if}
