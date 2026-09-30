<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { readerLayout } from '$lib/reader/layout.svelte';
	import MessagePane from '$lib/reader/MessagePane.svelte';

	let id = $derived(Number(page.params.id));

	$effect(() => {
		if (readerLayout.wide) goto(`/reader?m=${id}`, { replaceState: true });
	});
</script>

{#if !readerLayout.wide}
	<MessagePane
		{id}
		onOpen={(next) => goto(`/reader/m/${next}`, { replaceState: true })}
		onBack={(areaId) => goto(`/reader/area/${areaId}`, { replaceState: true })}
	/>
{/if}
