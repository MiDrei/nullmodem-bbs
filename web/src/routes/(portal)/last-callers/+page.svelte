<script lang="ts">
	// The Last Callers list moved into the Community page.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';

	onMount(() => goto('/community?tab=callers', { replaceState: true }));
</script>
