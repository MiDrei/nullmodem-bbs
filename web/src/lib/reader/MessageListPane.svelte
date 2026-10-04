<script lang="ts">
	// One area's messages, oldest first, opened at the first unread;
	// "Read" starts there. New writes a message, "All read" marks the
	// whole area read (for areas like FSX_DAT nobody reads one by one).
	import { t } from '$lib/i18n.svelte';
	import {
		listBBSMessages,
		listBBSMessageAreas,
		getFirstUnreadMessagePosition,
		markBBSAreaRead,
		type BBSMessageSummary
	} from '$lib/api';
	import { toast } from '$lib/toast.svelte';
	import { LIST_PAGE, sendOrQueue } from '$lib/reader/offline.svelte';
	import { readerToken, readerAuthFailed, errorText, shortDate } from '$lib/reader/session';
	import ComposeSheet from '$lib/reader/ComposeSheet.svelte';
	import type { Snippet } from 'svelte';

	let {
		areaId,
		selectedId = null,
		onOpen,
		headerStart,
		onBack,
		onChanged
	}: {
		areaId: number;
		selectedId?: number | null;
		onOpen: (id: number) => void;
		/** Back to the area list; none when it's beside this. */
		onBack?: () => void;
		/** Shown first in the top bar (the split view's panel toggle). */
		headerStart?: Snippet;
		/** Something was posted or marked read: counts elsewhere changed. */
		onChanged?: () => void;
	} = $props();

	const PAGE = LIST_PAGE;

	let title = $state('');
	let messages = $state<BBSMessageSummary[]>([]);
	let offset = $state(0);
	let total = $state(0);
	let error = $state<string | null>(null);
	let loaded = $state(false);
	let firstUnread = $derived(messages.find((m) => m.unread));

	async function load(id: number) {
		const token = await readerToken();
		if (!token) return;
		loaded = false;
		try {
			const [pos, areas] = await Promise.all([getFirstUnreadMessagePosition(token, id), listBBSMessageAreas(token)]);
			title = areas.find((a) => a.id === id)?.name ?? '';
			// A few already-read ones above the first unread, for context.
			offset = Math.max(0, pos.position - 3);
			const p = await listBBSMessages(token, id, PAGE, offset);
			messages = p.messages;
			total = p.total;
			error = null;
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			error = errorText(err, t('web.msgs.load_failed'));
		} finally {
			loaded = true;
		}
	}

	async function more(older: boolean) {
		const token = await readerToken();
		if (!token) return;
		const from = older ? Math.max(0, offset - PAGE) : offset + messages.length;
		const limit = older ? offset - from : PAGE;
		try {
			const p = await listBBSMessages(token, areaId, limit, from);
			if (older) {
				messages = [...p.messages, ...messages];
				offset = from;
			} else {
				messages = [...messages, ...p.messages];
			}
			total = p.total;
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			error = errorText(err, t('web.reader.more_failed'));
		}
	}

	async function markAllRead() {
		if (!confirm(t('web.reader.mark_read_confirm', { AREA: title || t('web.search.this_area') }))) return;
		const token = await readerToken();
		if (!token) return;
		try {
			const res = await markBBSAreaRead(token, areaId);
			messages = messages.map((m) => ({ ...m, unread: false }));
			toast.push(t('web.reader.marked', { COUNT: res.marked }), 'success');
			onChanged?.();
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			toast.push(errorText(err, t('web.reader.mark_failed')), 'error');
		}
	}

	let composing = $state(false);
	let to = $state('All');
	let subject = $state('');
	let body = $state('');
	let sending = $state(false);

	function startNew() {
		to = 'All';
		subject = '';
		body = '';
		composing = true;
	}

	async function send() {
		const token = await readerToken();
		if (!token) return;
		sending = true;
		try {
			const how = await sendOrQueue(token, { kind: 'echo', areaId, to: to.trim(), subject, body });
			composing = false;
			if (how === 'queued') {
				toast.push(t('web.reader.queued_message'), 'success');
				return;
			}
			toast.push(t('web.msgs.posted'), 'success');
			await load(areaId);
			onChanged?.();
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			toast.push(errorText(err, t('web.msgs.post_failed')), 'error');
		} finally {
			sending = false;
		}
	}

	$effect(() => {
		load(areaId);
	});

	// Opened beside the list: it's read now.
	$effect(() => {
		const id = selectedId;
		if (id != null) {
			const m = messages.find((x) => x.id === id);
			if (m?.unread) m.unread = false;
		}
	});
</script>

<header class="r-bar">
	{@render headerStart?.()}
	{#if onBack}
		<button class="r-btn text-3xl leading-none" onclick={onBack} aria-label={t('web.common.back')}>‹</button>
	{/if}
	<span class="r-title">{title}</span>
	<button class="r-btn text-sm" onclick={startNew}>{t('web.reader.new')}</button>
	{#if firstUnread}
		<button class="r-btn text-sm" onclick={markAllRead}>{t('web.reader.all_read')}</button>
		<button class="r-btn text-sm font-semibold" onclick={() => firstUnread && onOpen(firstUnread.id)}>{t('web.reader.read')}</button>
	{/if}
</header>

{#if error}
	<p class="r-note text-red-400">{error}</p>
{:else if loaded}
	{#if offset > 0}
		<button class="r-row justify-center text-sm text-accent" onclick={() => more(true)}>{t('web.reader.earlier')}</button>
	{/if}
	{#each messages as m (m.id)}
		<button class="r-row {m.id === selectedId ? 'bg-surface' : ''}" onclick={() => onOpen(m.id)}>
			<span class="h-2 w-2 shrink-0 rounded-full {m.unread ? 'bg-accent' : ''}"></span>
			<span class="min-w-0 flex-1">
				<span class="block truncate {m.unread ? 'font-semibold text-ink-strong' : 'text-ink-soft'}">{m.subject}</span>
				<span class="block truncate text-xs text-muted">{m.from_name} → {m.to_name}</span>
			</span>
			<span class="shrink-0 text-xs text-faint">{shortDate(m.posted_at)}</span>
		</button>
	{:else}
		<p class="r-note">{t('web.reader.no_messages')}</p>
	{/each}
	{#if offset + messages.length < total}
		<button class="r-row justify-center text-sm text-accent" onclick={() => more(false)}>{t('web.reader.more')}</button>
	{/if}
{/if}

{#if composing}
	<ComposeSheet
		heading={t('web.reader.new_message')}
		bind:to
		bind:subject
		bind:body
		busy={sending}
		onSend={send}
		onCancel={() => (composing = false)}
	/>
{/if}
