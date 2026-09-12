import type { BoxCharSet } from './cp437';

export interface StampedCell {
	row: number;
	col: number;
	char: number;
}

// bresenhamLine returns every grid cell on the straight path between
// two points, for the designer's Line tool. Handles any direction
// (horizontal, vertical, diagonal) with a single algorithm rather
// than special-casing the axis-aligned case ANSI art usually wants.
export function bresenhamLine(r0: number, c0: number, r1: number, c1: number): { row: number; col: number }[] {
	const points: { row: number; col: number }[] = [];
	const dr = Math.abs(r1 - r0);
	const dc = Math.abs(c1 - c0);
	const sr = r0 < r1 ? 1 : -1;
	const sc = c0 < c1 ? 1 : -1;
	let err = dc - dr;
	let row = r0;
	let col = c0;
	for (;;) {
		points.push({ row, col });
		if (row === r1 && col === c1) break;
		const e2 = 2 * err;
		if (e2 > -dr) {
			err -= dr;
			col += sc;
		}
		if (e2 < dc) {
			err += dc;
			row += sr;
		}
	}
	return points;
}

export function rectCells(r0: number, c0: number, r1: number, c1: number, char: number): StampedCell[] {
	const top = Math.min(r0, r1);
	const bottom = Math.max(r0, r1);
	const left = Math.min(c0, c1);
	const right = Math.max(c0, c1);
	const cells: StampedCell[] = [];
	for (let row = top; row <= bottom; row++) {
		for (let col = left; col <= right; col++) {
			cells.push({ row, col, char });
		}
	}
	return cells;
}

// boxCells draws a rectangle outline using the four corner glyphs and
// horizontal/vertical edge glyphs of a CP437 box-drawing set (single
// or double line), auto-selecting the right glyph at each corner --
// the PabloDraw/Moebius-style "box tool" behavior. When fillInterior
// is set, the interior is stamped with fillChar first so the outline
// draws on top of it.
export function boxCells(
	r0: number,
	c0: number,
	r1: number,
	c1: number,
	set: BoxCharSet,
	fillInterior: boolean,
	fillChar: number
): StampedCell[] {
	const top = Math.min(r0, r1);
	const bottom = Math.max(r0, r1);
	const left = Math.min(c0, c1);
	const right = Math.max(c0, c1);
	const cells: StampedCell[] = [];

	if (fillInterior) {
		cells.push(...rectCells(top, left, bottom, right, fillChar));
	}

	if (top === bottom && left === right) {
		cells.push({ row: top, col: left, char: set.topLeft });
		return cells;
	}
	if (top === bottom) {
		for (let col = left; col <= right; col++) cells.push({ row: top, col, char: set.horizontal });
		cells.push({ row: top, col: left, char: set.topLeft });
		cells.push({ row: top, col: right, char: set.topRight });
		return cells;
	}
	if (left === right) {
		for (let row = top; row <= bottom; row++) cells.push({ row, col: left, char: set.vertical });
		cells.push({ row: top, col: left, char: set.topLeft });
		cells.push({ row: bottom, col: left, char: set.bottomLeft });
		return cells;
	}

	for (let col = left + 1; col < right; col++) {
		cells.push({ row: top, col, char: set.horizontal });
		cells.push({ row: bottom, col, char: set.horizontal });
	}
	for (let row = top + 1; row < bottom; row++) {
		cells.push({ row, col: left, char: set.vertical });
		cells.push({ row, col: right, char: set.vertical });
	}
	cells.push({ row: top, col: left, char: set.topLeft });
	cells.push({ row: top, col: right, char: set.topRight });
	cells.push({ row: bottom, col: left, char: set.bottomLeft });
	cells.push({ row: bottom, col: right, char: set.bottomRight });
	return cells;
}
