<script lang="ts">
	import { fade } from 'svelte/transition';
	import { toast } from './toast.svelte';
</script>

<div class="pointer-events-none fixed right-4 bottom-4 z-50 flex flex-col gap-2">
	{#each toast.toasts as t (t.id)}
		<div
			transition:fade={{ duration: 200 }}
			class="pointer-events-auto rounded px-4 py-2 text-sm shadow-lg {t.kind === 'success'
				? 'bg-green-600 text-white'
				: 'bg-red-600 text-white'}"
		>
			{t.message}
		</div>
	{/each}
</div>
