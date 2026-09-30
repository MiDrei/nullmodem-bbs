<script lang="ts">
	// The reader's start: on a wide screen all three panes side by side,
	// else the area list, one screen at a time.
	import { goto } from '$app/navigation';
	import { readerLayout } from '$lib/reader/layout.svelte';
	import AreaListPane from '$lib/reader/AreaListPane.svelte';
	import SplitView from '$lib/reader/SplitView.svelte';
	import { page } from '$app/state';

	// Turned upright with something open beside the list: keep showing it.
	$effect(() => {
		if (readerLayout.wide) return;
		const q = page.url.searchParams;
		const m = q.get('m');
		if (q.get('n') === '1') goto(m ? `/reader/netmail/${m}` : '/reader/netmail', { replaceState: true });
		else if (m) goto(`/reader/m/${m}`, { replaceState: true });
		else if (q.get('a')) goto(`/reader/area/${q.get('a')}`, { replaceState: true });
	});
</script>

{#if readerLayout.wide}
	<SplitView />
{:else}
	<AreaListPane onArea={(id) => goto(`/reader/area/${id}`)} onNetmail={() => goto('/reader/netmail')} />
{/if}
