<script lang="ts">
	// Chat with the callers: the rooms with something going on (a
	// caller paging you first), talking in one -- they're on Telnet/SSH,
	// the room is shared through the database -- and the one-liners
	// wall to tidy up.
	import { onMount, onDestroy, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { auth } from '$lib/auth.svelte';
	import ChatSettings from '$lib/admin/ChatSettings.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listChatRooms,
		getChatRoom,
		chatAction,
		listOneliners,
		deleteOneliner,
		ApiError,
		type ChatRoom,
		type ChatLine,
		type ChatPresence,
		type Oneliner,
		type ChatRoomSettings
	} from '$lib/api';

	let rooms = $state<ChatRoom[]>([]);
	let current = $state<string | null>(null);
	let lines = $state<ChatLine[]>([]);
	let present = $state<ChatPresence[]>([]);
	let text = $state('');
	let oneliners = $state<Oneliner[]>([]);
	let settings = $state<ChatRoomSettings[]>([]);
	let box = $state<HTMLDivElement | undefined>();
	let roomTimer: ReturnType<typeof setInterval> | undefined;
	let listTimer: ReturnType<typeof setInterval> | undefined;
	let polling = false;

	async function failed(err: unknown, fallback: string) {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return;
		}
		toast.push(err instanceof ApiError ? err.message : fallback, 'error');
	}

	async function loadRooms() {
		if (!auth.token) return;
		try {
			rooms = await listChatRooms(auth.token);
		} catch (err) {
			await failed(err, 'Could not load the rooms.');
		}
	}

	async function poll() {
		if (!auth.token || !current || polling) return;
		polling = true;
		const room = current;
		try {
			const last = lines.length ? lines[lines.length - 1].id : 0;
			const st = await getChatRoom(auth.token, room, last);
			if (room !== current) return;
			present = st.present;
			if (st.lines.length) {
				lines = [...lines, ...st.lines];
				await tick();
				box?.scrollTo({ top: box.scrollHeight });
			}
		} catch {
			// Next time.
		} finally {
			polling = false;
		}
	}

	async function open(room: string) {
		if (!auth.token || room === current) return;
		await leave();
		current = room;
		lines = [];
		present = [];
		try {
			await chatAction(auth.token, room, 'enter');
		} catch (err) {
			await failed(err, 'Could not enter the room.');
		}
		history.replaceState(history.state, '', `?room=${encodeURIComponent(room)}`);
		await poll();
		clearInterval(roomTimer);
		roomTimer = setInterval(poll, 1000);
	}

	async function leave() {
		clearInterval(roomTimer);
		if (auth.token && current) {
			const room = current;
			current = null;
			await chatAction(auth.token, room, 'leave').catch(() => {});
		}
	}

	async function send(e: SubmitEvent) {
		e.preventDefault();
		const t = text.trim();
		if (!auth.token || !current || !t) return;
		text = '';
		try {
			await chatAction(auth.token, current, 'say', t);
			await poll();
		} catch (err) {
			text = t;
			await failed(err, 'Could not send it.');
		}
	}

	async function removeOneliner(o: Oneliner) {
		if (!auth.token || !confirm(`Delete "${o.text}" by ${o.username}?`)) return;
		try {
			await deleteOneliner(auth.token, o.id);
			oneliners = oneliners.filter((x) => x.id !== o.id);
		} catch (err) {
			await failed(err, 'Could not delete it.');
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		await loadRooms();
		listOneliners(auth.token).then((l) => (oneliners = l)).catch(() => {});
		listTimer = setInterval(loadRooms, 5000);
		const want = page.url.searchParams.get('room');
		if (want) await open(want);
	});

	onDestroy(() => {
		clearInterval(listTimer);
		leave();
	});

	const time = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
	const label = (name: string) =>
		settings.find((r) => r.name === name)?.title ?? (name === 'main' ? 'Teleconference' : name.startsWith('page-') ? `${name.slice(5)} (page)` : name);
	const bridged = (name: string) => !!settings.find((r) => r.name === name)?.discord_channel;
	const who = (p: ChatPresence[]) => p.map((x) => (x.source === 'web' ? `${x.username} (web)` : `${x.username} (${x.source})`)).join(', ');
</script>

<div class="mb-6">
	<h1 class="page-title">Chat & One-liners</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">
		Talk with the callers on Telnet and SSH -- and, through a bridged room, with Discord. A caller paging
		you waits in their own room -- you get a notification, and it's at the top here.
	</p>
</div>

<div class="grid gap-4 lg:grid-cols-[16rem_1fr]">
	<section class="card p-0">
		<h2 class="card-label px-4 pt-4 pb-2">Rooms</h2>
		{#each rooms as r (r.name)}
			<button
				class="flex w-full flex-col items-start gap-0.5 border-t border-line px-4 py-2.5 text-left hover:bg-surface {current === r.name ? 'bg-surface' : ''}"
				onclick={() => open(r.name)}
			>
				<span class="flex w-full items-center gap-2">
					<span class="flex-1 truncate {r.paging ? 'font-semibold text-fuchsia-300' : 'text-ink-strong'}">{label(r.name)}</span>
					{#if bridged(r.name)}<span class="text-[11px] text-indigo-400" title="Bridged to Discord">↔ Discord</span>{/if}
					{#if r.present.length}<span class="r-badge rounded-full bg-accent px-1.5 text-[11px] text-black">{r.present.length}</span>{/if}
				</span>
				{#if r.paging}
					<span class="text-xs text-fuchsia-300">waiting for you</span>
				{:else if r.last_line}
					<span class="w-full truncate text-xs text-faint">{r.last_line.username}: {r.last_line.text || r.last_line.kind}</span>
				{/if}
			</button>
		{/each}
	</section>

	<section class="card flex min-h-[28rem] flex-col p-0">
		{#if !current}
			<p class="m-auto text-sm text-muted">Pick a room.</p>
		{:else}
			<div class="flex items-baseline gap-3 border-b border-line px-4 py-3">
				<h2 class="font-semibold text-ink-strong">{label(current)}</h2>
				<span class="min-w-0 flex-1 truncate text-xs text-muted">{present.length ? 'here: ' + who(present) : 'nobody here'}</span>
				<button class="btn-secondary btn-xs" onclick={leave}>Leave</button>
			</div>
			<div bind:this={box} class="h-[24rem] flex-1 overflow-y-auto px-4 py-3 font-mono text-[13px] leading-relaxed">
				{#each lines as l (l.id)}
					<div>
						<span class="text-faint">{time(l.at)}</span>
						{#if l.kind === 'join'}
							<span class="text-emerald-500">{l.username} joined ({l.source})</span>
						{:else if l.kind === 'leave'}
							<span class="text-emerald-700">{l.username} left</span>
						{:else if l.kind === 'page'}
							<span class="font-semibold text-fuchsia-300">{l.username} paged you: {l.text}</span>
						{:else}
							{#if l.source === 'discord'}
								<span class="text-indigo-400">{l.username}@discord:</span>
							{:else}
								<span class="text-accent">{l.username}:</span>
							{/if}
							<span class="text-ink">{l.text}</span>
						{/if}
					</div>
				{/each}
			</div>
			<form class="flex gap-2 border-t border-line p-3" onsubmit={send}>
				<!-- svelte-ignore a11y_autofocus -->
				<input class="field min-w-0 flex-1" maxlength="400" placeholder="Say something…" bind:value={text} autofocus />
				<button type="submit" class="btn-primary btn-sm" disabled={!text.trim()}>Send</button>
			</form>
		{/if}
	</section>
</div>

<div class="mt-4">
	<ChatSettings onchange={(r) => (settings = r)} />
</div>

<section class="card mt-4">
	<h2 class="card-label mb-3">One-liners</h2>
	{#if oneliners.length === 0}
		<p class="text-sm text-muted">The wall is empty.</p>
	{:else}
		<table class="w-full text-left text-sm">
			<tbody class="divide-y divide-slate-800">
				{#each [...oneliners].reverse() as o (o.id)}
					<tr>
						<td class="py-1.5 text-xs whitespace-nowrap text-muted">{new Date(o.at).toLocaleString()}</td>
						<td class="py-1.5 text-accent">{o.username}</td>
						<td class="py-1.5 text-ink">{o.text}</td>
						<td class="py-1.5 text-right"><button class="btn-secondary btn-xs" onclick={() => removeOneliner(o)}>Delete</button></td>
					</tr>
				{/each}
			</tbody>
		</table>
	{/if}
</section>
