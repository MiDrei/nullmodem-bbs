<script lang="ts">
	import { formatDate } from '$lib/datetime';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { avatarGradient, initials } from '$lib/avatar';
	import {
		listBBSMessageAreas,
		listBBSMessages,
		postBBSMessage,
		getFirstUnreadMessagePosition,
		ApiError,
		type BBSMessageArea,
		type BBSMessagePage
	} from '$lib/api';

	const areaId = Number(page.params.id);
	const pageSize = 25;

	let area = $state<BBSMessageArea | null>(null);
	let messagePage = $state<BBSMessagePage | null>(null);
	// Seeded from ?offset= so returning here (Back to area from the
	// reader uses history.back(), see messages/[id]/+page.svelte)
	// lands back on the same page instead of always resetting to the
	// first one.
	let offset = $state(Number(page.url.searchParams.get('offset') ?? 0) || 0);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let composing = $state(false);
	let composeSubject = $state('');
	let composeBody = $state('');
	let composeTo = $state('All');
	let posting = $state(false);

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			bbsAuth.clear();
			await goto('/login');
			return true;
		}
		return false;
	}

	async function load() {
		if (!bbsAuth.token) return;
		try {
			const [areas, msgs] = await Promise.all([
				listBBSMessageAreas(bbsAuth.token),
				listBBSMessages(bbsAuth.token, areaId, pageSize, offset)
			]);
			area = areas.find((a) => a.id === areaId) ?? null;
			messagePage = msgs;
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load messages.';
		} finally {
			loaded = true;
		}
	}

	onMount(async () => {
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		// A URL that already names an offset means we got here via
		// history.back()/a page-turn's own replaceState (see
		// syncOffsetToURL) -- respect that exact page. A fresh entry
		// (no ?offset=) instead jumps straight to the page containing
		// the first unread message, the same way the Telnet/SSH area
		// lightbar does (see internal/bbs's firstUnreadIndex) rather
		// than always opening at the oldest page.
		if (!page.url.searchParams.has('offset')) {
			try {
				const { position } = await getFirstUnreadMessagePosition(bbsAuth.token, areaId);
				offset = Math.floor(position / pageSize) * pageSize;
			} catch {
				// Non-critical: falls back to the oldest page (offset 0).
			}
		}
		await load();
	});

	// replaceState (not a new history entry) so Back from the reader's
	// history.back() lands here rather than stepping back through
	// every page turn one at a time.
	function syncOffsetToURL() {
		const url = new URL(page.url);
		if (offset > 0) url.searchParams.set('offset', String(offset));
		else url.searchParams.delete('offset');
		goto(url, { replaceState: true, noScroll: true, keepFocus: true });
	}

	function nextPage() {
		if (!messagePage) return;
		if (offset + pageSize >= messagePage.total) return;
		offset += pageSize;
		syncOffsetToURL();
		load();
	}

	function prevPage() {
		offset = Math.max(0, offset - pageSize);
		syncOffsetToURL();
		load();
	}

	function relativeTime(iso: string): string {
		const diffMs = Date.now() - new Date(iso).getTime();
		const mins = Math.round(diffMs / 60000);
		if (mins < 1) return 'just now';
		if (mins < 60) return `${mins}m ago`;
		const hours = Math.round(mins / 60);
		if (hours < 24) return `${hours}h ago`;
		const days = Math.round(hours / 24);
		if (days < 7) return `${days}d ago`;
		return formatDate(iso);
	}

	async function post() {
		if (!bbsAuth.token) return;
		posting = true;
		try {
			await postBBSMessage(bbsAuth.token, areaId, composeTo, composeSubject, composeBody);
			composing = false;
			composeSubject = '';
			composeBody = '';
			composeTo = 'All';
			offset = 0;
			await load();
			toast.push('Message posted.', 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not post message.', 'error');
		} finally {
			posting = false;
		}
	}
</script>

<a href="/message-areas" class="text-sm text-cyan-400 hover:text-cyan-300">&larr; Message Areas</a>

{#if loadError}
	<p class="mt-4 text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="mt-4 text-sm text-slate-400">Loading…</p>
{:else}
	<div class="mt-2 mb-6 flex items-center justify-between">
		<h1 class="text-2xl font-bold tracking-tight text-slate-100">{area?.name ?? 'Area'}</h1>
		{#if area && area.min_sl_write <= 255}
			<button
				class="rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-4 py-1.5 text-sm font-semibold text-white shadow-lg shadow-fuchsia-500/20 transition hover:shadow-fuchsia-500/40"
				onclick={() => (composing = !composing)}
			>
				{composing ? 'Cancel' : '+ New Message'}
			</button>
		{/if}
	</div>

	{#if composing}
		<div class="mb-6 rounded-2xl border border-slate-800/60 bg-slate-900/40 p-4">
			<div class="flex flex-col gap-3">
				<label class="flex flex-col gap-1 text-sm">
					<span class="text-slate-400">To</span>
					<input
						class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 text-slate-100 focus:border-cyan-400 focus:outline-none"
						bind:value={composeTo}
					/>
				</label>
				<label class="flex flex-col gap-1 text-sm">
					<span class="text-slate-400">Subject</span>
					<input
						class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 text-slate-100 focus:border-cyan-400 focus:outline-none"
						bind:value={composeSubject}
					/>
				</label>
				<label class="flex flex-col gap-1 text-sm">
					<span class="text-slate-400">Message</span>
					<textarea
						class="h-32 rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 font-mono text-sm text-slate-100 focus:border-cyan-400 focus:outline-none"
						bind:value={composeBody}
					></textarea>
				</label>
			</div>
			<button
				class="mt-4 rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-4 py-1.5 text-sm font-semibold text-white disabled:opacity-40"
				disabled={posting || !composeSubject || !composeBody}
				onclick={post}
			>
				{posting ? 'Posting…' : 'Post'}
			</button>
		</div>
	{/if}

	{#if messagePage && messagePage.messages.length === 0}
		<p class="text-sm text-slate-400">No messages in this area yet.</p>
	{:else if messagePage}
		<div class="overflow-hidden rounded-2xl border border-slate-800/60 bg-slate-900/40">
			{#each messagePage.messages as m, i (m.id)}
				<a
					href="/messages/{m.id}"
					class="group flex items-center gap-3 px-4 py-2.5 transition hover:bg-slate-800/60 {i > 0
						? 'border-t border-slate-800/60'
						: ''} {m.unread ? 'border-l-2 border-l-fuchsia-400' : 'border-l-2 border-l-transparent'}"
				>
					<div
						class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-gradient-to-br text-xs font-bold text-white {avatarGradient(
							m.from_name
						)}"
					>
						{initials(m.from_name)}
					</div>
					<div class="min-w-0 flex-1">
						<div
							class="truncate text-sm {m.unread
								? 'font-semibold text-slate-100'
								: 'font-medium text-slate-400'} transition group-hover:text-white"
						>
							{m.subject}
						</div>
						<div class="truncate text-xs text-slate-500">{m.from_name}</div>
					</div>
					<span class="shrink-0 text-xs text-slate-500">{relativeTime(m.posted_at)}</span>
				</a>
			{/each}
		</div>

		<div class="mt-4 flex items-center justify-between text-sm">
			<button
				class="rounded-full border border-slate-700 px-3 py-1 text-slate-300 transition hover:border-cyan-400 hover:text-cyan-300 disabled:opacity-30"
				disabled={offset === 0}
				onclick={prevPage}
			>
				&larr; Older
			</button>
			<span class="text-slate-500">
				{offset + 1}&ndash;{Math.min(offset + pageSize, messagePage.total)} of {messagePage.total}
			</span>
			<button
				class="rounded-full border border-slate-700 px-3 py-1 text-slate-300 transition hover:border-cyan-400 hover:text-cyan-300 disabled:opacity-30"
				disabled={offset + pageSize >= messagePage.total}
				onclick={nextPage}
			>
				Newer &rarr;
			</button>
		</div>
	{/if}
{/if}
