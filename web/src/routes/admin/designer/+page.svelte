<script lang="ts">
	import { onMount, onDestroy, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listScreens,
		createScreen,
		deleteScreen,
		importScreen,
		getScreenGrid,
		saveScreenGrid,
		ApiError,
		type ScreenSummary,
		type Grid,
		type GridCell
	} from '$lib/api';
	import { charToCp437, DOS_PALETTE, COLOR_NAMES, BOX_SINGLE, BOX_DOUBLE } from '$lib/cp437';
	import { drawGlyphCell, drawGlyphOnly } from '$lib/cp437-bitmap';
	import { bresenhamLine, rectCells, boxCells, type StampedCell } from '$lib/designer-shapes';

	type Tool = 'pencil' | 'eraser' | 'text' | 'rect' | 'line' | 'box' | 'select' | 'paste';

	// CELL_W/CELL_H match the bitmap glyphs' native 8x16 aspect ratio
	// (see $lib/cp437-bitmap) so cells scale cleanly with zoom.
	const CELL_W = 9;
	const CELL_H = 16;
	const MAX_HISTORY = 50;

	let screens = $state<ScreenSummary[]>([]);
	let selectedName = $state<string | null>(null);
	let grid = $state<Grid | null>(null);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);
	let saving = $state(false);

	let currentChar = $state(0x20);
	let currentFG = $state(7);
	let currentBG = $state(0);
	let tool = $state<Tool>('pencil');
	let boxStyle = $state<'single' | 'double'>('single');
	let insertMode = $state(false);
	let fillInterior = $state(false);
	let zoom = $state(1);
	let rowOpIndex = $state(1);
	let selectedPlaceholder = $state('BBSNAME');

	let newName = $state('');
	let newWidth = $state(80);
	let newHeight = $state(25);
	let showNewForm = $state(false);
	let importInput = $state<HTMLInputElement | undefined>();

	let history = $state<Grid[]>([]);
	let future = $state<Grid[]>([]);
	let clipboard: Grid | null = null;
	let selection = $state<{ r0: number; c0: number; r1: number; c1: number } | null>(null);
	let textCursor = $state<{ row: number; col: number } | null>(null);
	let textSessionSaved = false;
	let cursorBlinkOn = true;
	let cursorBlinkTimer: ReturnType<typeof setInterval> | undefined;

	let baseCanvas = $state<HTMLCanvasElement | undefined>();
	let overlayCanvas = $state<HTMLCanvasElement | undefined>();
	let dragging = false;
	let dragStart: { row: number; col: number } | null = null;

	function cw() {
		return CELL_W * zoom;
	}
	function ch() {
		return CELL_H * zoom;
	}
	function idx(row: number, col: number) {
		return row * grid!.width + col;
	}

	function cloneGrid(g: Grid): Grid {
		return { width: g.width, height: g.height, cells: g.cells.map((c) => ({ ...c })) };
	}

	function pushHistory() {
		if (!grid) return;
		history.push(cloneGrid(grid));
		if (history.length > MAX_HISTORY) history.shift();
		future = [];
	}

	function undo() {
		if (!grid || history.length === 0) return;
		future.push(cloneGrid(grid));
		grid = history.pop()!;
		redrawAll();
	}

	function redo() {
		if (!grid || future.length === 0) return;
		history.push(cloneGrid(grid));
		grid = future.pop()!;
		redrawAll();
	}

	function blankRow(width: number): GridCell[] {
		return Array.from({ length: width }, () => ({ char: 0x20, fg: 7, bg: 0 }));
	}

	// rowOpIndex is the 1-based row number these operate relative to
	// (clamped to the grid's current bounds), so "insert above row 1"
	// / "insert below row N" / "delete row N" cover growing or
	// shrinking a screen by a whole line without needing a separate
	// row-selection UI.
	function insertBlankRowAt(atRow: number) {
		if (!grid) return;
		const cells = grid.cells.slice();
		cells.splice(atRow * grid.width, 0, ...blankRow(grid.width));
		grid = { width: grid.width, height: grid.height + 1, cells };
	}

	function insertRow(before: boolean) {
		if (!grid) return;
		const at = Math.min(Math.max(rowOpIndex - 1, 0), grid.height);
		const insertAt = before ? at : at + 1;
		pushHistory();
		insertBlankRowAt(insertAt);
		redrawAll();
	}

	function deleteRow() {
		if (!grid || grid.height <= 1) return;
		const at = Math.min(Math.max(rowOpIndex - 1, 0), grid.height - 1);
		pushHistory();
		const cells = grid.cells.slice();
		cells.splice(at * grid.width, grid.width);
		grid = { width: grid.width, height: grid.height - 1, cells };
		redrawAll();
	}

	// shiftRowRight/shiftRowLeft implement the text tool's insert-mode
	// character shifting, scoped to a single row (a fixed-width ANSI
	// grid has no line-wrapping concept to reflow into, so shifting
	// only ever happens within the row being edited).
	function shiftRowRight(row: number, fromCol: number) {
		if (!grid) return;
		for (let c = grid.width - 1; c > fromCol; c--) {
			grid.cells[idx(row, c)] = grid.cells[idx(row, c - 1)];
		}
	}

	function shiftRowLeft(row: number, fromCol: number) {
		if (!grid) return;
		for (let c = fromCol; c < grid.width - 1; c++) {
			grid.cells[idx(row, c)] = grid.cells[idx(row, c + 1)];
		}
		grid.cells[idx(row, grid.width - 1)] = { char: 0x20, fg: currentFG, bg: currentBG };
	}

	function rowRangeCells(row: number, fromCol: number, toCol: number): { row: number; col: number }[] {
		const cells: { row: number; col: number }[] = [];
		for (let c = fromCol; c <= toCol; c++) cells.push({ row, col: c });
		return cells;
	}

	// stampByte places one CP437 byte at textCursor and advances it,
	// honoring insertMode the same way a typed keystroke does. Shared
	// by keyboard typing and macro insertion so both follow identical
	// row-shift/wrap behavior.
	function stampByte(byteValue: number) {
		if (!grid || !textCursor) return;
		const { row, col } = textCursor;
		if (insertMode) {
			shiftRowRight(row, col);
			grid.cells[idx(row, col)] = { char: byteValue, fg: currentFG, bg: currentBG };
			redrawCells(rowRangeCells(row, col, grid.width - 1));
		} else {
			grid.cells[idx(row, col)] = { char: byteValue, fg: currentFG, bg: currentBG };
			redrawCells([{ row, col }]);
		}
		let nextCol = col + 1;
		let nextRow = row;
		if (nextCol >= grid.width) {
			nextCol = 0;
			nextRow = Math.min(row + 1, grid.height - 1);
		}
		textCursor = { row: nextRow, col: nextCol };
	}

	// asciiBytes converts a plain-ASCII string (placeholder names,
	// braces, colon -- never CP437 art bytes) to byte values directly,
	// which is exact for 0x20-0x7E without going through charToCp437.
	function asciiBytes(s: string): number[] {
		return Array.from(s).map((c) => c.charCodeAt(0));
	}

	// insertMacroBytes stamps a sequence of raw CP437 bytes at
	// textCursor as one undo step, requiring the text tool to already
	// have a cursor placed (clicking a cell is how the designer knows
	// *where* to insert).
	function insertMacroBytes(bytes: number[]) {
		if (!grid || tool !== 'text' || !textCursor) {
			toast.push('Select the Text tool and click a cell first.', 'error');
			return;
		}
		pushHistory();
		textSessionSaved = true;
		for (const b of bytes) stampByte(b);
		cursorBlinkOn = true;
		drawTextCursor();
	}

	function insertPlaceholder(name: string) {
		insertMacroBytes(asciiBytes(`{${name}}`));
	}

	// fillCount is the optional explicit repeat count for the next
	// {FILL:x} insertion ("" means the classic auto-distribute-to-
	// remaining-width behavior, matching a bare {FILL:x}).
	let fillCount = $state('');

	function insertFill(fillByte: number) {
		const count = fillCount.trim();
		const suffix = count && /^\d+$/.test(count) ? asciiBytes(`:${count}`) : [];
		insertMacroBytes([...asciiBytes('{FILL:'), fillByte, ...suffix, ...asciiBytes('}')]);
	}

	const PLACEHOLDERS = [
		'BBSNAME',
		'SYSOP',
		'VERSION',
		'NODE',
		'DATE',
		'TIME',
		'USERNAME',
		'SL',
		'TOTALCALLS'
	];

	async function loadScreens() {
		if (!auth.token) return;
		try {
			screens = await listScreens(auth.token);
			loadError = null;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : 'Could not load screens.';
			loaded = true;
			return;
		}
		// Reveal the toolbar/canvas markup now, before opening the first
		// screen: openScreen's canvas draw only works once its <canvas>
		// elements actually exist in the DOM, which they don't while
		// this `loaded` gate is still false (see the template below).
		loaded = true;
		if (screens.length > 0 && !selectedName) await openScreen(screens[0].name);
	}

	async function openScreen(name: string) {
		if (!auth.token) return;
		try {
			grid = await getScreenGrid(auth.token, name);
			selectedName = name;
			history = [];
			future = [];
			selection = null;
			textCursor = null;
			stopCursorBlink();
			await tick();
			redrawAll();
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			toast.push(err instanceof ApiError ? err.message : 'Could not open screen.', 'error');
		}
	}

	async function handleCreate() {
		if (!auth.token || !newName.trim()) return;
		const name = newName.trim().endsWith('.ans') ? newName.trim() : `${newName.trim()}.ans`;
		try {
			await createScreen(auth.token, name, newWidth, newHeight);
			toast.push(`Created ${name}.`, 'success');
			showNewForm = false;
			newName = '';
			await loadScreens();
			await openScreen(name);
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : 'Could not create screen.', 'error');
		}
	}

	async function handleDelete() {
		if (!auth.token || !selectedName) return;
		if (!confirm(`Delete ${selectedName}? This cannot be undone.`)) return;
		try {
			await deleteScreen(auth.token, selectedName);
			toast.push(`Deleted ${selectedName}.`, 'success');
			selectedName = null;
			grid = null;
			await loadScreens();
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : 'Could not delete screen.', 'error');
		}
	}

	async function handleImport(e: Event) {
		if (!auth.token) return;
		const input = e.target as HTMLInputElement;
		const file = input.files?.[0];
		if (!file) return;
		try {
			const result = await importScreen(auth.token, file);
			toast.push(`Imported ${result.name}.`, 'success');
			await loadScreens();
			await openScreen(result.name);
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : 'Could not import screen.', 'error');
		} finally {
			input.value = '';
		}
	}

	async function handleSave() {
		if (!auth.token || !selectedName || !grid) return;
		saving = true;
		try {
			await saveScreenGrid(auth.token, selectedName, grid);
			toast.push(`Saved ${selectedName}. Restart the bbs daemon for changes to take effect.`, 'success');
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : 'Could not save screen.', 'error');
		} finally {
			saving = false;
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		await loadScreens();
		window.addEventListener('keydown', onKeydown);
	});
	onDestroy(() => {
		window.removeEventListener('keydown', onKeydown);
		stopCursorBlink();
	});

	// --- Rendering ---

	function redrawAll() {
		if (!grid || !baseCanvas) return;
		const ctx = baseCanvas.getContext('2d');
		if (!ctx) return;
		baseCanvas.width = grid.width * cw();
		baseCanvas.height = grid.height * ch();
		if (overlayCanvas) {
			overlayCanvas.width = baseCanvas.width;
			overlayCanvas.height = baseCanvas.height;
		}
		for (let row = 0; row < grid.height; row++) {
			for (let col = 0; col < grid.width; col++) {
				drawCell(ctx, row, col);
			}
		}
	}

	function drawCell(ctx: CanvasRenderingContext2D, row: number, col: number) {
		if (!grid) return;
		const cell = grid.cells[idx(row, col)];
		drawGlyphCell(ctx, cell.char, col * cw(), row * ch(), cw(), ch(), DOS_PALETTE[cell.fg], DOS_PALETTE[cell.bg]);
	}

	function redrawCells(cells: { row: number; col: number }[]) {
		if (!baseCanvas) return;
		const ctx = baseCanvas.getContext('2d');
		if (!ctx) return;
		for (const c of cells) drawCell(ctx, c.row, c.col);
	}

	function clearOverlay() {
		if (!overlayCanvas) return;
		const ctx = overlayCanvas.getContext('2d');
		ctx?.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
	}

	function drawOverlayPreview(cells: StampedCell[]) {
		if (!overlayCanvas) return;
		clearOverlay();
		const ctx = overlayCanvas.getContext('2d');
		if (!ctx) return;
		for (const c of cells) {
			const x = c.col * cw();
			const y = c.row * ch();
			ctx.fillStyle = DOS_PALETTE[currentBG];
			ctx.globalAlpha = 0.85;
			ctx.fillRect(x, y, cw(), ch());
			ctx.globalAlpha = 1;
			drawGlyphOnly(ctx, c.char, x, y, cw(), ch(), DOS_PALETTE[currentFG]);
		}
	}

	function drawSelectionOverlay(r0: number, c0: number, r1: number, c1: number) {
		if (!overlayCanvas) return;
		clearOverlay();
		const ctx = overlayCanvas.getContext('2d');
		if (!ctx) return;
		const top = Math.min(r0, r1);
		const left = Math.min(c0, c1);
		const w = Math.abs(r1 - r0) + 1;
		const h = Math.abs(c1 - c0) + 1;
		ctx.strokeStyle = '#22d3ee';
		ctx.lineWidth = 2;
		ctx.strokeRect(left * cw() + 1, top * ch() + 1, h * cw() - 2, w * ch() - 2);
	}

	// The text tool has nothing else drawn on the overlay canvas, so
	// it owns the overlay exclusively while active: a blinking block
	// at textCursor shows exactly where the next keystroke will land.
	function drawTextCursor() {
		clearOverlay();
		if (tool !== 'text' || !textCursor || !cursorBlinkOn || !overlayCanvas) return;
		const ctx = overlayCanvas.getContext('2d');
		if (!ctx) return;
		ctx.fillStyle = '#22d3ee';
		ctx.globalAlpha = 0.6;
		ctx.fillRect(textCursor.col * cw(), textCursor.row * ch(), cw(), ch());
		ctx.globalAlpha = 1;
	}

	function startCursorBlink() {
		stopCursorBlink();
		cursorBlinkOn = true;
		drawTextCursor();
		cursorBlinkTimer = setInterval(() => {
			cursorBlinkOn = !cursorBlinkOn;
			drawTextCursor();
		}, 500);
	}

	function stopCursorBlink() {
		if (cursorBlinkTimer) clearInterval(cursorBlinkTimer);
		cursorBlinkTimer = undefined;
	}

	function selectTool(t: Tool) {
		tool = t;
		if (t !== 'text') {
			textCursor = null;
			stopCursorBlink();
			clearOverlay();
		}
	}

	// glyphPreview is a small Svelte action that draws one CP437
	// glyph's pixel bitmap onto a <canvas>, for the character palette
	// and its "currently selected" swatch -- the same bitmap-based
	// drawGlyphOnly the main designer surface uses, so what you pick
	// here looks exactly like what lands on the canvas.
	function glyphPreview(node: HTMLCanvasElement, params: { code: number; color: string; scale?: number }) {
		function draw(p: typeof params) {
			const scale = p.scale ?? 2;
			node.width = 8 * scale;
			node.height = 16 * scale;
			const ctx = node.getContext('2d');
			if (!ctx) return;
			ctx.clearRect(0, 0, node.width, node.height);
			drawGlyphOnly(ctx, p.code, 0, 0, node.width, node.height, p.color);
		}
		draw(params);
		return { update: draw };
	}

	// --- Pointer interaction ---

	function cellFromPointer(e: PointerEvent): { row: number; col: number } | null {
		if (!grid || !overlayCanvas) return null;
		const rect = overlayCanvas.getBoundingClientRect();
		const col = Math.floor((e.clientX - rect.left) / cw());
		const row = Math.floor((e.clientY - rect.top) / ch());
		if (row < 0 || row >= grid.height || col < 0 || col >= grid.width) return null;
		return { row, col };
	}

	function paintCell(row: number, col: number, char: number, fg: number, bg: number) {
		if (!grid) return;
		grid.cells[idx(row, col)] = { char, fg, bg };
		redrawCells([{ row, col }]);
	}

	function onPointerDown(e: PointerEvent) {
		const cell = cellFromPointer(e);
		if (!cell || !grid) return;
		overlayCanvas?.setPointerCapture(e.pointerId);

		if (tool === 'text') {
			textCursor = cell;
			textSessionSaved = false;
			startCursorBlink();
			return;
		}
		if (tool === 'paste') {
			if (!clipboard) {
				toast.push('Clipboard is empty. Select a region and Copy first.', 'error');
				return;
			}
			pushHistory();
			const touched: { row: number; col: number }[] = [];
			for (let r = 0; r < clipboard.height && cell.row + r < grid.height; r++) {
				for (let c = 0; c < clipboard.width && cell.col + c < grid.width; c++) {
					const src = clipboard.cells[r * clipboard.width + c];
					const row = cell.row + r;
					const col = cell.col + c;
					grid.cells[idx(row, col)] = { ...src };
					touched.push({ row, col });
				}
			}
			redrawCells(touched);
			return;
		}

		dragging = true;
		dragStart = cell;

		if (tool === 'pencil' || tool === 'eraser') {
			pushHistory();
			const char = tool === 'eraser' ? 0x20 : currentChar;
			const fg = tool === 'eraser' ? 7 : currentFG;
			const bg = tool === 'eraser' ? 0 : currentBG;
			paintCell(cell.row, cell.col, char, fg, bg);
		}
	}

	function onPointerMove(e: PointerEvent) {
		const cell = cellFromPointer(e);
		if (!cell || !grid) return;

		if (!dragging) {
			if (tool === 'select' && selection) drawSelectionOverlay(selection.r0, selection.c0, selection.r1, selection.c1);
			return;
		}
		if (!dragStart) return;

		if (tool === 'pencil' || tool === 'eraser') {
			const char = tool === 'eraser' ? 0x20 : currentChar;
			const fg = tool === 'eraser' ? 7 : currentFG;
			const bg = tool === 'eraser' ? 0 : currentBG;
			paintCell(cell.row, cell.col, char, fg, bg);
			return;
		}
		if (tool === 'rect') {
			drawOverlayPreview(rectCells(dragStart.row, dragStart.col, cell.row, cell.col, currentChar));
			return;
		}
		if (tool === 'line') {
			const points = bresenhamLine(dragStart.row, dragStart.col, cell.row, cell.col);
			drawOverlayPreview(points.map((p) => ({ ...p, char: currentChar })));
			return;
		}
		if (tool === 'box') {
			const set = boxStyle === 'single' ? BOX_SINGLE : BOX_DOUBLE;
			drawOverlayPreview(boxCells(dragStart.row, dragStart.col, cell.row, cell.col, set, fillInterior, currentChar));
			return;
		}
		if (tool === 'select') {
			selection = { r0: dragStart.row, c0: dragStart.col, r1: cell.row, c1: cell.col };
			drawSelectionOverlay(dragStart.row, dragStart.col, cell.row, cell.col);
			return;
		}
	}

	function onPointerUp(e: PointerEvent) {
		const cell = cellFromPointer(e);
		if (dragging && dragStart && cell && grid) {
			if (tool === 'rect') {
				pushHistory();
				commitStamped(rectCells(dragStart.row, dragStart.col, cell.row, cell.col, currentChar));
			} else if (tool === 'line') {
				pushHistory();
				const points = bresenhamLine(dragStart.row, dragStart.col, cell.row, cell.col);
				commitStamped(points.map((p) => ({ ...p, char: currentChar })));
			} else if (tool === 'box') {
				pushHistory();
				const set = boxStyle === 'single' ? BOX_SINGLE : BOX_DOUBLE;
				commitStamped(boxCells(dragStart.row, dragStart.col, cell.row, cell.col, set, fillInterior, currentChar));
			}
		}
		clearOverlay();
		if (tool === 'select' && selection) drawSelectionOverlay(selection.r0, selection.c0, selection.r1, selection.c1);
		dragging = false;
		dragStart = null;
	}

	function commitStamped(cells: StampedCell[]) {
		if (!grid) return;
		const touched: { row: number; col: number }[] = [];
		for (const c of cells) {
			if (c.row < 0 || c.row >= grid.height || c.col < 0 || c.col >= grid.width) continue;
			grid.cells[idx(c.row, c.col)] = { char: c.char, fg: currentFG, bg: currentBG };
			touched.push({ row: c.row, col: c.col });
		}
		redrawCells(touched);
	}

	function copySelection() {
		if (!grid || !selection) return;
		const top = Math.min(selection.r0, selection.r1);
		const bottom = Math.max(selection.r0, selection.r1);
		const left = Math.min(selection.c0, selection.c1);
		const right = Math.max(selection.c0, selection.c1);
		const w = right - left + 1;
		const h = bottom - top + 1;
		const cells: GridCell[] = [];
		for (let r = top; r <= bottom; r++) {
			for (let c = left; c <= right; c++) {
				cells.push({ ...grid.cells[idx(r, c)] });
			}
		}
		clipboard = { width: w, height: h, cells };
		toast.push(`Copied ${w}x${h} block. Switch to Paste and click to stamp it.`, 'success');
	}

	// --- Keyboard (undo/redo + text tool typing) ---

	function onKeydown(e: KeyboardEvent) {
		if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'z') {
			e.preventDefault();
			if (e.shiftKey) redo();
			else undo();
			return;
		}
		if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'y') {
			e.preventDefault();
			redo();
			return;
		}
		if (tool !== 'text' || !textCursor || !grid) return;

		if (e.key === 'Escape') {
			textCursor = null;
			stopCursorBlink();
			clearOverlay();
			return;
		}
		if (e.key === 'ArrowUp' || e.key === 'ArrowDown' || e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
			e.preventDefault();
			let { row, col } = textCursor;
			if (e.key === 'ArrowUp') row = Math.max(row - 1, 0);
			if (e.key === 'ArrowDown') row = Math.min(row + 1, grid.height - 1);
			if (e.key === 'ArrowLeft') col = Math.max(col - 1, 0);
			if (e.key === 'ArrowRight') col = Math.min(col + 1, grid.width - 1);
			textCursor = { row, col };
			cursorBlinkOn = true;
			drawTextCursor();
			return;
		}
		if (!textSessionSaved) {
			pushHistory();
			textSessionSaved = true;
		}
		if (e.key === 'Backspace') {
			e.preventDefault();
			let { row, col } = textCursor;
			col--;
			if (col < 0 && row > 0) {
				row--;
				col = grid.width - 1;
			}
			if (col < 0) col = 0;
			if (insertMode) {
				shiftRowLeft(row, col);
				redrawCells(rowRangeCells(row, col, grid.width - 1));
			} else {
				grid.cells[idx(row, col)] = { char: 0x20, fg: currentFG, bg: currentBG };
				redrawCells([{ row, col }]);
			}
			textCursor = { row, col };
			cursorBlinkOn = true;
			drawTextCursor();
			return;
		}
		if (e.key === 'Enter') {
			e.preventDefault();
			if (insertMode) {
				const newRow = textCursor.row + 1;
				insertBlankRowAt(newRow);
				textCursor = { row: newRow, col: 0 };
				redrawAll();
			} else {
				const row = Math.min(textCursor.row + 1, grid.height - 1);
				textCursor = { row, col: 0 };
			}
			cursorBlinkOn = true;
			drawTextCursor();
			return;
		}
		if (e.key.length === 1) {
			e.preventDefault();
			stampByte(charToCp437(e.key));
			cursorBlinkOn = true;
			drawTextCursor();
		}
	}
</script>

<!--
	The rest of the admin UI is intentionally width-capped by the root
	layout's <main class="max-w-5xl">, but the designer needs all the
	room it can get for the palette + canvas side by side. This breaks
	just this page out to the full viewport width (minus a small side
	gutter) regardless of that ancestor constraint: 50vw is relative to
	the viewport, not the parent, so `calc(50% - 50vw)` is exactly the
	negative margin needed to cancel the parent's centering.
-->
<div class="mx-[calc(50%-50vw)] px-4 lg:px-8">
	<div class="mb-4">
		<h1 class="text-xl font-semibold text-slate-100">ANSI Designer</h1>
	<p class="mt-1 text-sm text-slate-500">
		Draw and edit .ans screen files directly in the browser. Changes are saved back to the
		screens directory; restart the bbs daemon to see them live.
	</p>
	<p class="mt-1 text-sm text-slate-500">
		Pick a tool, a foreground/background color and a character on the left, then click or drag on
		the canvas to draw with them.
	</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	<div class="mb-4 flex flex-wrap items-center gap-2 rounded border border-slate-800 p-3">
		<select
			value={selectedName ?? ''}
			onchange={(e) => openScreen((e.target as HTMLSelectElement).value)}
			class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-sm text-slate-100 focus:border-cyan-500 focus:outline-none"
		>
			{#each screens as screen (screen.name)}
				<option value={screen.name}>{screen.name}</option>
			{/each}
		</select>
		<button
			class="rounded bg-cyan-600 px-3 py-1 text-sm text-white hover:bg-cyan-500 disabled:opacity-50"
			disabled={!selectedName || saving}
			onclick={handleSave}
		>
			{saving ? 'Saving…' : 'Save'}
		</button>
		<button
			class="rounded border border-slate-700 px-3 py-1 text-sm text-slate-300 hover:bg-slate-800"
			onclick={() => (showNewForm = !showNewForm)}
		>
			New…
		</button>
		<button
			class="rounded border border-slate-700 px-3 py-1 text-sm text-slate-300 hover:bg-slate-800"
			onclick={() => importInput?.click()}
		>
			Import…
		</button>
		<input
			bind:this={importInput}
			type="file"
			accept=".ans"
			class="hidden"
			onchange={handleImport}
		/>
		<button
			class="rounded border border-red-900 px-3 py-1 text-sm text-red-400 hover:bg-red-950 disabled:opacity-50"
			disabled={!selectedName}
			onclick={handleDelete}
		>
			Delete
		</button>
		<div class="ml-auto flex items-center gap-2 text-sm text-slate-400">
			<span>Zoom</span>
			<select
				bind:value={zoom}
				onchange={() => redrawAll()}
				class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
			>
				<option value={1}>1x</option>
				<option value={1.5}>1.5x</option>
				<option value={2}>2x</option>
			</select>
		</div>
	</div>

	{#if showNewForm}
		<div class="mb-4 flex flex-wrap items-end gap-2 rounded border border-slate-800 p-3">
			<label class="text-sm text-slate-400">
				Name
				<input
					bind:value={newName}
					placeholder="myscreen.ans"
					class="mt-1 block w-40 rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
				/>
			</label>
			<label class="text-sm text-slate-400">
				Width
				<input
					type="number"
					min="1"
					max="240"
					bind:value={newWidth}
					class="mt-1 block w-20 rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
				/>
			</label>
			<label class="text-sm text-slate-400">
				Height
				<input
					type="number"
					min="1"
					max="500"
					bind:value={newHeight}
					class="mt-1 block w-20 rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
				/>
			</label>
			<button
				class="rounded bg-cyan-600 px-3 py-1 text-sm text-white hover:bg-cyan-500"
				onclick={handleCreate}
			>
				Create
			</button>
		</div>
	{/if}

	{#if grid}
		<div class="flex flex-col gap-4 lg:flex-row">
			<div class="flex shrink-0 flex-col gap-4 lg:w-80">
				<section class="rounded border border-slate-800 p-3">
					<h2 class="mb-2 card-label">Tools</h2>
					<div class="grid grid-cols-4 gap-1">
						{#each [['pencil', '✎'], ['eraser', '⌫'], ['text', 'T'], ['rect', '▭'], ['line', '╱'], ['box', '▢'], ['select', '⛶'], ['paste', '📋']] as [t, icon]}
							<button
								class="rounded border px-2 py-1.5 text-sm {tool === t
									? 'border-cyan-600 bg-cyan-950 text-cyan-300'
									: 'border-slate-800 text-slate-400 hover:border-slate-700'}"
								onclick={() => selectTool(t as Tool)}
								title={t}
							>
								{icon}
							</button>
						{/each}
					</div>
					{#if tool === 'box'}
						<div class="mt-2 flex items-center gap-2 text-xs text-slate-400">
							<select bind:value={boxStyle} class="rounded border border-slate-700 bg-slate-900 px-1 py-0.5">
								<option value="single">Single</option>
								<option value="double">Double</option>
							</select>
							<label class="flex items-center gap-1">
								<input type="checkbox" bind:checked={fillInterior} class="check" />
								Fill
							</label>
						</div>
					{/if}
					{#if tool === 'select'}
						<button
							class="mt-2 w-full rounded border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800 disabled:opacity-50"
							disabled={!selection}
							onclick={copySelection}
						>
							Copy selection
						</button>
					{/if}
					{#if tool === 'text'}
						<label class="mt-2 flex items-center gap-1 text-xs text-slate-400">
							<input type="checkbox" bind:checked={insertMode} class="check" />
							Insert mode (shift rest of the row)
						</label>
					{/if}
					<div class="mt-2 flex gap-1">
						<button
							class="flex-1 rounded border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800 disabled:opacity-50"
							disabled={history.length === 0}
							onclick={undo}
						>
							Undo
						</button>
						<button
							class="flex-1 rounded border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800 disabled:opacity-50"
							disabled={future.length === 0}
							onclick={redo}
						>
							Redo
						</button>
					</div>
				</section>

				<section class="rounded border border-slate-800 p-3">
					<h2 class="mb-2 card-label">Rows</h2>
					<label class="mb-2 flex items-center gap-2 text-xs text-slate-400">
						Row #
						<input
							type="number"
							min="1"
							max={grid.height}
							bind:value={rowOpIndex}
							class="w-16 rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
						/>
						of {grid.height}
					</label>
					<div class="grid grid-cols-2 gap-1">
						<button
							class="rounded border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800"
							onclick={() => insertRow(true)}
						>
							Insert above
						</button>
						<button
							class="rounded border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800"
							onclick={() => insertRow(false)}
						>
							Insert below
						</button>
						<button
							class="col-span-2 rounded border border-red-900 px-2 py-1 text-xs text-red-400 hover:bg-red-950 disabled:opacity-50"
							disabled={grid.height <= 1}
							onclick={deleteRow}
						>
							Delete row
						</button>
					</div>
				</section>

				<section class="rounded border border-slate-800 p-3">
					<h2 class="mb-2 card-label">Insert Field</h2>
					<p class="mb-2 text-xs text-slate-500">
						Text tool + click a cell, then insert a placeholder or fill token there instead of
						typing braces by hand.
					</p>
					<div class="flex gap-1">
						<select
							bind:value={selectedPlaceholder}
							class="min-w-0 flex-1 rounded border border-slate-700 bg-slate-900 px-2 py-1 text-sm text-slate-100 focus:border-cyan-500 focus:outline-none"
						>
							{#each PLACEHOLDERS as p (p)}
								<option value={p}>{'{' + p + '}'}</option>
							{/each}
						</select>
						<button
							class="shrink-0 rounded bg-cyan-600 px-3 py-1 text-sm text-white hover:bg-cyan-500"
							onclick={() => insertPlaceholder(selectedPlaceholder)}
						>
							Insert
						</button>
					</div>
					<label class="mt-2 flex items-center gap-2 text-xs text-slate-400">
						Count
						<input
							type="number"
							min="1"
							class="w-20 rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
							bind:value={fillCount}
							placeholder="auto"
							title="Repeat exactly this many times instead of auto-filling remaining width"
						/>
					</label>
					<div class="mt-2 flex flex-wrap gap-1">
						<button
							class="rounded border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800"
							onclick={() => insertFill(0x20)}
						>
							Fill: space
						</button>
						<button
							class="rounded border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800"
							onclick={() => insertFill(0xcd)}
						>
							Fill: ═
						</button>
						<button
							class="rounded border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800"
							onclick={() => insertFill(0xc4)}
						>
							Fill: ─
						</button>
						<button
							class="rounded border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800"
							onclick={() => insertFill(0x2e)}
						>
							Fill: .
						</button>
						<button
							class="rounded border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800"
							onclick={() => insertFill(currentChar)}
							title="Uses the character currently selected below"
						>
							Fill: selected char
						</button>
					</div>
				</section>

				<section class="rounded border border-slate-800 p-3">
					<h2 class="mb-2 card-label">Foreground</h2>
					<div class="grid grid-cols-8 gap-1">
						{#each DOS_PALETTE as color, i}
							<button
								class="h-6 w-6 rounded border {currentFG === i ? 'border-cyan-400' : 'border-slate-700'}"
								style="background-color:{color}"
								title={COLOR_NAMES[i]}
								onclick={() => (currentFG = i)}
							></button>
						{/each}
					</div>
					<h2 class="mt-3 mb-2 card-label">Background</h2>
					<div class="grid grid-cols-8 gap-1">
						{#each DOS_PALETTE.slice(0, 8) as color, i}
							<button
								class="h-6 w-6 rounded border {currentBG === i ? 'border-cyan-400' : 'border-slate-700'}"
								style="background-color:{color}"
								title={COLOR_NAMES[i]}
								onclick={() => (currentBG = i)}
							></button>
						{/each}
					</div>
				</section>

				<section class="rounded border border-slate-800 p-3">
					<h2 class="mb-2 card-label">Character</h2>
					<div class="mb-2 flex items-center gap-3 rounded border border-slate-800 bg-slate-950 p-2">
						<span
							class="flex h-16 w-12 shrink-0 items-center justify-center rounded"
							style="background-color:{DOS_PALETTE[currentBG]}"
						>
							<canvas
								use:glyphPreview={{ code: currentChar, color: DOS_PALETTE[currentFG], scale: 4 }}
								style="image-rendering:pixelated"
							></canvas>
						</span>
						<span class="text-xs text-slate-500">
							Selected: code {currentChar}. Click a glyph below to change it, then draw with
							Pencil, Text, Line, Rect or Box.
						</span>
					</div>
					<div
						class="grid max-h-96 gap-1 overflow-y-auto"
						style="grid-template-columns: repeat(auto-fill, minmax(2.5rem, 1fr));"
					>
						{#each Array(256) as _, i}
							<button
								class="flex h-10 w-10 items-center justify-center rounded {currentChar === i
									? 'bg-cyan-600'
									: 'bg-slate-900 hover:bg-slate-800'}"
								title="Code {i}"
								onclick={() => (currentChar = i)}
							>
								<canvas
									use:glyphPreview={{ code: i, color: currentChar === i ? '#ffffff' : '#cbd5e1', scale: 2 }}
									style="image-rendering:pixelated"
								></canvas>
							</button>
						{/each}
					</div>
				</section>
			</div>

			<div class="min-w-0 flex-1 overflow-auto rounded border border-slate-800 bg-black p-4">
				<div class="flex items-start">
					<div class="mr-2 flex shrink-0 flex-col text-right font-mono text-[10px] text-slate-600 select-none">
						{#each Array(grid.height) as _, r}
							<button
								type="button"
								class="block w-full cursor-pointer text-right hover:text-cyan-400 {rowOpIndex === r + 1
									? 'text-cyan-400'
									: ''}"
								style="height:{ch()}px; line-height:{ch()}px;"
								title="Set Row # to {r + 1}"
								onclick={() => (rowOpIndex = r + 1)}
							>
								{r + 1}
							</button>
						{/each}
					</div>
					<div class="relative inline-block">
						<canvas bind:this={baseCanvas} class="block"></canvas>
						<canvas
							bind:this={overlayCanvas}
							class="absolute top-0 left-0 touch-none"
							onpointerdown={onPointerDown}
							onpointermove={onPointerMove}
							onpointerup={onPointerUp}
						></canvas>
					</div>
				</div>
			</div>
		</div>
	{/if}
{/if}
</div>
