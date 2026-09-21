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
	let { grid, zoom = 1.1 }: { grid: Grid; zoom?: number } = $props();

	let canvas = $state<HTMLCanvasElement | undefined>();

	function draw() {
		if (!canvas) return;
		const ctx = canvas.getContext('2d');
		if (!ctx) return;
		const cw = CELL_W * zoom;
		const ch = CELL_H * zoom;
		canvas.width = grid.width * cw;
		canvas.height = grid.height * ch;
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
		draw();
	});
</script>

<canvas bind:this={canvas} class="block"></canvas>
