<script lang="ts">
	// One echomail message: reading it marks it read on the BBS, as
	// everywhere else. Prev/Next and swiping move through its area.
	import { toast } from '$lib/toast.svelte';
	import { getBBSMessage, postBBSMessage, listBBSMessageAreas, type BBSMessage } from '$lib/api';
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

	async function load(messageId: number) {
		const token = await readerToken();
		if (!token) return;
		try {
			message = await getBBSMessage(token, messageId);
			const areas = await listBBSMessageAreas(token);
			areaName = areas.find((a) => a.id === message?.area_id)?.name ?? '';
			error = null;
			replying = false;
			if (message) onRead?.(message.area_id);
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			error = errorText(err, 'Could not load the message.');
		}
	}

	$effect(() => {
		load(id);
	});

	function startReply() {
		if (!message) return;
		to = message.from_name;
		subject = message.subject.startsWith('Re: ') ? message.subject : `Re: ${message.subject}`;
		body = quoteText(message.body, message.from_name) + '\n\n';
		replying = true;
	}

	async function send() {
		const token = await readerToken();
		if (!token || !message) return;
		sending = true;
		try {
			await postBBSMessage(token, message.area_id, to, subject, body);
			replying = false;
			toast.push('Reply posted.', 'success');
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			toast.push(errorText(err, 'Could not post the reply.'), 'error');
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
	/>
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
