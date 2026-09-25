<script lang="ts">
	import { formatDateTime } from '$lib/datetime';
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { avatarGradient, initials } from '$lib/avatar';
	import AnsiArt from '$lib/AnsiArt.svelte';
	import { getBBSMessage, postBBSMessage, ApiError, type BBSMessage } from '$lib/api';

	// $derived (not a plain const) so Prev/Next -- which navigate to
	// another /messages/[id] URL that SvelteKit resolves to this same
	// component instance rather than remounting it -- actually picks
	// up the new id and reloads (see the $effect below).
	let messageId = $derived(Number(page.params.id));

	let message = $state<BBSMessage | null>(null);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let replying = $state(false);
	let replySubject = $state('');
	let replyBody = $state('');
	let replyTo = $state('');
	let posting = $state(false);

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
			message = await getBBSMessage(bbsAuth.token, id);
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
		if (e.key === 'ArrowLeft' && message?.prev_id) goto(`/messages/${message.prev_id}`, { replaceState: true });
		if (e.key === 'ArrowRight' && message?.next_id) goto(`/messages/${message.next_id}`, { replaceState: true });
	}

	onMount(() => {
		window.addEventListener('keydown', onKeydown);
	});
	onDestroy(() => {
		window.removeEventListener('keydown', onKeydown);
	});

	function startReply() {
		if (!message) return;
		replyTo = message.from_name;
		replySubject = message.subject.startsWith('Re: ') ? message.subject : `Re: ${message.subject}`;
		replyBody = '';
		replying = true;
	}

	async function sendReply() {
		if (!bbsAuth.token || !message) return;
		posting = true;
		try {
			await postBBSMessage(bbsAuth.token, message.area_id, replyTo, replySubject, replyBody);
			replying = false;
			toast.push('Reply posted.', 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not post reply.', 'error');
		} finally {
			posting = false;
		}
	}
</script>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else if message}
	<div class="flex items-center justify-between">
		<a
			href="/message-areas/{message.area_id}"
			class="text-sm text-cyan-400 hover:text-cyan-300"
			onclick={(e) => {
				// Prefer returning to wherever the area list actually was
				// (including which page it was scrolled to -- see that
				// page's own offset<->URL syncing) over always jumping to
				// its first page; history.back() is a no-op with nothing
				// to fall back to, in which case the plain href above
				// still takes over normally.
				if (history.length > 1) {
					e.preventDefault();
					history.back();
				}
			}}
		>
			&larr; Back to area
		</a>
		<div class="flex items-center gap-2">
			<button
				class="rounded-full border border-slate-700 px-3 py-1 text-sm text-slate-300 transition hover:border-cyan-400 hover:text-cyan-300 disabled:opacity-30"
				disabled={!message.prev_id}
				onclick={() => message?.prev_id && goto(`/messages/${message.prev_id}`, { replaceState: true })}
			>
				&larr; Prev
			</button>
			<button
				class="rounded-full border border-slate-700 px-3 py-1 text-sm text-slate-300 transition hover:border-cyan-400 hover:text-cyan-300 disabled:opacity-30"
				disabled={!message.next_id}
				onclick={() => message?.next_id && goto(`/messages/${message.next_id}`, { replaceState: true })}
			>
				Next &rarr;
			</button>
		</div>
	</div>

	<div class="mt-3 mb-5 flex items-start gap-3">
		<div
			class="flex h-11 w-11 shrink-0 items-center justify-center rounded-full bg-gradient-to-br text-sm font-bold text-white {avatarGradient(
				message.from_name
			)}"
		>
			{initials(message.from_name)}
		</div>
		<div class="min-w-0">
			<h1 class="text-xl font-bold tracking-tight text-slate-100">{message.subject}</h1>
			<div class="mt-0.5 text-sm text-slate-500">
				<strong class="text-slate-300">{message.from_name}</strong> to {message.to_name} &middot;
				{formatDateTime(message.posted_at)}
			</div>
		</div>
	</div>

	{#if message.preformatted && message.grid}
		<div class="overflow-x-auto rounded-2xl border border-slate-800/60 bg-black p-4">
			<AnsiArt grid={message.grid} />
		</div>
	{:else if message.preformatted}
		<div class="overflow-x-auto rounded-2xl border border-slate-800/60 bg-black p-4">
			<div class="inline-block font-mono text-sm leading-tight whitespace-pre">
				{@html message.body_html}
			</div>
		</div>
	{:else}
		<!-- Still font-mono even for a "plain" (non-preformatted) message:
		     echomail is authored assuming a monospace terminal regardless
		     -- an ASCII banner or hand-aligned table that doesn't trip
		     ansi.IsPreformatted's heuristics (no real ANSI codes, no CP437
		     block bytes) still relies on every character being the same
		     width to line up. Confirmed live: a figlet-style text banner
		     rendered in this card's original proportional font came out
		     visibly misaligned. whitespace-pre-wrap still lets genuinely
		     long lines wrap for comfortable reading. -->
		<div class="max-w-2xl rounded-2xl border border-slate-800/60 bg-slate-900/40 p-6">
			<div class="font-mono text-sm leading-relaxed whitespace-pre-wrap text-slate-200">
				{@html message.body_html}
			</div>
		</div>
	{/if}

	<button
		class="mt-5 rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-4 py-1.5 text-sm font-semibold text-white shadow-lg shadow-fuchsia-500/20 transition hover:shadow-fuchsia-500/40"
		onclick={startReply}
	>
		Reply
	</button>

	{#if replying}
		<div class="mt-4 max-w-2xl rounded-2xl border border-slate-800/60 bg-slate-900/40 p-4">
			<div class="flex flex-col gap-3">
				<label class="flex flex-col gap-1 text-sm">
					<span class="text-slate-400">To</span>
					<input
						class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 text-slate-100 focus:border-cyan-400 focus:outline-none"
						bind:value={replyTo}
					/>
				</label>
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
				disabled={posting || !replySubject || !replyBody}
				onclick={sendReply}
			>
				{posting ? 'Posting…' : 'Post reply'}
			</button>
		</div>
	{/if}
{/if}

<style>
	/* -global- so it survives being referenced from the raw {@html}
	   message body markup, which isn't compiled by Svelte's CSS scoping
	   (see internal/ansi.ToHTML). */
	@keyframes -global-ansi-blink {
		50% {
			opacity: 0;
		}
	}
</style>
