<script lang="ts">
	// Netmail received, newest first; New writes one.
	import { t } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { listBBSNetmail, type BBSNetmailSummary } from '$lib/api';
	import { toast } from '$lib/toast.svelte';
	import { sendOrQueue } from '$lib/reader/offline.svelte';
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
			const how = await sendOrQueue(token, { kind: 'netmail', to: to.trim(), toName: toName.trim(), subject, body });
			composing = false;
			toast.push(how === 'queued' ? t('web.reader.queued_netmail') : t('web.netmail.sent_ok'), 'success');
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			toast.push(errorText(err, t('web.netmail.send_failed')), 'error');
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
			error = errorText(err, t('web.netmail.load_failed'));
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
		<button class="r-btn text-3xl leading-none" onclick={onBack} aria-label={t('web.common.back')}>‹</button>
	{/if}
	<span class="r-title">{t('web.nav.netmail')}</span>
	<button class="r-btn text-sm" onclick={startNew}>{t('web.reader.new')}</button>
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
		<p class="r-note">{t('web.netmail.none')}</p>
	{/each}
{/if}

{#if composing}
	<ComposeSheet
		heading={t('web.netmail.new')}
		bind:to
		bind:toName
		askToName
		toPlaceholder={t('web.reader.netmail_to')}
		bind:subject
		bind:body
		busy={sending}
		onSend={send}
		onCancel={() => (composing = false)}
	/>
{/if}
