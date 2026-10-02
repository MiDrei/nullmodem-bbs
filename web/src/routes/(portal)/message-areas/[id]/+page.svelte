<script lang="ts">
	import { formatDate } from '$lib/datetime';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
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

<a href="/message-areas" class="back-link">&larr; Message Areas</a>

{#if loadError}
	<p class="mt-4 text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="mt-4 text-sm text-muted">Loading…</p>
{:else}
	<div class="mt-1.5 mb-4 flex items-center justify-between gap-4">
		<h1 class="page-title">{area?.name ?? 'Area'}</h1>
		<div class="flex items-center gap-2">
			{#if area}
				<form action="/search" class="hidden sm:flex">
					<input type="hidden" name="area" value={area.id} />
					<input type="hidden" name="name" value={area.name} />
					<input name="q" class="field field-sm w-44" placeholder="Search here…" minlength="2" />
				</form>
			{/if}
			{#if area && area.min_sl_write <= 255 && !composing}
				<button class="btn-primary" onclick={() => (composing = true)}>+ New Message</button>
			{/if}
		</div>
	</div>

	{#if composing}
		<!-- D's "New message": mono labels, the text on a sunken panel. -->
		<form
			class="mb-6 flex flex-col gap-3.5"
			onsubmit={(e) => {
				e.preventDefault();
				post();
			}}
		>
			<label class="flex flex-col gap-2">
				<span class="card-label">To</span>
				<input class="field" bind:value={composeTo} />
			</label>
			<label class="flex flex-col gap-2">
				<span class="card-label">Subject</span>
				<input class="field" bind:value={composeSubject} placeholder="Say something…" />
			</label>
			<label class="flex flex-col gap-2">
				<span class="card-label">Message</span>
				<textarea
					class="body-panel h-64 resize-y outline-none focus:border-accent"
					bind:value={composeBody}
					placeholder="Write your message…"
				></textarea>
			</label>
			<div class="flex justify-end gap-2.5">
				<button type="button" class="btn-secondary" onclick={() => (composing = false)}>Cancel</button>
				<button type="submit" class="btn-primary" disabled={posting || !composeSubject || !composeBody}>
					{posting ? 'Posting…' : 'Post message'}
				</button>
			</div>
		</form>
	{/if}

	{#if messagePage && messagePage.messages.length === 0}
		<p class="text-sm text-muted">No messages in this area yet.</p>
	{:else if messagePage}
		<div class="flex flex-col">
			{#each messagePage.messages as m, i (m.id)}
				<a href="/messages/{m.id}" class="list-row group {m.unread ? 'list-row-unread' : ''}">
					<span class="list-num">{String(offset + i + 1).padStart(2, '0')}</span>
					<div class="min-w-0 flex-1">
						<div
							class="truncate text-[13.5px] {m.unread
								? 'font-semibold text-white'
								: 'font-medium text-slate-100'} group-hover:text-accent"
						>
							{m.subject}
						</div>
						<div class="mt-0.5 truncate text-xs text-faint">{m.from_name}</div>
					</div>
					{#if m.unread}
						<span class="badge-new">NEW</span>
					{/if}
					<span class="list-meta w-16 shrink-0 text-right">{relativeTime(m.posted_at)}</span>
				</a>
			{/each}
		</div>

		<div class="mt-2 flex items-center justify-between border-t border-line pt-4 text-xs">
			<button
				class="text-faint transition-colors hover:text-accent disabled:opacity-30 disabled:hover:text-faint"
				disabled={offset === 0}
				onclick={prevPage}
			>
				&larr; Older
			</button>
			<span class="list-meta">
				{offset + 1}&ndash;{Math.min(offset + pageSize, messagePage.total)} of {messagePage.total}
			</span>
			<button
				class="text-faint transition-colors hover:text-accent disabled:opacity-30 disabled:hover:text-faint"
				disabled={offset + pageSize >= messagePage.total}
				onclick={nextPage}
			>
				Newer &rarr;
			</button>
		</div>
	{/if}
{/if}
