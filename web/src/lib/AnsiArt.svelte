<script lang="ts">
	// Static (non-interactive) renderer for a Grid of real ANSI/CP437
	// art -- draws authentic pixel-bitmap glyphs on a <canvas> (see
	// cp437-bitmap.ts), the same mechanism the admin ANSI screen
	// designer uses for its editable surface, instead of CSS/font-based
	// spans (internal/ansi.ToHTML's HTML output): a browser/system font
	// renders CP437 block-shading and line-drawing characters
	// inconsistently -- gaps between "solid" blocks, misaligned box
	// corners -- confirmed live against a real fsxNet ANSI ad. A canvas
	// of actual pixels sidesteps all of that.
	import { onMount } from 'svelte';
	import { drawGlyphCell } from '$lib/cp437-bitmap';
	import { DOS_PALETTE } from '$lib/cp437';
	import type { Grid } from '$lib/api';

	const CELL_W = 9;
	const CELL_H = 16;

	// zoom defaults a bit above native size -- confirmed live that raw
	// 9x16 cells (720px for 80 columns) read as cramped for real
	// prose-heavy ANSI ads; 1.1x (792px) still comfortably fits the
	// reader page's own content width (max-w-4xl minus its padding,
	// see routes/(portal)/+layout.svelte) with margin to spare, so the
	// box never needs to scroll.
	//
	// fit instead scales the art to the width it's given, up to maxZoom
	// -- for a page where it should be as large as the window allows
	// (the portal login's welcome screen) and never scroll. It's still
	// drawn cell by cell at the final size (times the screen's pixel
	// density), so it stays crisp rather than being stretched.
	let {
		grid,
		zoom = 1.1,
		fit = false,
		maxZoom = 1.6
	}: { grid: Grid; zoom?: number; fit?: boolean; maxZoom?: number } = $props();

	let canvas = $state<HTMLCanvasElement | undefined>();
	let boxWidth = $state(0);
	// The width it's drawn for. It only follows boxWidth on a real
	// change: a scroll pane's scrollbar appearing (the art made it
	// taller) narrows the box by a few pixels, the smaller art removes
	// the scrollbar again, and following every such step redrew it in
	// an endless loop that froze the page (the reader's split view).
	let drawWidth = $state(0);
	const HYSTERESIS = 24;

	$effect(() => {
		const w = boxWidth;
		if (w > 0 && (drawWidth === 0 || Math.abs(w - drawWidth) >= HYSTERESIS || w < drawWidth - 1)) {
			// Shrinking below what it's drawn at would overflow; a small
			// shrink is taken once, then growing back must be real.
			drawWidth = w;
		}
	});

	function effectiveZoom(): number {
		if (!fit || drawWidth <= 0) return zoom;
		return Math.min(maxZoom, drawWidth / (grid.width * CELL_W));
	}

	function draw() {
		if (!canvas) return;
		const ctx = canvas.getContext('2d');
		if (!ctx) return;
		const z = effectiveZoom();
		const dpr = fit && typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1;
		const cw = CELL_W * z * dpr;
		const ch = CELL_H * z * dpr;
		canvas.width = Math.round(grid.width * cw);
		canvas.height = Math.round(grid.height * ch);
		canvas.style.width = fit ? `${Math.floor(grid.width * CELL_W * z)}px` : '';
		canvas.style.height = fit ? `${Math.floor(grid.height * CELL_H * z)}px` : '';
		for (let row = 0; row < grid.height; row++) {
			for (let col = 0; col < grid.width; col++) {
				const cell = grid.cells[row * grid.width + col];
				drawGlyphCell(ctx, cell.char, col * cw, row * ch, cw, ch, DOS_PALETTE[cell.fg], DOS_PALETTE[cell.bg]);
			}
		}
	}

	onMount(draw);
	$effect(() => {
		grid;
		zoom;
		drawWidth;
		draw();
	});
</script>

{#if fit}
	<div class="w-full" bind:clientWidth={boxWidth}>
		<canvas bind:this={canvas} class="mx-auto block"></canvas>
	</div>
{:else}
	<canvas bind:this={canvas} class="block"></canvas>
{/if}
