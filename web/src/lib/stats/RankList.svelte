<script lang="ts">
	// A short ranking: name, a bar for its share, the number.
	import { t, i18n } from '$lib/i18n.svelte';
	import type { StatsRanked } from '$lib/api';

	let {
		title,
		items,
		unit = '',
		empty = t('web.stats.nothing'),
		detail = true
	}: {
		title: string;
		items: StatsRanked[] | null;
		/** After the number: one suffix, or [singular, plural]. */
		unit?: string | [string, string];
		empty?: string;
		detail?: boolean;
	} = $props();

	const max = $derived(Math.max(1, ...(items ?? []).map((x) => x.count)));
</script>

<div class="card">
	<h2 class="card-label mb-3">{title}</h2>
	{#if !items?.length}
		<p class="text-sm text-muted">{empty}</p>
	{:else}
		<ol class="flex flex-col gap-2 text-sm">
			{#each items as x, i (i)}
				<li>
					<div class="flex items-baseline justify-between gap-3">
						<span class="min-w-0 truncate">
							<span class="text-ink-strong">{x.name}</span>
							{#if detail && x.detail}<span class="text-xs text-faint"> · {x.detail}</span>{/if}
						</span>
						<span class="shrink-0 font-mono text-xs text-muted">
							{x.count.toLocaleString(i18n.locale)}{typeof unit === 'string' ? unit : x.count === 1 ? unit[0] : unit[1]}{#if x.minutes}{` · ${x.minutes} min`}{/if}
						</span>
					</div>
					<div class="mt-1 h-1 rounded-full bg-surface">
						<div class="h-1 rounded-full bg-accent/70" style="width: {(x.count / max) * 100}%"></div>
					</div>
				</li>
			{/each}
		</ol>
	{/if}
</div>
