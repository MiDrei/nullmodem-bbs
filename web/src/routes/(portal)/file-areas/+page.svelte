<script lang="ts">
	import { t } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
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
			loadError = err instanceof ApiError ? err.message : t('web.common.could_not_load_file_areas');
		} finally {
			loaded = true;
		}
	});

	// Areas already arrive grouped by network (see ListAreaStats'
	// ORDER BY) -- fold them into named sections, same as message-areas.
	let groups = $derived.by(() => {
		const out: { network: string; areas: BBSFileArea[]; newCount: number }[] = [];
		for (const area of areas) {
			const label = area.network || t('web.areas.local');
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

<div class="mb-4">
	<h1 class="page-title">{t('common.files')}</h1>
	<p class="page-subtitle">{t('web.files.subtitle')}</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-muted">{t('web.common.loading')}</p>
{:else if areas.length === 0}
	<p class="text-sm text-muted">{t('web.files.none')}</p>
{:else}
	{#if groups.length > 1}
		<!-- One pill per network, so hundreds of areas aren't one scroll. -->
		<div class="mb-4 flex flex-wrap gap-1.5" role="tablist" aria-label={t('web.areas.networks')}>
			<button
				role="tab"
				aria-selected={activeNetwork === ALL_TAB}
				class="pill {activeNetwork === ALL_TAB ? 'pill-active' : ''}"
				onclick={() => (activeNetwork = ALL_TAB)}
			>
				{t('web.common.all')} · {areas.length}{totalNew > 0 ? ` · ${t('common.count_new', { COUNT: totalNew })}` : ''}
			</button>
			{#each groups as group (group.network)}
				<button
					role="tab"
					aria-selected={activeNetwork === group.network}
					class="pill {activeNetwork === group.network ? 'pill-active' : ''}"
					onclick={() => (activeNetwork = group.network)}
				>
					{group.network} · {group.areas.length}{group.newCount > 0 ? ` · ${t('common.count_new', { COUNT: group.newCount })}` : ''}
				</button>
			{/each}
		</div>
	{/if}
	<div class="flex flex-col">
		{#each visibleGroups as group, gi (group.network)}
			{#if activeNetwork === ALL_TAB && groups.length > 1}
				<h2 class="card-label px-1 pb-1.5 {gi > 0 ? 'pt-5' : ''}">{group.network}</h2>
			{/if}
			{#each group.areas as area, i (area.id)}
				<a
					href="/file-areas/{area.id}"
					class="list-row group {area.new > 0 ? 'list-row-unread' : ''}"
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
					</div>
					{#if area.new > 0}
						<span class="badge-new">{t('web.common.n_new_badge', { COUNT: area.new })}</span>
					{/if}
					<span class="list-meta w-10 shrink-0 text-right">{area.total}</span>
				</a>
			{/each}
		{/each}
	</div>
{/if}
