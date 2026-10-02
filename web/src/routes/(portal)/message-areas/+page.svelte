<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { listBBSMessageAreas, setMyArea, ApiError, type BBSMessageArea } from '$lib/api';
	import { toast } from '$lib/toast.svelte';

	const ALL_TAB = '__all__';

	let areas = $state<BBSMessageArea[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);
	let activeNetwork = $state(ALL_TAB);
	// My areas (what the new scan, QWK and the reader include) or all.
	function savedScope(): 'mine' | 'all' {
		try {
			return localStorage.getItem('nullmodem.areaScope') === 'all' ? 'all' : 'mine';
		} catch {
			return 'mine';
		}
	}
	let scope = $state<'mine' | 'all'>(savedScope());
	function setScope(v: 'mine' | 'all') {
		scope = v;
		activeNetwork = ALL_TAB;
		try {
			localStorage.setItem('nullmodem.areaScope', v);
		} catch {
			// Just not remembered.
		}
	}

	async function toggle(area: BBSMessageArea, e?: Event) {
		e?.preventDefault();
		if (!bbsAuth.token) return;
		const want = !area.mine;
		area.mine = want;
		try {
			await setMyArea(bbsAuth.token, area.id, want);
		} catch (err) {
			area.mine = !want;
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not save it.', 'error');
		}
	}

	async function toggleGroup(list: BBSMessageArea[]) {
		const want = list.some((a) => !a.mine);
		for (const a of list) if (a.mine !== want) await toggle(a);
	}

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			bbsAuth.clear();
			await goto('/login');
			return true;
		}
		return false;
	}

	onMount(async () => {
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		try {
			areas = await listBBSMessageAreas(bbsAuth.token);
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load message areas.';
		} finally {
			loaded = true;
		}
	});

	// Areas already arrive grouped by network (see ListAreaStats'
	// ORDER BY) -- fold them into named sections here instead of
	// repeating each area's network as a per-row label.
	let groups = $derived.by(() => {
		const out: { network: string; areas: BBSMessageArea[]; newCount: number }[] = [];
		for (const area of areas.filter((a) => scope === 'all' || a.mine)) {
			const label = area.network || 'Local';
			const last = out[out.length - 1];
			if (last && last.network === label) {
				last.areas.push(area);
				last.newCount += area.new;
			} else {
				out.push({ network: label, areas: [area], newCount: area.new });
			}
		}
		return out;
	});
	let shown = $derived(areas.filter((a) => scope === 'all' || a.mine));
	let totalNew = $derived(shown.reduce((sum, a) => sum + a.new, 0));
	let mineCount = $derived(areas.filter((a) => a.mine).length);

	// A real hub can easily carry hundreds of areas across a handful of
	// networks -- one tab per network (plus "All") so that's not one
	// huge scroll to find anything.
	let visibleGroups = $derived(
		activeNetwork === ALL_TAB ? groups : groups.filter((g) => g.network === activeNetwork)
	);
</script>

<div class="mb-4 flex flex-wrap items-end justify-between gap-4">
	<div>
		<h1 class="page-title">Message Areas</h1>
		<p class="page-subtitle">Boards you can read and post to</p>
	</div>
	<form action="/search" class="flex gap-2">
		<input name="q" class="field field-sm w-56" placeholder="Search messages…" minlength="2" />
	</form>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-muted">Loading…</p>
{:else if areas.length === 0}
	<p class="text-sm text-muted">No message areas available to you yet.</p>
{:else}
	<div class="mb-3 flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-line">
		<div class="flex gap-1">
			{#each [['mine', `My areas · ${mineCount}`], ['all', `All areas · ${areas.length}`]] as [v, label] (v)}
				<button
					class="border-b-2 px-3 py-2 text-sm font-medium transition {scope === v ? 'border-accent text-ink-strong' : 'border-transparent text-muted hover:text-ink'}"
					onclick={() => setScope(v as 'mine' | 'all')}>{label}</button
				>
			{/each}
		</div>
		<p class="pb-2 text-xs text-faint">
			{scope === 'mine' ? 'What the new scan, QWK packets and the reader app include.' : '✓ marks your areas -- click it to add or take out an area.'}
		</p>
	</div>
	{#if scope === 'mine' && mineCount === 0}
		<p class="text-sm text-muted">You took every area out -- add some under <button class="text-accent hover:underline" onclick={() => setScope('all')}>All areas</button>.</p>
	{/if}
	{#if groups.length > 1}
		<!-- One pill per network, so hundreds of areas aren't one scroll. -->
		<div class="mb-4 flex flex-wrap gap-1.5" role="tablist" aria-label="Networks">
			<button
				role="tab"
				aria-selected={activeNetwork === ALL_TAB}
				class="pill {activeNetwork === ALL_TAB ? 'pill-active' : ''}"
				onclick={() => (activeNetwork = ALL_TAB)}
			>
				All · {shown.length}{totalNew > 0 ? ` · ${totalNew} new` : ''}
			</button>
			{#each groups as group (group.network)}
				<button
					role="tab"
					aria-selected={activeNetwork === group.network}
					class="pill {activeNetwork === group.network ? 'pill-active' : ''}"
					onclick={() => (activeNetwork = group.network)}
				>
					{group.network} · {group.areas.length}{group.newCount > 0 ? ` · ${group.newCount} new` : ''}
				</button>
			{/each}
		</div>
	{/if}
	<div class="flex flex-col">
		{#each visibleGroups as group, gi (group.network)}
			{#if (activeNetwork === ALL_TAB && groups.length > 1) || scope === 'all'}
				<div class="flex items-baseline justify-between px-1 pb-1.5 {gi > 0 ? 'pt-5' : ''}">
					<h2 class="card-label">{group.network}</h2>
					{#if scope === 'all'}
						<button class="text-xs text-muted hover:text-accent" onclick={() => toggleGroup(group.areas)}>
							{group.areas.some((a) => !a.mine) ? 'all into my areas' : 'all out of my areas'}
						</button>
					{/if}
				</div>
			{/if}
			{#each group.areas as area, i (area.id)}
				<div class="flex items-stretch">
				<a
					href="/message-areas/{area.id}"
					class="list-row group min-w-0 flex-1 {area.new > 0 ? 'list-row-unread' : ''}"
				>
					<span class="list-num">{String(i + 1).padStart(2, '0')}</span>
					<div class="min-w-0 flex-1">
						<div
							class="truncate text-[13.5px] {area.new > 0
								? 'font-semibold text-white'
								: 'font-medium text-slate-100'} group-hover:text-accent"
						>
							{area.name}
						</div>
						{#if area.description}
							<div class="mt-0.5 truncate text-xs text-faint">{area.description}</div>
						{/if}
					</div>
					{#if area.new > 0}
						<span class="badge-new">{area.new} NEW</span>
					{/if}
					<span class="list-meta w-10 shrink-0 text-right">{area.total}</span>
				</a>
				<button
					class="w-10 shrink-0 text-base transition {area.mine ? 'text-accent' : 'text-faint hover:text-accent'}"
					title={area.mine ? 'In your areas -- click to take it out' : 'Not in your areas -- click to add it'}
					aria-label={area.mine ? `Take ${area.name} out of my areas` : `Add ${area.name} to my areas`}
					aria-pressed={area.mine}
					onclick={(e) => toggle(area, e)}>{area.mine ? '✓' : '+'}</button
				>
				</div>
			{/each}
		{/each}
	</div>
{/if}
