<script lang="ts">
	import { t } from '$lib/i18n.svelte';
	import { formatDateTime } from '$lib/datetime';
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import AnsiArt from '$lib/AnsiArt.svelte';
	import { quoteText } from '$lib/reader/quote';
	import { getBBSNetmail, sendBBSNetmail, deleteBBSNetmail, ApiError, type BBSNetmail } from '$lib/api';

	// $derived (not a plain const) so Prev/Next navigation -- which
	// resolves to this same component instance rather than remounting
	// it -- actually picks up the new id (see the $effect below).
	let messageId = $derived(Number(page.params.id));

	let message = $state<BBSNetmail | null>(null);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let replying = $state(false);
	let replySubject = $state('');
	let replyBody = $state('');
	let sending = $state(false);

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			bbsAuth.clear();
			await goto('/login');
			return true;
		}
		return false;
	}

	async function load(id: number) {
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		loaded = false;
		replying = false;
		try {
			message = await getBBSNetmail(bbsAuth.token, id);
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : t('web.msg.load_failed');
		} finally {
			loaded = true;
		}
	}

	$effect(() => {
		load(messageId);
	});

	function onKeydown(e: KeyboardEvent) {
		if (replying || (e.target as HTMLElement)?.tagName === 'TEXTAREA') return;
		if (e.key === 'ArrowLeft' && message?.prev_id) goto(`/netmail/${message.prev_id}`, { replaceState: true });
		if (e.key === 'ArrowRight' && message?.next_id) goto(`/netmail/${message.next_id}`, { replaceState: true });
	}

	onMount(() => {
		window.addEventListener('keydown', onKeydown);
	});
	onDestroy(() => {
		window.removeEventListener('keydown', onKeydown);
	});

	function startReply() {
		if (!message) return;
		replySubject = message.subject.startsWith('Re: ') ? message.subject : `Re: ${message.subject}`;
		replyBody = quoteText(message.body, message.from_name, message.to_name) + '\n\n';
		replying = true;
	}

	async function sendReply() {
		if (!bbsAuth.token || !message) return;
		// A mail that came in by email goes back to its address.
		const to = message.email || message.from_address || message.from_name;
		// When the original came from an FTN address, from_name is that
		// remote user's real name -- pass it along as the reply's
		// recipient name so it isn't lost (see the compose page's
		// matching field for why an FTN address alone isn't enough).
		const toName = message.from_address ? message.from_name : '';
		sending = true;
		try {
			await sendBBSNetmail(bbsAuth.token, to, replySubject, replyBody, toName, false, message.id);
			replying = false;
			toast.push(t('web.netmail.reply_sent'), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('web.netmail.reply_failed'), 'error');
		} finally {
			sending = false;
		}
	}

	async function remove() {
		if (!bbsAuth.token || !message) return;
		// There's only one copy of a netmail message, not a separate one
		// per side -- deleting it from your own Sent list removes it
		// from the recipient's Inbox too, so a local recipient gets an
		// extra warning about that (an FTN recipient has no local Inbox
		// copy to lose, so the plain confirmation is enough there).
		const question =
			!message.is_recipient && message.to_address === ''
				? t('web.netmail.delete_both')
				: t('web.netmail.delete');
		if (!confirm(question)) return;
		try {
			await deleteBBSNetmail(bbsAuth.token, message.id);
			toast.push(t('web.netmail.deleted'), 'success');
			await goto('/netmail');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('web.netmail.delete_failed'), 'error');
		}
	}
</script>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-muted">{t('web.common.loading')}</p>
{:else if message}
	{@const peer = message.is_recipient ? message.from_name : message.to_name}
	<div class="flex items-center justify-between">
		<a href="/netmail" class="back-link">&larr; {t('web.nav.netmail')}</a>
		<div class="flex items-center gap-2">
			<button
				class="btn-secondary px-3.5 py-2 text-xs"
				disabled={!message.prev_id}
				onclick={() => message?.prev_id && goto(`/netmail/${message.prev_id}`, { replaceState: true })}
			>
				&larr; {t('web.common.prev')}
			</button>
			<button
				class="btn-secondary px-3.5 py-2 text-xs"
				disabled={!message.next_id}
				onclick={() => message?.next_id && goto(`/netmail/${message.next_id}`, { replaceState: true })}
			>
				{t('web.common.next')} &rarr;
			</button>
		</div>
	</div>

	<div class="mt-7 mb-5 flex items-center justify-between gap-3.5">
		<div class="flex min-w-0 items-center gap-3.5">
			<div
				class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-surface font-mono text-[13px] text-accent"
				aria-hidden="true"
			>
				{peer.slice(0, 1).toUpperCase()}
			</div>
			<div class="min-w-0">
				<h1 class="text-lg font-semibold text-ink-strong">{message.subject}</h1>
				<div class="mt-0.5 text-[12.5px] text-muted">
					{#if message.is_recipient}
						<span class="text-slate-400">{message.from_name}</span>
						{#if message.from_address}<span class="font-mono text-faint">{message.from_address}</span>{/if}
						{#if message.email && message.email !== message.from_name}<span class="font-mono text-faint">&lt;{message.email}&gt;</span>{/if}
					{:else}
						{t('web.msg.to_lower')} <span class="text-slate-400">{message.to_name}</span>
						{#if message.to_address}<span class="font-mono text-faint">{message.to_address}</span>{/if}
						{#if message.email_status}
							<span
								class="ml-1 rounded px-1.5 py-0.5 text-[11px] {message.email_status === 'failed'
									? 'bg-red-500/15 text-red-400'
									: message.email_status === 'sent'
										? 'bg-emerald-500/15 text-emerald-400'
										: 'bg-amber-500/15 text-amber-400'}"
								title={message.email_error || ''}>{message.email_status === 'failed'
									? t('web.netmail.email_failed')
									: message.email_status === 'sent'
										? t('web.netmail.email_sent')
										: t('web.netmail.email_queued')}</span
							>
						{/if}
					{/if}
					&middot; {formatDateTime(message.posted_at)}
				</div>
			</div>
		</div>
		<button
			class="btn-secondary shrink-0 px-3.5 py-2 text-xs hover:!border-red-400 hover:!text-red-400"
			onclick={remove}
		>
			{t('web.common.delete')}
		</button>
	</div>

	{#if message.preformatted && message.grid}
		<!-- As wide as the column allows, and never a scrollbar. -->
		<div class="ansi-panel">
			<AnsiArt grid={message.grid} fit maxZoom={1.3} />
		</div>
	{:else}
		<!-- See messages/[id]/+page.svelte's matching branch for why this
		     stays monospace even for a "plain" message. -->
		<div class="body-panel whitespace-pre-wrap">{@html message.body_html}</div>
	{/if}

	{#if message.is_recipient && !replying}
		<button class="btn-primary mt-5 px-5" onclick={startReply}>{t('web.msg.reply')}</button>
	{/if}

	{#if replying}
		<form
			class="mt-6 flex flex-col gap-3.5"
			onsubmit={(e) => {
				e.preventDefault();
				sendReply();
			}}
		>
			<label class="flex flex-col gap-2">
				<span class="card-label">{t('web.msg.subject')}</span>
				<input class="field" bind:value={replySubject} />
			</label>
			<label class="flex flex-col gap-2">
				<span class="card-label">{t('web.msg.message')}</span>
				<textarea
					class="body-panel h-56 resize-y outline-none focus:border-accent"
					bind:value={replyBody}
				></textarea>
			</label>
			<div class="flex justify-end gap-2.5">
				<button type="button" class="btn-secondary" onclick={() => (replying = false)}>{t('web.common.cancel')}</button>
				<button type="submit" class="btn-primary" disabled={sending || !replySubject || !replyBody}>
					{sending ? t('web.netmail.sending') : t('web.netmail.send_reply')}
				</button>
			</div>
		</form>
	{/if}
{/if}

<style>
	@keyframes -global-ansi-blink {
		50% {
			opacity: 0;
		}
	}
</style>
