<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';

	onMount(() => {
		goto(bbsAuth.token ? '/message-areas' : '/login');
	});
</script>
