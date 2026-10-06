<script lang="ts">
	// The sysop's latest news, in the reader's language: front page and
	// portal.
	import type { PublicNews } from '$lib/api';
	let { list }: { list: PublicNews[] } = $props();
	const day = (iso: string) => iso.slice(0, 10);
</script>

<ul class="flex flex-col divide-y divide-line">
	{#each list as n (n.id)}
		<li class="py-3 first:pt-0 last:pb-0">
			<div class="mb-1 flex flex-wrap items-baseline gap-x-3">
				<span class="font-mono text-xs text-faint">{day(n.created_at)}</span>
				<h3 class="font-semibold text-ink-strong">{n.title}</h3>
			</div>
			<p class="text-sm leading-relaxed whitespace-pre-line text-ink-soft">{n.text}</p>
		</li>
	{/each}
</ul>
