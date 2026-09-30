<script lang="ts">
	// Netmail received, newest first; New writes one.
	import { onMount } from 'svelte';
	import { listBBSNetmail, sendBBSNetmail, type BBSNetmailSummary } from '$lib/api';
	import { toast } from '$lib/toast.svelte';
	import { readerToken, readerAuthFailed, errorText, shortDate } from '$lib/reader/session';
	import ComposeSheet from '$lib/reader/ComposeSheet.svelte';
	import type { Snippet } from 'svelte';

	let {
		selectedId = null,
		onOpen,
		headerStart,
		onBack
	}: {
		selectedId?: number | null;
		onOpen: (id: number) => void;
		onBack?: () => void;
		/** Shown first in the top bar (the split view's panel toggle). */
		headerStart?: Snippet;
	} = $props();

	let mails = $state<BBSNetmailSummary[]>([]);
	let error = $state<string | null>(null);
	let loaded = $state(false);

	let composing = $state(false);
	let to = $state('');
	let toName = $state('');
	let subject = $state('');
	let body = $state('');
	let sending = $state(false);

	function startNew() {
		to = toName = subject = body = '';
		composing = true;
	}

	async function send() {
		const token = await readerToken();
		if (!token) return;
		sending = true;
		try {
			await sendBBSNetmail(token, to.trim(), subject, body, toName.trim());
			composing = false;
			toast.push('Netmail sent.', 'success');
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			toast.push(errorText(err, 'Could not send the netmail.'), 'error');
		} finally {
			sending = false;
		}
	}

	onMount(async () => {
		const token = await readerToken();
		if (!token) return;
		try {
			mails = await listBBSNetmail(token);
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			error = errorText(err, 'Could not load the netmail.');
		} finally {
			loaded = true;
		}
	});

	$effect(() => {
		const id = selectedId;
		if (id != null) {
			const m = mails.find((x) => x.id === id);
			if (m?.unread) m.unread = false;
		}
	});
</script>

<header class="r-bar">
	{@render headerStart?.()}
	{#if onBack}
		<button class="r-btn text-3xl leading-none" onclick={onBack} aria-label="Back">‹</button>
	{/if}
	<span class="r-title">Netmail</span>
	<button class="r-btn text-sm" onclick={startNew}>New</button>
</header>

{#if error}
	<p class="r-note text-red-400">{error}</p>
{:else if loaded}
	{#each mails as m (m.id)}
		<button class="r-row {m.id === selectedId ? 'bg-surface' : ''}" onclick={() => onOpen(m.id)}>
			<span class="h-2 w-2 shrink-0 rounded-full {m.unread ? 'bg-accent' : ''}"></span>
			<span class="min-w-0 flex-1">
				<span class="block truncate {m.unread ? 'font-semibold text-ink-strong' : 'text-ink-soft'}">{m.subject}</span>
				<span class="block truncate text-xs text-muted">{m.from_name}</span>
			</span>
			<span class="shrink-0 text-xs text-faint">{shortDate(m.posted_at)}</span>
		</button>
	{:else}
		<p class="r-note">No netmail.</p>
	{/each}
{/if}

{#if composing}
	<ComposeSheet
		heading="New netmail"
		bind:to
		bind:toName
		askToName
		toPlaceholder="Username, or FTN address like 21:3/100"
		bind:subject
		bind:body
		busy={sending}
		onSend={send}
		onCancel={() => (composing = false)}
	/>
{/if}
