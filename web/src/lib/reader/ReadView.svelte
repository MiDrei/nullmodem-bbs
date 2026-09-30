<script lang="ts">
	// One message, full screen: swipe left for the next, right for the
	// previous (or the arrows at the bottom), Reply below it.
	import AnsiArt from '$lib/AnsiArt.svelte';
	import { formatDateTime } from '$lib/datetime';
	import type { Grid } from '$lib/api';

	let {
		title,
		from,
		to,
		postedAt,
		subject,
		bodyHtml,
		preformatted,
		grid,
		onBack,
		onPrev,
		onNext,
		onReply,
		position = ''
	}: {
		title: string;
		from: string;
		to: string;
		postedAt: string;
		subject: string;
		bodyHtml: string;
		preformatted: boolean;
		grid?: Grid;
		onBack: () => void;
		onPrev?: () => void;
		onNext?: () => void;
		onReply?: () => void;
		position?: string;
	} = $props();

	let startX = 0;
	let startY = 0;
	let tracking = false;

	function touchStart(e: TouchEvent) {
		if (e.touches.length !== 1) return;
		// ANSI art scrolls sideways itself; don't turn that into a page.
		if ((e.target as HTMLElement).closest('.no-swipe')) return;
		startX = e.touches[0].clientX;
		startY = e.touches[0].clientY;
		tracking = true;
	}

	function touchEnd(e: TouchEvent) {
		if (!tracking) return;
		tracking = false;
		const dx = e.changedTouches[0].clientX - startX;
		const dy = e.changedTouches[0].clientY - startY;
		if (Math.abs(dx) < 70 || Math.abs(dx) < Math.abs(dy) * 1.5) return;
		if (dx < 0) onNext?.();
		else onPrev?.();
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions (swiping is a shortcut; the arrows below do the same) -->
<div class="flex min-h-dvh flex-col" ontouchstart={touchStart} ontouchend={touchEnd}>
	<header class="r-bar">
		<button class="r-btn text-3xl leading-none" onclick={onBack} aria-label="Back">‹</button>
		<span class="r-title text-sm font-normal text-muted">{title}</span>
		{#if position}<span class="text-xs text-faint">{position}</span>{/if}
	</header>

	<article class="flex-1 px-4 pt-4 pb-28">
		<h1 class="text-lg leading-snug font-semibold text-ink-strong">{subject}</h1>
		<div class="mt-1 mb-4 text-[13px] text-muted">
			<span class="text-ink-soft">{from}</span> → {to} · {formatDateTime(postedAt)}
		</div>
		{#if preformatted && grid}
			<div class="no-swipe -mx-4 bg-[var(--color-ansi)] py-3">
				<AnsiArt {grid} fit maxZoom={1.2} />
			</div>
		{:else if preformatted}
			<div class="no-swipe -mx-4 overflow-x-auto bg-[var(--color-ansi)] p-4">
				<div class="inline-block font-mono text-[13px] leading-tight whitespace-pre">{@html bodyHtml}</div>
			</div>
		{:else}
			<div class="font-mono text-[14.5px] leading-relaxed break-words whitespace-pre-wrap text-ink">
				{@html bodyHtml}
			</div>
		{/if}
	</article>

	<nav
		class="fixed inset-x-0 bottom-0 flex items-center justify-between border-t border-line bg-black/95 px-3 pt-2 backdrop-blur"
		style="padding-bottom: max(0.5rem, env(safe-area-inset-bottom))"
	>
		<button class="r-btn text-4xl leading-none" disabled={!onPrev} onclick={() => onPrev?.()} aria-label="Previous">‹</button>
		{#if onReply}
			<button class="btn-primary px-6 py-2.5 text-base" onclick={onReply}>Reply</button>
		{/if}
		<button class="r-btn text-4xl leading-none" disabled={!onNext} onclick={() => onNext?.()} aria-label="Next">›</button>
	</nav>
</div>
