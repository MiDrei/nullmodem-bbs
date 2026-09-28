<script lang="ts">
	import AnsiArt from '$lib/AnsiArt.svelte';
	import type { FilePreview } from '$lib/api';

	let {
		title,
		loading,
		error,
		preview,
		imageURL,
		onclose
	}: {
		title: string;
		loading: boolean;
		error: string | null;
		preview: FilePreview | null;
		imageURL: string | null;
		onclose: () => void;
	} = $props();

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') onclose();
	}
</script>

<svelte:window onkeydown={handleKeydown} />

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4" onclick={onclose}>
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		class="max-h-[85vh] w-full max-w-3xl overflow-auto rounded-2xl border border-slate-800 bg-slate-900 p-5 shadow-2xl"
		onclick={(e) => e.stopPropagation()}
		role="dialog"
		aria-modal="true"
		tabindex="-1"
	>
		<div class="mb-4 flex items-center justify-between gap-4">
			<h2 class="truncate text-sm font-semibold text-slate-100">{title}</h2>
			<button
				class="shrink-0 rounded-full border border-slate-700 px-2.5 py-1 text-xs text-slate-300 hover:bg-slate-800"
				onclick={onclose}
			>
				Close
			</button>
		</div>

		{#if loading}
			<p class="text-sm text-slate-400">Loading…</p>
		{:else if error}
			<p class="text-sm text-red-400">{error}</p>
		{:else if preview?.kind === 'image'}
			<div class="overflow-hidden rounded-xl border border-slate-800/60 bg-black">
				{#if imageURL}
					<img src={imageURL} alt={title} class="block max-w-full" />
				{/if}
			</div>
		{:else if preview?.kind === 'text'}
			<div class="overflow-x-auto rounded-xl border border-slate-800/60 bg-black p-4">
				{#if preview.preformatted && preview.grid}
					<AnsiArt grid={preview.grid} />
				{:else}
					<div class="inline-block font-mono text-sm leading-tight whitespace-pre text-[#d8d6d0]">
						{@html preview.body_html}
					</div>
				{/if}
			</div>
			{#if preview.truncated}
				<p class="mt-2 text-xs text-slate-500">Preview truncated -- download the file to see the rest.</p>
			{/if}
		{:else}
			<p class="text-sm text-slate-500">No preview available for this file.</p>
		{/if}
	</div>
</div>
