<script lang="ts">
	import { formatDateTime } from '$lib/datetime';
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { avatarGradient, initials } from '$lib/avatar';
	import AnsiArt from '$lib/AnsiArt.svelte';
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
		replyBody = '';
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
	<p class="text-sm text-slate-400">Loading…</p>
{:else if message}
	<div class="flex items-center justify-between">
		<a href="/netmail" class="text-sm text-cyan-400 hover:text-cyan-300">&larr; Netmail</a>
		<div class="flex items-center gap-2">
			<button
				class="rounded-full border border-slate-700 px-3 py-1 text-sm text-slate-300 transition hover:border-cyan-400 hover:text-cyan-300 disabled:opacity-30"
				disabled={!message.prev_id}
				onclick={() => message?.prev_id && goto(`/netmail/${message.prev_id}`, { replaceState: true })}
			>
				&larr; Prev
			</button>
			<button
				class="rounded-full border border-slate-700 px-3 py-1 text-sm text-slate-300 transition hover:border-cyan-400 hover:text-cyan-300 disabled:opacity-30"
				disabled={!message.next_id}
				onclick={() => message?.next_id && goto(`/netmail/${message.next_id}`, { replaceState: true })}
			>
				Next &rarr;
			</button>
		</div>
	</div>

	<div class="mt-3 mb-5 flex items-start justify-between gap-3">
		<div class="flex items-start gap-3">
			<div
				class="flex h-11 w-11 shrink-0 items-center justify-center rounded-full bg-gradient-to-br text-sm font-bold text-white {avatarGradient(
					message.is_recipient ? message.from_name : message.to_name
				)}"
			>
				{initials(message.is_recipient ? message.from_name : message.to_name)}
			</div>
			<div class="min-w-0">
				<h1 class="text-xl font-bold tracking-tight text-slate-100">{message.subject}</h1>
				<div class="mt-0.5 text-sm text-slate-500">
					{#if message.is_recipient}
						<strong class="text-slate-300">{message.from_name}</strong>
						{#if message.from_address}<span class="text-slate-600">({message.from_address})</span>{/if}
					{:else}
						To <strong class="text-slate-300">{message.to_name}</strong>
						{#if message.to_address}<span class="text-slate-600">({message.to_address})</span>{/if}
					{/if}
					&middot; {formatDateTime(message.posted_at)}
				</div>
			</div>
		</div>
		<button
			class="shrink-0 rounded-full border border-red-900/60 px-3 py-1 text-sm text-red-400 transition hover:bg-red-950/60"
			onclick={remove}
		>
			Delete
		</button>
	</div>

	{#if message.preformatted && message.grid}
		<div class="overflow-x-auto rounded-2xl border border-slate-800/60 bg-black p-4">
			<AnsiArt grid={message.grid} />
		</div>
	{:else}
		<!-- See messages/[id]/+page.svelte's matching branch for why this
		     stays font-mono even for a "plain" message. -->
		<div class="max-w-2xl rounded-2xl border border-slate-800/60 bg-slate-900/40 p-6">
			<div class="font-mono text-sm leading-relaxed whitespace-pre-wrap text-slate-200">
				{@html message.body_html}
			</div>
		</div>
	{/if}

	{#if message.is_recipient}
		<button
			class="mt-5 rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-4 py-1.5 text-sm font-semibold text-white shadow-lg shadow-fuchsia-500/20 transition hover:shadow-fuchsia-500/40"
			onclick={startReply}
		>
			Reply
		</button>
	{/if}

	{#if replying}
		<div class="mt-4 max-w-2xl rounded-2xl border border-slate-800/60 bg-slate-900/40 p-4">
			<div class="flex flex-col gap-3">
				<label class="flex flex-col gap-1 text-sm">
					<span class="text-slate-400">Subject</span>
					<input
						class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 text-slate-100 focus:border-cyan-400 focus:outline-none"
						bind:value={replySubject}
					/>
				</label>
				<label class="flex flex-col gap-1 text-sm">
					<span class="text-slate-400">Message</span>
					<textarea
						class="h-32 rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 font-mono text-sm text-slate-100 focus:border-cyan-400 focus:outline-none"
						bind:value={replyBody}
					></textarea>
				</label>
			</div>
			<button
				class="mt-4 rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-4 py-1.5 text-sm font-semibold text-white disabled:opacity-40"
				disabled={sending || !replySubject || !replyBody}
				onclick={sendReply}
			>
				{sending ? 'Sending…' : 'Send reply'}
			</button>
		</div>
	{/if}
{/if}

<style>
	@keyframes -global-ansi-blink {
		50% {
			opacity: 0;
		}
	}
</style>
