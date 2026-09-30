<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { readerLayout } from '$lib/reader/layout.svelte';
	import MessageListPane from '$lib/reader/MessageListPane.svelte';

	let areaId = $derived(Number(page.params.id));

	// A wide screen shows it beside the areas.
	$effect(() => {
		if (readerLayout.wide) goto(`/reader?a=${areaId}`, { replaceState: true });
	});
</script>

{#if !readerLayout.wide}
	{#key areaId}
		<MessageListPane
			{areaId}
			onOpen={(id) => goto(`/reader/m/${id}`)}
			onBack={() => goto('/reader')}
		/>
	{/key}
{/if}
