<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';

	onMount(() => {
		goto(auth.token ? '/admin/dashboard' : '/admin/login');
	});
</script>
