<script lang="ts">
	// Message search: subject, text, sender and recipient, across the
	// areas you can read -- or one, from its page (?area=).
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { formatDateTime } from '$lib/datetime';
	import { searchMessages, ApiError, type SearchHit } from '$lib/api';

	let q = $state(page.url.searchParams.get('q') ?? '');
	const areaId = Number(page.url.searchParams.get('area') ?? 0) || 0;
	const areaName = page.url.searchParams.get('name') ?? '';
	let hits = $state<SearchHit[] | null>(null);
	let error = $state<string | null>(null);
	let busy = $state(false);

	async function run(e?: SubmitEvent) {
		e?.preventDefault();
		if (!bbsAuth.token || q.trim().length < 2) return;
		busy = true;
		try {
			hits = await searchMessages(bbsAuth.token, q.trim(), areaId);
			error = null;
			const u = new URL(page.url);
			u.searchParams.set('q', q.trim());
			history.replaceState(history.state, '', u);
		} catch (err) {
			if (err instanceof ApiError && err.status === 401) {
				bbsAuth.clear();
				await goto('/login');
				return;
			}
			error = err instanceof ApiError ? err.message : 'Could not search.';
		} finally {
			busy = false;
		}
	}

	onMount(() => {
		if (!bbsAuth.token) {
			goto('/login');
			return;
		}
		if (q) run();
	});

	// The match, highlighted (plain text in, nothing interpreted).
	function marked(text: string): { pre: string; hit: string; post: string } {
		const i = text.toLowerCase().indexOf(q.trim().toLowerCase());
		if (i < 0 || !q.trim()) return { pre: text, hit: '', post: '' };
		return { pre: text.slice(0, i), hit: text.slice(i, i + q.trim().length), post: text.slice(i + q.trim().length) };
	}
</script>

<div class="mb-5">
	<h1 class="page-title">Search</h1>
	<p class="page-subtitle">{areaId ? `In ${areaName || 'this area'}` : 'In all areas you can read'} -- subject, text, from and to.</p>
</div>

<form class="mb-5 flex gap-2" onsubmit={run}>
	<!-- svelte-ignore a11y_autofocus -->
	<input class="field min-w-0 flex-1" placeholder="Search messages…" bind:value={q} autofocus />
	<button type="submit" class="btn-primary btn-sm" disabled={busy || q.trim().length < 2}>{busy ? 'Searching…' : 'Search'}</button>
</form>

{#if error}
	<p class="text-sm text-red-400">{error}</p>
{:else if hits && hits.length === 0}
	<p class="text-sm text-muted">Nothing found.</p>
{:else if hits}
	<p class="mb-2 text-xs text-faint">{hits.length === 100 ? 'The newest 100 -- search more precisely for older ones.' : `${hits.length} found`}</p>
	<div class="flex flex-col divide-y divide-line">
		{#each hits as h (h.id)}
			{@const s = marked(h.snippet)}
			<a href="/messages/{h.id}" class="block py-3 hover:bg-surface/50">
				<div class="flex flex-wrap items-baseline gap-x-3">
					<span class="font-medium text-ink-strong">{h.subject}</span>
					<span class="text-xs text-muted">{h.from_name} → {h.to_name}</span>
					<span class="ml-auto text-xs text-faint">{h.area_tag} · {formatDateTime(h.posted_at)}</span>
				</div>
				<p class="mt-1 text-[13px] text-ink-soft">
					{s.pre}{#if s.hit}<mark class="rounded bg-accent/25 px-0.5 text-ink-strong">{s.hit}</mark>{/if}{s.post}
				</p>
			</a>
		{/each}
	</div>
{/if}
