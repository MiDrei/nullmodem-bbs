<script lang="ts">
	// One netmail; a reply goes back to its sender.
	import { toast } from '$lib/toast.svelte';
	import { getBBSNetmail, type BBSNetmail } from '$lib/api';
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
		onOpen: (id: number) => void;
		onBack?: () => void;
		onRead?: () => void;
	} = $props();

	let mail = $state<BBSNetmail | null>(null);
	let error = $state<string | null>(null);

	let replying = $state(false);
	let to = $state('');
	let subject = $state('');
	let body = $state('');
	let sending = $state(false);

	async function load(mailId: number) {
		const token = await readerToken();
		if (!token) return;
		try {
			mail = await getBBSNetmail(token, mailId);
			error = null;
			replying = false;
			onRead?.();
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			error = errorText(err, 'Could not load the netmail.');
		}
	}

	$effect(() => {
		load(id);
	});

	function startReply() {
		if (!mail) return;
		// To an FTN sender: its address, with the name alongside.
		to = mail.from_address ? `${mail.from_name} @ ${mail.from_address}` : mail.from_name;
		subject = mail.subject.startsWith('Re: ') ? mail.subject : `Re: ${mail.subject}`;
		body = quoteText(mail.body, mail.from_name, mail.to_name) + '\n\n';
		replying = true;
	}

	async function send() {
		const token = await readerToken();
		if (!token || !mail) return;
		const target = mail.from_address || mail.from_name;
		const toName = mail.from_address ? mail.from_name : '';
		sending = true;
		try {
			const how = await sendOrQueue(token, { kind: 'netmail', to: target, toName, subject, body });
			replying = false;
			toast.push(how === 'queued' ? "You're offline: the reply goes out once you're back online." : 'Reply sent.', 'success');
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			toast.push(errorText(err, 'Could not send the reply.'), 'error');
		} finally {
			sending = false;
		}
	}
</script>

{#if error}
	<p class="r-note text-red-400">{error}</p>
{:else if mail}
	<ReadView
		title="Netmail"
		from={mail.from_address ? `${mail.from_name} (${mail.from_address})` : mail.from_name}
		to={mail.to_name}
		postedAt={mail.posted_at}
		subject={mail.subject}
		bodyHtml={mail.body_html}
		preformatted={mail.preformatted}
		grid={mail.grid}
		{onBack}
		onPrev={mail.prev_id ? () => mail?.prev_id && onOpen(mail.prev_id) : undefined}
		onNext={mail.next_id ? () => mail?.next_id && onOpen(mail.next_id) : undefined}
		onReply={mail.is_recipient ? startReply : undefined}
	/>
{/if}

{#if replying && mail}
	<ComposeSheet bind:to bind:subject bind:body toLocked busy={sending} onSend={send} onCancel={() => (replying = false)} />
{/if}
