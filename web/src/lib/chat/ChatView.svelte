<script lang="ts">
	// One chat room for a caller (portal, reader): enters on open, polls
	// for new lines and who's here, leaves on close. The same room as
	// the teleconference on Telnet -- and Discord/Matrix where bridged.
	import { t } from '$lib/i18n.svelte';
	import { onMount, onDestroy, tick } from 'svelte';
	import { getBBSChatRoom, bbsChatAction, ApiError, type ChatLine, type ChatPresence } from '$lib/api';

	let {
		room,
		token,
		me,
		compact = false,
		onFailed
	}: {
		room: string;
		/** The caller's token, fresh each time (the reader refreshes it). */
		token: () => Promise<string | null> | string | null;
		/** The caller's handle: their own lines stand out. */
		me: string;
		/** The reader app: full height, larger input. */
		compact?: boolean;
		onFailed?: (err: unknown) => void;
	} = $props();

	let lines = $state<ChatLine[]>([]);
	let present = $state<ChatPresence[]>([]);
	let text = $state('');
	let sending = $state(false);
	let box = $state<HTMLDivElement | undefined>();
	let timer: ReturnType<typeof setInterval> | undefined;
	let polling = false;
	let left = false;

	const time = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
	const bridged = (l: ChatLine) => l.source === 'discord' || l.source === 'matrix';
	const where = (p: ChatPresence) => (p.source === 'web' ? 'web' : p.source.startsWith('node') ? 'telnet' : p.source);

	async function poll() {
		if (polling || left) return;
		polling = true;
		try {
			const t = await token();
			if (!t) return;
			const last = lines.length ? lines[lines.length - 1].id : 0;
			const st = await getBBSChatRoom(t, room, last);
			present = st.present;
			if (st.lines.length) {
				const atBottom = !box || box.scrollHeight - box.scrollTop - box.clientHeight < 60;
				lines = [...lines, ...st.lines].slice(-500);
				await tick();
				if (atBottom) box?.scrollTo({ top: box.scrollHeight });
			}
		} catch (err) {
			onFailed?.(err);
		} finally {
			polling = false;
		}
	}

	async function leave() {
		if (left) return;
		left = true;
		clearInterval(timer);
		const t = await token();
		if (t) bbsChatAction(t, room, 'leave').catch(() => {});
	}

	onMount(() => {
		(async () => {
			const t = await token();
			if (!t) return;
			try {
				await bbsChatAction(t, room, 'enter');
			} catch (err) {
				onFailed?.(err);
				return;
			}
			await poll();
			box?.scrollTo({ top: box.scrollHeight });
			timer = setInterval(poll, 1500);
		})();
		const onHide = () => leave();
		window.addEventListener('pagehide', onHide);
		return () => window.removeEventListener('pagehide', onHide);
	});
	onDestroy(leave);

	async function send(e: SubmitEvent) {
		e.preventDefault();
		const say = text.trim();
		if (!say || sending) return;
		sending = true;
		text = '';
		try {
			const t = await token();
			if (!t) return;
			await bbsChatAction(t, room, 'say', say);
			await poll();
			box?.scrollTo({ top: box.scrollHeight });
		} catch (err) {
			text = say;
			if (err instanceof ApiError && err.status !== 401) alert(err.message);
			else onFailed?.(err);
		} finally {
			sending = false;
		}
	}
</script>

<div class="flex min-h-0 flex-1 flex-col">
	<div class="truncate border-b border-line px-4 py-2 text-xs text-muted">
		{#if present.length}
			{t('web.chat.here', { NAMES: present.map((p) => `${p.username} (${where(p)})`).join(', ') })}
		{:else}
			{t('web.chat.nobody')}
		{/if}
	</div>
	<div bind:this={box} class="min-h-0 flex-1 overflow-y-auto px-4 py-3 {compact ? 'text-[15px]' : 'text-[13.5px]'} leading-relaxed">
		{#if lines.length === 0}
			<p class="text-sm text-faint">{t('web.chat.empty')}</p>
		{/if}
		{#each lines as l (l.id)}
			<div class="break-words">
				<span class="font-mono text-[11px] text-faint">{time(l.at)}</span>
				{#if l.kind === 'join'}
					<span class="text-emerald-500/80">{t('web.chat.came_in', { NAME: l.username })}</span>
				{:else if l.kind === 'leave'}
					<span class="text-faint">{t('web.chat.left', { NAME: l.username })}</span>
				{:else}
					<span class="font-medium {l.username === me && l.source === 'web' ? 'text-accent' : bridged(l) ? 'text-indigo-400' : 'text-ink-strong'}"
						>{l.username}{bridged(l) ? `@${l.source}` : ''}:</span
					>
					<span class="text-ink">{l.text}</span>
				{/if}
			</div>
		{/each}
	</div>
	<form
		class="flex gap-2 border-t border-line p-3"
		style={compact ? 'padding-bottom: max(0.75rem, env(safe-area-inset-bottom))' : ''}
		onsubmit={send}
	>
		<input class="field min-w-0 flex-1" maxlength="400" placeholder={t('web.chat.say')} bind:value={text} enterkeyhint="send" />
		<button type="submit" class="btn-primary btn-sm" disabled={!text.trim() || sending}>{t('web.chat.send')}</button>
	</form>
</div>
