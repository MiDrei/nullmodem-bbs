<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { avatarGradient, initials } from '$lib/avatar';
	import { listBBSFileAreas, ApiError, type BBSFileArea } from '$lib/api';

	const ALL_TAB = '__all__';

	let areas = $state<BBSFileArea[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);
	let activeNetwork = $state(ALL_TAB);

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
			areas = await listBBSFileAreas(bbsAuth.token);
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load file areas.';
		} finally {
			loaded = true;
		}
	});

	// Areas already arrive grouped by network (see ListAreaStats'
	// ORDER BY) -- fold them into named sections, same as message-areas.
	let groups = $derived.by(() => {
		const out: { network: string; areas: BBSFileArea[]; newCount: number }[] = [];
		for (const area of areas) {
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
	let totalNew = $derived(areas.reduce((sum, a) => sum + a.new, 0));

	// A real hub can easily carry hundreds of areas across a handful of
	// networks -- one tab per network (plus "All") so that's not one
	// huge scroll to find anything.
	let visibleGroups = $derived(
		activeNetwork === ALL_TAB ? groups : groups.filter((g) => g.network === activeNetwork)
	);
</script>

<div class="mb-6">
	<h1 class="text-2xl font-bold tracking-tight text-slate-100">Files</h1>
	<p class="mt-1 text-sm text-slate-500">File libraries you can browse and download from.</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else if areas.length === 0}
	<p class="text-sm text-slate-400">No file areas available to you yet.</p>
{:else}
	{#if groups.length > 1}
		<div class="mb-4 flex flex-wrap gap-1 border-b border-slate-800/60">
			<button
				class="border-b-2 px-3 py-2 text-sm font-medium transition {activeNetwork === ALL_TAB
					? 'border-fuchsia-400 text-slate-100'
					: 'border-transparent text-slate-500 hover:text-slate-300'}"
				onclick={() => (activeNetwork = ALL_TAB)}
			>
				All ({areas.length})
				{#if totalNew > 0}
					<span
						class="ml-1 rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-1.5 py-0.5 text-[10px] font-semibold text-white"
					>
						{totalNew}
					</span>
				{/if}
			</button>
			{#each groups as group (group.network)}
				<button
					class="border-b-2 px-3 py-2 text-sm font-medium transition {activeNetwork ===
					group.network
						? 'border-fuchsia-400 text-slate-100'
						: 'border-transparent text-slate-500 hover:text-slate-300'}"
					onclick={() => (activeNetwork = group.network)}
				>
					{group.network} ({group.areas.length})
					{#if group.newCount > 0}
						<span
							class="ml-1 rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-1.5 py-0.5 text-[10px] font-semibold text-white"
						>
							{group.newCount}
						</span>
					{/if}
				</button>
			{/each}
		</div>
	{/if}
	<div class="flex flex-col gap-6">
		{#each visibleGroups as group (group.network)}
			<div>
				<h2 class="mb-2 px-1 text-xs font-semibold tracking-widest text-slate-500 uppercase">
					{group.network}
				</h2>
				<div class="overflow-hidden rounded-2xl border border-slate-800/60 bg-slate-900/40">
					{#each group.areas as area, i (area.id)}
						<a
							href="/file-areas/{area.id}"
							class="group flex items-center gap-3 px-4 py-2.5 transition hover:bg-slate-800/60 {i > 0
								? 'border-t border-slate-800/60'
								: ''} {area.new > 0 ? 'border-l-2 border-l-fuchsia-400' : 'border-l-2 border-l-transparent'}"
						>
							<div
								class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-gradient-to-br text-xs font-bold text-white {avatarGradient(
									area.name
								)}"
							>
								{initials(area.name)}
							</div>
							<div class="min-w-0 flex-1">
								<div
									class="truncate text-sm font-medium {area.new > 0
										? 'text-slate-100'
										: 'text-slate-300'} transition group-hover:text-white"
								>
									{area.name}
								</div>
							</div>
							{#if area.new > 0}
								<span
									class="rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-2 py-0.5 text-xs font-semibold text-white"
								>
									{area.new} new
								</span>
							{/if}
							<span class="w-12 shrink-0 text-right text-xs text-slate-500">{area.total}</span>
						</a>
					{/each}
				</div>
			</div>
		{/each}
	</div>
{/if}
