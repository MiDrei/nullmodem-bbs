<script lang="ts">
	// The doors' bulletins (scoreboards, news): one at a time, picked
	// from pills, drawn like the ANSI they are.
	import AnsiArt from '$lib/AnsiArt.svelte';
	import type { DoorBulletinView } from '$lib/api';

	let { list }: { list: DoorBulletinView[] } = $props();
	let at = $state(0);
	const cur = $derived(list[Math.min(at, list.length - 1)]);
</script>

{#if list.length}
	{#if list.length > 1}
		<div class="mb-3 flex flex-wrap gap-1.5">
			{#each list as b, i (b.door + b.title)}
				<button class="pill {i === at ? 'pill-active' : ''}" onclick={() => (at = i)}>{b.title}</button>
			{/each}
		</div>
	{/if}
	<div class="ansi-panel">
		<AnsiArt grid={cur.grid} fit maxZoom={1.3} />
	</div>
	<p class="mt-1.5 text-right text-[11px] text-faint">{cur.door} · as of {new Date(cur.updated).toLocaleString()}</p>
{/if}
