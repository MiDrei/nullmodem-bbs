<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { readerLayout } from '$lib/reader/layout.svelte';
	import NetmailPane from '$lib/reader/NetmailPane.svelte';

	let id = $derived(Number(page.params.id));

	$effect(() => {
		if (readerLayout.wide) goto(`/reader?n=1&m=${id}`, { replaceState: true });
	});
</script>

{#if !readerLayout.wide}
	<NetmailPane
		{id}
		onOpen={(next) => goto(`/reader/netmail/${next}`, { replaceState: true })}
		onBack={() => goto('/reader/netmail', { replaceState: true })}
	/>
{/if}
