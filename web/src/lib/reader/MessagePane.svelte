<script lang="ts">
	// One echomail message: reading it marks it read on the BBS, as
	// everywhere else. Prev/Next and swiping move through its area.
	import { t } from '$lib/i18n.svelte';
	import { toast } from '$lib/toast.svelte';
	import { getBBSMessage, getBBSThread, listBBSMessageAreas, type BBSMessage, type ThreadEntry } from '$lib/api';
	import ThreadTree from '$lib/ThreadTree.svelte';
	import { sendOrQueue } from '$lib/reader/offline.svelte';
	import { readerToken, readerAuthFailed, errorText } from '$lib/reader/session';
	import ReadView from '$lib/reader/ReadView.svelte';
	import ComposeSheet from '$lib/reader/ComposeSheet.svelte';
	import { quoteText } from '$lib/reader/quote';

	let {
		id,
		onOpen,
		onBack,
		onRead
	}: {
		id: number;
		/** Show another message of the area (Prev/Next). */
		onOpen: (id: number) => void;
		/** Back to the list, given the message's area; none when beside it. */
		onBack?: (areaId: number) => void;
		/** It was loaded, so now it's read; with its area. */
		onRead?: (areaId: number) => void;
	} = $props();

	let message = $state<BBSMessage | null>(null);
	let areaName = $state('');
	let error = $state<string | null>(null);

	let replying = $state(false);
	let to = $state('');
	let subject = $state('');
	let body = $state('');
	let sending = $state(false);
	let thread = $state<ThreadEntry[]>([]);

	async function load(messageId: number) {
		const token = await readerToken();
		if (!token) return;
		try {
			message = await getBBSMessage(token, messageId);
			thread = [];
			// Offline it just isn't there.
			getBBSThread(token, messageId)
				.then((t) => {
					if (message?.id === messageId) thread = t.messages.map((e) => (e.id === messageId ? { ...e, read: true } : e));
				})
				.catch(() => {});
			const areas = await listBBSMessageAreas(token);
			areaName = areas.find((a) => a.id === message?.area_id)?.name ?? '';
			error = null;
			replying = false;
			if (message) onRead?.(message.area_id);
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			error = errorText(err, t('web.msg.load_failed'));
		}
	}

	$effect(() => {
		load(id);
	});

	function startReply() {
		if (!message) return;
		to = message.from_name;
		subject = message.subject.startsWith('Re: ') ? message.subject : `Re: ${message.subject}`;
		body = quoteText(message.body, message.from_name, message.to_name) + '\n\n';
		replying = true;
	}

	async function send() {
		const token = await readerToken();
		if (!token || !message) return;
		sending = true;
		try {
			const how = await sendOrQueue(token, { kind: 'echo', areaId: message.area_id, to, subject, body, replyTo: message.id });
			replying = false;
			toast.push(how === 'queued' ? t('web.reader.queued_reply') : t('web.msg.reply_posted'), 'success');
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			toast.push(errorText(err, t('web.msg.reply_failed')), 'error');
		} finally {
			sending = false;
		}
	}
</script>

{#if error}
	<p class="r-note text-red-400">{error}</p>
{:else if message}
	<ReadView
		title={areaName}
		from={message.from_name}
		to={message.to_name}
		postedAt={message.posted_at}
		subject={message.subject}
		bodyHtml={message.body_html}
		preformatted={message.preformatted}
		grid={message.grid}
		onBack={onBack ? () => message && onBack?.(message.area_id) : undefined}
		onPrev={message.prev_id ? () => message?.prev_id && onOpen(message.prev_id) : undefined}
		onNext={message.next_id ? () => message?.next_id && onOpen(message.next_id) : undefined}
		onReply={startReply}
	>
		{#snippet after()}
			{#if thread.length > 1 && message}
				<section class="no-swipe mt-6 border-t border-line pt-3">
					<h2 class="card-label mb-1.5">{t('web.reader.thread_n', { COUNT: thread.length })}</h2>
					<ThreadTree entries={thread} current={message.id} onselect={(tid) => tid !== message?.id && onOpen(tid)} />
				</section>
			{/if}
		{/snippet}
	</ReadView>
{/if}

{#if replying && message}
	<ComposeSheet
		bind:to
		bind:subject
		bind:body
		busy={sending}
		onSend={send}
		onCancel={() => (replying = false)}
	/>
{/if}
