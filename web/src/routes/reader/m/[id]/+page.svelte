<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { toast } from '$lib/toast.svelte';
	import { getBBSMessage, postBBSMessage, listBBSMessageAreas, type BBSMessage } from '$lib/api';
	import { readerToken, readerAuthFailed, errorText } from '$lib/reader/session';
	import ReadView from '$lib/reader/ReadView.svelte';
	import ReplySheet from '$lib/reader/ReplySheet.svelte';

	let id = $derived(Number(page.params.id));
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
			// Opening it marks it read on the BBS, as everywhere else.
			message = await getBBSMessage(token, messageId);
			if (!areaName) {
				const areas = await listBBSMessageAreas(token);
				areaName = areas.find((a) => a.id === message?.area_id)?.name ?? '';
			}
			error = null;
			window.scrollTo(0, 0);
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			error = errorText(err, 'Could not load the message.');
		}
	}

	$effect(() => {
		load(id);
	});

	function open(next?: number) {
		if (next) goto(`/reader/m/${next}`, { replaceState: true });
	}

	function startReply() {
		if (!message) return;
		to = message.from_name;
		subject = message.subject.startsWith('Re: ') ? message.subject : `Re: ${message.subject}`;
		body = '';
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
		onBack={() => goto(`/reader/area/${message?.area_id}`, { replaceState: true })}
		onPrev={message.prev_id ? () => open(message?.prev_id) : undefined}
		onNext={message.next_id ? () => open(message?.next_id) : undefined}
		onReply={startReply}
	/>
{/if}

{#if replying && message}
	<ReplySheet
		bind:to
		bind:subject
		bind:body
		quote={message.body}
		busy={sending}
		onSend={send}
		onCancel={() => (replying = false)}
	/>
{/if}

