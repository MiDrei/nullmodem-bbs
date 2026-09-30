<script lang="ts">
	import { goto } from '$app/navigation';
	import { readerLayout } from '$lib/reader/layout.svelte';
	import NetmailListPane from '$lib/reader/NetmailListPane.svelte';

	$effect(() => {
		if (readerLayout.wide) goto('/reader?n=1', { replaceState: true });
	});
</script>

{#if !readerLayout.wide}
	<NetmailListPane onOpen={(id) => goto(`/reader/netmail/${id}`)} onBack={() => goto('/reader')} />
{/if}
