<script lang="ts">
	// One message, full screen: swipe left for the next, right for the
	// previous (or the arrows at the bottom), Reply below it.
	import { untrack, tick, type Snippet } from 'svelte';
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
		position = '',
		after
	}: {
		title: string;
		from: string;
		to: string;
		postedAt: string;
		subject: string;
		bodyHtml: string;
		preformatted: boolean;
		grid?: Grid;
		/** Back to the list; none when the list is beside it. */
		onBack?: () => void;
		onPrev?: () => void;
		onNext?: () => void;
		onReply?: () => void;
		position?: string;
		/** Below the text: the thread, in the reader. */
		after?: Snippet;
	} = $props();

	// Swiping: the message follows the finger, slides out when let go
	// far enough and the next one slides in from the other side -- or
	// springs back (there's no message that way, or not far enough).
	const SLIDE_MS = 180;
	let startX = 0;
	let startY = 0;
	let tracking = false;
	let horizontal: boolean | null = null;
	let dx = $state(0);
	let animate = $state(false);
	let width = $state(0);
	// Which way the next message comes in from, once it has loaded.
	let entering: -1 | 1 | 0 = 0;

	function touchStart(e: TouchEvent) {
		if (e.touches.length !== 1) return;
		// ANSI art scrolls sideways itself; don't turn that into a page.
		if ((e.target as HTMLElement).closest('.no-swipe')) return;
		startX = e.touches[0].clientX;
		startY = e.touches[0].clientY;
		tracking = true;
		horizontal = null;
		animate = false;
	}

	function touchMove(e: TouchEvent) {
		if (!tracking) return;
		const mx = e.touches[0].clientX - startX;
		const my = e.touches[0].clientY - startY;
		if (horizontal === null && Math.abs(mx) + Math.abs(my) > 10) horizontal = Math.abs(mx) > Math.abs(my);
		if (!horizontal) return;
		// Nothing that way: it gives, but reluctantly.
		const blocked = (mx < 0 && !onNext) || (mx > 0 && !onPrev);
		dx = blocked ? mx * 0.25 : mx;
	}

	function touchEnd() {
		if (!tracking) return;
		tracking = false;
		if (!horizontal) return;
		if (dx <= -70 && onNext) slide(-1);
		else if (dx >= 70 && onPrev) slide(1);
		else {
			animate = true;
			dx = 0;
		}
	}

	// Slide the message out (dir -1: to the left, for the next one),
	// then show the other one, which comes in from the opposite side.
	function slide(dir: -1 | 1) {
		const go = dir < 0 ? onNext : onPrev;
		if (!go) return;
		animate = true;
		dx = dir * (width || 400);
		entering = dir;
		setTimeout(() => go(), SLIDE_MS);
		// It didn't load after all: back into view rather than gone.
		setTimeout(() => {
			if (entering) {
				entering = 0;
				animate = true;
				dx = 0;
			}
		}, 2500);
	}

	let root = $state<HTMLDivElement | undefined>();

	// A new message arrived: in from the side it was swiped from.
	$effect(() => {
		subject;
		postedAt;
		bodyHtml;
		// Runs for each message shown, not on a resize (untracked).
		untrack(() => enter());
	});

	function enter() {
		// Every new message starts at its top, in a pane or full screen.
		const pane = root?.closest('.pane');
		if (pane) pane.scrollTop = 0;
		else window.scrollTo(0, 0);
		if (!entering) return;
		const from = -entering * (width || 400);
		entering = 0;
		animate = false;
		dx = from;
		// Put it at the far side first (flushed, and a layout forced so
		// the browser has that as the start), then let it glide in. No
		// requestAnimationFrame: it doesn't run everywhere, and the
		// message would stay out of sight.
		tick().then(() => {
			void root?.offsetWidth;
			animate = true;
			dx = 0;
		});
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions (swiping is a shortcut; the arrows below do the same) -->
<div
	bind:this={root}
	class="r-full flex flex-col overflow-x-clip"
	ontouchstart={touchStart}
	ontouchmove={touchMove}
	ontouchend={touchEnd}
	ontouchcancel={touchEnd}
>
	<header class="r-bar">
		{#if onBack}
			<button class="r-btn text-3xl leading-none" onclick={onBack} aria-label="Back">‹</button>
		{/if}
		<span class="r-title text-sm font-normal text-muted">{title}</span>
		{#if position}<span class="text-xs text-faint">{position}</span>{/if}
	</header>

	<article
		class="min-w-0 flex-1 px-4 pt-4 pb-6"
		bind:clientWidth={width}
		style="transform: translateX({dx}px); transition: {animate
			? `transform ${SLIDE_MS}ms ease-out, opacity ${SLIDE_MS}ms ease-out`
			: 'none'}; opacity: {1 - Math.min(Math.abs(dx) / (width || 400), 1) * 0.6}; touch-action: pan-y"
	>
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
		{@render after?.()}
	</article>

	<nav
		class="sticky bottom-0 flex items-center justify-between border-t border-line bg-black/95 px-3 pt-2 backdrop-blur"
		style="padding-bottom: max(0.5rem, env(safe-area-inset-bottom))"
	>
		<button class="r-btn text-4xl leading-none" disabled={!onPrev} onclick={() => slide(1)} aria-label="Previous">‹</button>
		{#if onReply}
			<button class="btn-primary px-6 py-2.5 text-base" onclick={onReply}>Reply</button>
		{/if}
		<button class="r-btn text-4xl leading-none" disabled={!onNext} onclick={() => slide(-1)} aria-label="Next">›</button>
	</nav>
</div>
