<script lang="ts">
	// One area's messages, oldest first, opened at the first unread;
	// "Read" starts there. New writes a message, "All read" marks the
	// whole area read (for areas like FSX_DAT nobody reads one by one).
	import {
		listBBSMessages,
		listBBSMessageAreas,
		getFirstUnreadMessagePosition,
		markBBSAreaRead,
		postBBSMessage,
		type BBSMessageSummary
	} from '$lib/api';
	import { toast } from '$lib/toast.svelte';
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

	const PAGE = 40;

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
			error = errorText(err, 'Could not load the messages.');
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
			error = errorText(err, 'Could not load more.');
		}
	}

	async function markAllRead() {
		if (!confirm(`Mark all of ${title || 'this area'} as read?`)) return;
		const token = await readerToken();
		if (!token) return;
		try {
			const res = await markBBSAreaRead(token, areaId);
			messages = messages.map((m) => ({ ...m, unread: false }));
			toast.push(`${res.marked} marked as read.`, 'success');
			onChanged?.();
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			toast.push(errorText(err, 'Could not mark the area read.'), 'error');
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
			await postBBSMessage(token, areaId, to.trim(), subject, body);
			composing = false;
			toast.push('Message posted.', 'success');
			await load(areaId);
			onChanged?.();
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			toast.push(errorText(err, 'Could not post the message.'), 'error');
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
		<button class="r-btn text-3xl leading-none" onclick={onBack} aria-label="Back">‹</button>
	{/if}
	<span class="r-title">{title}</span>
	<button class="r-btn text-sm" onclick={startNew}>New</button>
	{#if firstUnread}
		<button class="r-btn text-sm" onclick={markAllRead}>All read</button>
		<button class="r-btn text-sm font-semibold" onclick={() => firstUnread && onOpen(firstUnread.id)}>Read</button>
	{/if}
</header>

{#if error}
	<p class="r-note text-red-400">{error}</p>
{:else if loaded}
	{#if offset > 0}
		<button class="r-row justify-center text-sm text-accent" onclick={() => more(true)}>Earlier messages</button>
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
		<p class="r-note">No messages.</p>
	{/each}
	{#if offset + messages.length < total}
		<button class="r-row justify-center text-sm text-accent" onclick={() => more(false)}>More</button>
	{/if}
{/if}

{#if composing}
	<ComposeSheet
		heading="New message"
		bind:to
		bind:subject
		bind:body
		busy={sending}
		onSend={send}
		onCancel={() => (composing = false)}
	/>
{/if}
