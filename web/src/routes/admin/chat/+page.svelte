<script lang="ts">
	import { t, i18n } from '$lib/i18n.svelte';
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
		clearChatRoom,
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
			await failed(err, t('web.common.could_not_load_the_rooms'));
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
			await failed(err, t('admin.chat.could_not_enter_the_room'));
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

	async function clearRoom() {
		if (!auth.token || !current || !confirm(t('admin.chat.clear_confirm', { ROOM: label(current) }))) return;
		try {
			await clearChatRoom(auth.token, current);
			lines = [];
			toast.push(t('admin.chat.cleared'), 'success');
		} catch (err) {
			await failed(err, t('admin.chat.could_not_clear'));
		}
	}

	async function send(e: SubmitEvent) {
		e.preventDefault();
		const said = text.trim();
		if (!auth.token || !current || !said) return;
		text = '';
		try {
			await chatAction(auth.token, current, 'say', said);
			await poll();
		} catch (err) {
			text = said;
			await failed(err, t('admin.common.could_not_send_it'));
		}
	}

	async function removeOneliner(o: Oneliner) {
		if (!auth.token || !confirm(t('admin.chat.delete_text_by_username', { TEXT: o.text, USERNAME: o.username }))) return;
		try {
			await deleteOneliner(auth.token, o.id);
			oneliners = oneliners.filter((x) => x.id !== o.id);
		} catch (err) {
			await failed(err, t('admin.common.could_not_delete_it'));
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
		settings.find((r) => r.name === name)?.title ?? (name === 'main' ? t('common.teleconference') : name.startsWith('page-') ? t('admin.chat.v_page', { V: name.slice(5) }) : name);
	const bridged = (name: string) => {
		const r = settings.find((x) => x.name === name);
		return [r?.discord_channel && 'Discord', r?.matrix_room && 'Matrix'].filter(Boolean).join(', ');
	};
	const who = (p: ChatPresence[]) => p.map((x) => (x.source === 'web' ? t('admin.chat.username_web', { USERNAME: x.username }) : `${x.username} (${x.source})`)).join(', ');
</script>

<div class="mb-6">
	<h1 class="page-title">{t('admin.common.chat_one_liners')}</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">
		{t('admin.chat.talk_with_the_callers_on')}
	</p>
</div>

<div class="grid gap-4 lg:grid-cols-[16rem_1fr]">
	<section class="card p-0">
		<h2 class="card-label px-4 pt-4 pb-2">{t('web.common.rooms')}</h2>
		{#each rooms as r (r.name)}
			<button
				class="flex w-full flex-col items-start gap-0.5 border-t border-line px-4 py-2.5 text-left hover:bg-surface {current === r.name ? 'bg-surface' : ''}"
				onclick={() => open(r.name)}
			>
				<span class="flex w-full items-center gap-2">
					<span class="flex-1 truncate {r.paging ? 'font-semibold text-fuchsia-300' : 'text-ink-strong'}">{label(r.name)}</span>
					{#if bridged(r.name)}<span class="text-[11px] text-indigo-400" title={t('admin.chat.bridged')}>↔ {bridged(r.name)}</span>{/if}
					{#if r.present.length}<span class="r-badge rounded-full bg-accent px-1.5 text-[11px] text-black">{r.present.length}</span>{/if}
				</span>
				{#if r.paging}
					<span class="text-xs text-fuchsia-300">{t('admin.chat.waiting_for_you')}</span>
				{:else if r.last_line}
					<span class="w-full truncate text-xs text-faint">{r.last_line.username}: {r.last_line.text || r.last_line.kind}</span>
				{/if}
			</button>
		{/each}
	</section>

	<section class="card flex min-h-[28rem] flex-col p-0">
		{#if !current}
			<p class="m-auto text-sm text-muted">{t('web.common.pick_a_room')}</p>
		{:else}
			<div class="flex items-baseline gap-3 border-b border-line px-4 py-3">
				<h2 class="font-semibold text-ink-strong">{label(current)}</h2>
				<span class="min-w-0 flex-1 truncate text-xs text-muted">{present.length ? t('common.here_names', { NAMES: who(present) }) : t('admin.chat.nobody_here')}</span>
				<button class="btn-secondary btn-xs hover:!border-red-400 hover:!text-red-400" onclick={clearRoom} title={t('admin.chat.clear_hint')}>{t('admin.chat.clear')}</button>
				<button class="btn-secondary btn-xs" onclick={leave}>{t('admin.common.leave')}</button>
			</div>
			<div bind:this={box} class="h-[24rem] flex-1 overflow-y-auto px-4 py-3 font-mono text-[13px] leading-relaxed">
				{#each lines as l (l.id)}
					<div>
						<span class="text-faint">{time(l.at)}</span>
						{#if l.kind === 'join'}
							<span class="text-emerald-500">{t('common.username_joined_source', { USERNAME: l.username, SOURCE: l.source })}</span>
						{:else if l.kind === 'leave'}
							<span class="text-emerald-700">{t('common.username_left', { USERNAME: l.username })}</span>
						{:else if l.kind === 'page'}
							<span class="font-semibold text-fuchsia-300">{t('admin.chat.username_paged_you_text', { USERNAME: l.username, TEXT: l.text })}</span>
						{:else}
							{#if l.source === 'discord' || l.source === 'matrix'}
								<span class="text-indigo-400">{l.username}@{l.source}:</span>
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
				<input class="field min-w-0 flex-1" maxlength="400" placeholder={t('web.common.say_something')} bind:value={text} autofocus />
				<button type="submit" class="btn-primary btn-sm" disabled={!text.trim()}>{t('web.common.send')}</button>
			</form>
		{/if}
	</section>
</div>

<div class="mt-4">
	<ChatSettings onchange={(r) => (settings = r)} />
</div>

<section class="card mt-4">
	<h2 class="card-label mb-3">{t('common.one_liners')}</h2>
	{#if oneliners.length === 0}
		<p class="text-sm text-muted">{t('admin.chat.the_wall_is_empty')}</p>
	{:else}
		<table class="w-full text-left text-sm">
			<tbody class="divide-y divide-slate-800">
				{#each [...oneliners].reverse() as o (o.id)}
					<tr>
						<td class="py-1.5 text-xs whitespace-nowrap text-muted">{new Date(o.at).toLocaleString(i18n.locale)}</td>
						<td class="py-1.5 text-accent">{o.username}</td>
						<td class="py-1.5 text-ink">{o.text}</td>
						<td class="py-1.5 text-right"><button class="btn-secondary btn-xs" onclick={() => removeOneliner(o)}>{t('web.common.delete')}</button></td>
					</tr>
				{/each}
			</tbody>
		</table>
	{/if}
</section>
