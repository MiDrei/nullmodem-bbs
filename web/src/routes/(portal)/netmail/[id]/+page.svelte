<script lang="ts">
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
			loadError = err instanceof ApiError ? err.message : 'Could not load message.';
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
		replyBody = quoteText(message.body, message.from_name) + '\n\n';
		replying = true;
	}

	async function sendReply() {
		if (!bbsAuth.token || !message) return;
		const to = message.from_address || message.from_name;
		// When the original came from an FTN address, from_name is that
		// remote user's real name -- pass it along as the reply's
		// recipient name so it isn't lost (see the compose page's
		// matching field for why an FTN address alone isn't enough).
		const toName = message.from_address ? message.from_name : '';
		sending = true;
		try {
			await sendBBSNetmail(bbsAuth.token, to, replySubject, replyBody, toName);
			replying = false;
			toast.push('Reply sent.', 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not send reply.', 'error');
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
				? "Delete this message? It'll also disappear from the recipient's inbox."
				: 'Delete this message?';
		if (!confirm(question)) return;
		try {
			await deleteBBSNetmail(bbsAuth.token, message.id);
			toast.push('Message deleted.', 'success');
			await goto('/netmail');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not delete message.', 'error');
		}
	}
</script>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-muted">Loading…</p>
{:else if message}
	{@const peer = message.is_recipient ? message.from_name : message.to_name}
	<div class="flex items-center justify-between">
		<a href="/netmail" class="back-link">&larr; Netmail</a>
		<div class="flex items-center gap-2">
			<button
				class="btn-secondary px-3.5 py-2 text-xs"
				disabled={!message.prev_id}
				onclick={() => message?.prev_id && goto(`/netmail/${message.prev_id}`, { replaceState: true })}
			>
				&larr; Prev
			</button>
			<button
				class="btn-secondary px-3.5 py-2 text-xs"
				disabled={!message.next_id}
				onclick={() => message?.next_id && goto(`/netmail/${message.next_id}`, { replaceState: true })}
			>
				Next &rarr;
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
					{:else}
						to <span class="text-slate-400">{message.to_name}</span>
						{#if message.to_address}<span class="font-mono text-faint">{message.to_address}</span>{/if}
					{/if}
					&middot; {formatDateTime(message.posted_at)}
				</div>
			</div>
		</div>
		<button
			class="btn-secondary shrink-0 px-3.5 py-2 text-xs hover:!border-red-400 hover:!text-red-400"
			onclick={remove}
		>
			Delete
		</button>
	</div>

	{#if message.preformatted && message.grid}
		<div class="ansi-panel overflow-x-auto">
			<AnsiArt grid={message.grid} />
		</div>
	{:else}
		<!-- See messages/[id]/+page.svelte's matching branch for why this
		     stays monospace even for a "plain" message. -->
		<div class="body-panel whitespace-pre-wrap">{@html message.body_html}</div>
	{/if}

	{#if message.is_recipient && !replying}
		<button class="btn-primary mt-5 px-5" onclick={startReply}>Reply</button>
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
				<span class="card-label">Subject</span>
				<input class="field" bind:value={replySubject} />
			</label>
			<label class="flex flex-col gap-2">
				<span class="card-label">Message</span>
				<textarea
					class="body-panel h-56 resize-y outline-none focus:border-accent"
					bind:value={replyBody}
				></textarea>
			</label>
			<div class="flex justify-end gap-2.5">
				<button type="button" class="btn-secondary" onclick={() => (replying = false)}>Cancel</button>
				<button type="submit" class="btn-primary" disabled={sending || !replySubject || !replyBody}>
					{sending ? 'Sending…' : 'Send reply'}
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
