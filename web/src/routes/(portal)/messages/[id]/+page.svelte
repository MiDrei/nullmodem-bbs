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
	import ThreadTree from '$lib/ThreadTree.svelte';
	import { getBBSMessage, getBBSThread, postBBSMessage, ApiError, type BBSMessage, type ThreadEntry } from '$lib/api';

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
	let thread = $state<ThreadEntry[]>([]);

	// Within the thread: what it answers, and the next one to read.
	const parent = $derived(thread.find((e) => e.id === message?.reply_to));
	const nextInThread = $derived.by(() => {
		const i = thread.findIndex((e) => e.id === message?.id);
		return i >= 0 ? thread.slice(i + 1).find((e) => !e.read) ?? thread[i + 1] : undefined;
	});

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
			thread = [];
			getBBSThread(bbsAuth.token, id)
				.then((t) => {
					// Opening it just marked this one read.
					if (message?.id === id) thread = t.messages.map((e) => (e.id === id ? { ...e, read: true } : e));
				})
				.catch(() => {});
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : t('web.common.could_not_load_message');
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
		if (e.key.toLowerCase() === 't' && nextInThread) goto(`/messages/${nextInThread.id}`, { replaceState: true });
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
		replyBody = quoteText(message.body, message.from_name, message.to_name) + '\n\n';
		replying = true;
	}

	async function sendReply() {
		if (!bbsAuth.token || !message) return;
		posting = true;
		try {
			await postBBSMessage(bbsAuth.token, message.area_id, replyTo, replySubject, replyBody, message.id);
			replying = false;
			toast.push(t('common.reply_posted'), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('web.msg.reply_failed'), 'error');
		} finally {
			posting = false;
		}
	}
</script>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-muted">{t('web.common.loading')}</p>
{:else if message}
	<div class="flex items-center justify-between">
		<a
			href="/message-areas/{message.area_id}"
			class="back-link"
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
			&larr; {t('web.msg.back_to_area')}
		</a>
		<div class="flex items-center gap-2">
			<button
				class="btn-secondary px-3.5 py-2 text-xs"
				disabled={!message.prev_id}
				onclick={() => message?.prev_id && goto(`/messages/${message.prev_id}`, { replaceState: true })}
			>
				&larr; {t('web.common.prev')}
			</button>
			<button
				class="btn-secondary px-3.5 py-2 text-xs"
				disabled={!message.next_id}
				onclick={() => message?.next_id && goto(`/messages/${message.next_id}`, { replaceState: true })}
			>
				{t('web.common.next')} &rarr;
			</button>
		</div>
	</div>

	<div class="mt-7 mb-5 flex items-center gap-3.5">
		<div
			class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-surface font-mono text-[13px] text-accent"
			aria-hidden="true"
		>
			{message.from_name.slice(0, 1).toUpperCase()}
		</div>
		<div class="min-w-0">
			<h1 class="text-lg font-semibold text-ink-strong">{message.subject}</h1>
			<div class="mt-0.5 text-[12.5px] text-muted">
				<span class="text-slate-400">{message.from_name}</span> {t('web.common.to_2')} {message.to_name} &middot;
				{formatDateTime(message.posted_at)}
			</div>
			{#if parent}
				<a href="/messages/{parent.id}" data-sveltekit-replacestate class="mt-0.5 block truncate text-[12px] text-faint hover:text-accent">
					↳ {t('web.msg.in_reply_to', { NAME: parent.from_name })}{parent.guessed ? ` ${t('web.msg.by_subject')}` : ''}
				</a>
			{/if}
		</div>
	</div>

	{#if message.preformatted && message.grid}
		<!-- As wide as the column allows, and never a scrollbar. -->
		<div class="ansi-panel">
			<AnsiArt grid={message.grid} fit maxZoom={1.3} />
		</div>
	{:else if message.preformatted}
		<div class="ansi-panel overflow-x-auto">
			<div class="inline-block font-mono text-sm leading-tight whitespace-pre">
				{@html message.body_html}
			</div>
		</div>
	{:else}
		<!-- Monospace even for a "plain" (non-preformatted) message:
		     echomail is authored assuming a monospace terminal regardless
		     -- an ASCII banner or hand-aligned table that doesn't trip
		     ansi.IsPreformatted's heuristics (no real ANSI codes, no CP437
		     block bytes) still relies on every character being the same
		     width to line up. whitespace-pre-wrap still lets genuinely
		     long lines wrap for comfortable reading. -->
		<div class="body-panel whitespace-pre-wrap">{@html message.body_html}</div>
	{/if}

	{#if thread.length > 1}
		<section class="card mt-5 px-3 py-3">
			<div class="mb-1.5 flex items-center justify-between px-2">
				<h2 class="card-label">{t('web.common.thread_count_messages', { COUNT: thread.length })}</h2>
				{#if nextInThread}
					<a href="/messages/{nextInThread.id}" data-sveltekit-replacestate class="text-xs text-accent hover:underline" title="T">{t('web.msg.next_in_thread')} →</a>
				{/if}
			</div>
			<ThreadTree entries={thread} current={message.id} href={(id) => `/messages/${id}`} />
		</section>
	{/if}

	{#if !replying}
		<button class="btn-primary mt-5 px-5" onclick={startReply}>{t('web.msg.reply')}</button>
	{:else}
		<form
			class="mt-6 flex flex-col gap-3.5"
			onsubmit={(e) => {
				e.preventDefault();
				sendReply();
			}}
		>
			<label class="flex flex-col gap-2">
				<span class="card-label">{t('web.common.to')}</span>
				<input class="field" bind:value={replyTo} />
			</label>
			<label class="flex flex-col gap-2">
				<span class="card-label">{t('common.subject')}</span>
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
				<button type="submit" class="btn-primary" disabled={posting || !replySubject || !replyBody}>
					{posting ? t('web.msg.posting') : t('web.msg.post_reply')}
				</button>
			</div>
		</form>
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
