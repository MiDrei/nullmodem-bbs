<script lang="ts">
	// A dialog over the page: closes on Escape, on a click on the dimmed
	// backdrop and from its own Close button. Focuses its first field when
	// it opens so a sysop can start typing straight away.
	import { onMount, type Snippet } from 'svelte';

	let {
		title,
		onclose,
		wide = false,
		children
	}: { title: string; onclose: () => void; wide?: boolean; children: Snippet } = $props();

	let box = $state<HTMLDivElement | null>(null);

	onMount(() => {
		box?.querySelector<HTMLElement>('input:not([type="hidden"]):not([disabled]), select, textarea')?.focus();
	});

	function keydown(e: KeyboardEvent) {
		if (e.key === 'Escape') onclose();
	}
</script>

<svelte:window onkeydown={keydown} />

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div
	class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/50 p-4 md:pt-[8vh]"
	onclick={(e) => {
		if (e.target === e.currentTarget) onclose();
	}}
>
	<div
		bind:this={box}
		role="dialog"
		aria-modal="true"
		aria-label={title}
		class="w-full {wide
			? 'max-w-4xl'
			: 'max-w-xl'} rounded-2xl border border-line-strong bg-surface p-6 shadow-2xl shadow-black/40"
	>
		<div class="mb-5 flex items-center justify-between gap-4">
			<h2 class="text-base font-semibold text-ink-strong">{title}</h2>
			<button type="button" class="btn-secondary btn-xs" onclick={onclose}>Close</button>
		</div>
		{@render children()}
	</div>
</div>
