// A small ANSI-BBS terminal emulator for the web terminal: the screen
// as CP437 cells with DOS colors, driven by the bytes the BBS sends --
// what SyncTERM or NetRunner would do, as far as BBSes and doors use
// it: cursor movement and positioning, erasing, colors (SGR, bold as
// bright), save/restore, insert/delete of characters and lines,
// scrolling, and the cursor position report doors ask for.

export interface Cell {
	ch: number; // CP437 byte
	fg: number; // 0-15, DOS palette
	bg: number; // 0-7
}

const ESC = 0x1b;

export class VT {
	cols: number;
	rows: number;
	cells: Cell[];
	/** Cells changed since the renderer last looked (indexes). */
	dirty = new Set<number>();
	cx = 0;
	cy = 0;
	cursorVisible = true;
	/** Bytes to send back (cursor position reports). */
	onReply?: (bytes: number[]) => void;

	private fg = 7;
	private bg = 0;
	private bold = false;
	private reverse = false;
	private savedX = 0;
	private savedY = 0;
	private pendingWrap = false;
	private state: 'text' | 'esc' | 'csi' = 'text';
	private params = '';

	constructor(cols = 80, rows = 25) {
		this.cols = cols;
		this.rows = rows;
		this.cells = Array.from({ length: cols * rows }, () => this.blank());
		this.markAll();
	}

	private blank(): Cell {
		return { ch: 32, fg: 7, bg: this.bg };
	}

	markAll() {
		for (let i = 0; i < this.cells.length; i++) this.dirty.add(i);
	}

	reset() {
		this.fg = 7;
		this.bg = 0;
		this.bold = this.reverse = false;
		this.cx = this.cy = 0;
		this.cells = Array.from({ length: this.cols * this.rows }, () => this.blank());
		this.markAll();
	}

	write(data: Uint8Array) {
		for (const b of data) this.byte(b);
	}

	private byte(b: number) {
		switch (this.state) {
			case 'esc':
				if (b === 0x5b) {
					this.state = 'csi';
					this.params = '';
				} else {
					this.state = 'text';
					if (b === 0x63) this.reset(); // ESC c
					else if (b === 0x37) this.save(); // ESC 7
					else if (b === 0x38) this.restore(); // ESC 8
				}
				return;
			case 'csi':
				if ((b >= 0x30 && b <= 0x3f) || b === 0x20) {
					if (this.params.length < 32) this.params += String.fromCharCode(b);
				} else {
					this.state = 'text';
					this.csi(String.fromCharCode(b));
				}
				return;
		}
		switch (b) {
			case ESC:
				this.state = 'esc';
				return;
			case 0x0d:
				this.cx = 0;
				this.pendingWrap = false;
				return;
			case 0x0a:
				this.lineFeed();
				return;
			case 0x08:
				if (this.cx > 0) this.cx--;
				this.pendingWrap = false;
				return;
			case 0x09:
				this.cx = Math.min(this.cols - 1, (Math.floor(this.cx / 8) + 1) * 8);
				return;
			case 0x0c:
				this.eraseDisplay(2);
				this.cx = this.cy = 0;
				return;
			case 0x07:
			case 0x00:
				return;
		}
		this.put(b);
	}

	private put(b: number) {
		if (this.pendingWrap) {
			this.cx = 0;
			this.lineFeed();
			this.pendingWrap = false;
		}
		let fg = this.fg + (this.bold ? 8 : 0);
		let bg = this.bg;
		if (this.reverse) [fg, bg] = [bg, fg & 7];
		const i = this.cy * this.cols + this.cx;
		this.cells[i] = { ch: b, fg, bg };
		this.dirty.add(i);
		if (this.cx === this.cols - 1) this.pendingWrap = true;
		else this.cx++;
	}

	private lineFeed() {
		if (this.cy === this.rows - 1) this.scrollUp(1);
		else this.cy++;
	}

	private scrollUp(n: number, top = 0) {
		const c = this.cols;
		for (let k = 0; k < n; k++) {
			this.cells.splice(top * c, c);
			this.cells.splice((this.rows - 1) * c, 0, ...Array.from({ length: c }, () => this.blank()));
		}
		for (let i = top * c; i < this.cells.length; i++) this.dirty.add(i);
	}

	private scrollDown(n: number, top: number) {
		const c = this.cols;
		for (let k = 0; k < n; k++) {
			this.cells.splice((this.rows - 1) * c, c);
			this.cells.splice(top * c, 0, ...Array.from({ length: c }, () => this.blank()));
		}
		for (let i = top * c; i < this.cells.length; i++) this.dirty.add(i);
	}

	private save() {
		this.savedX = this.cx;
		this.savedY = this.cy;
	}

	private restore() {
		this.cx = this.savedX;
		this.cy = this.savedY;
		this.pendingWrap = false;
	}

	private clear(from: number, to: number) {
		for (let i = Math.max(0, from); i < Math.min(to, this.cells.length); i++) {
			this.cells[i] = this.blank();
			this.dirty.add(i);
		}
	}

	private eraseDisplay(mode: number) {
		const at = this.cy * this.cols + this.cx;
		if (mode === 0) this.clear(at, this.cells.length);
		else if (mode === 1) this.clear(0, at + 1);
		else this.clear(0, this.cells.length);
	}

	private csi(final: string) {
		const priv = this.params.startsWith('?');
		const nums = (priv ? this.params.slice(1) : this.params).split(';').map((p) => (p === '' ? NaN : parseInt(p, 10)));
		const n = (i: number, def = 1) => (Number.isNaN(nums[i]) || nums[i] === undefined ? def : nums[i]);
		const clampX = (x: number) => Math.max(0, Math.min(this.cols - 1, x));
		const clampY = (y: number) => Math.max(0, Math.min(this.rows - 1, y));
		this.pendingWrap = false;
		const rowStart = this.cy * this.cols;
		switch (final) {
			case 'A':
				this.cy = clampY(this.cy - n(0));
				break;
			case 'B':
				this.cy = clampY(this.cy + n(0));
				break;
			case 'C':
				this.cx = clampX(this.cx + n(0));
				break;
			case 'D':
				this.cx = clampX(this.cx - n(0));
				break;
			case 'E':
				this.cy = clampY(this.cy + n(0));
				this.cx = 0;
				break;
			case 'F':
				this.cy = clampY(this.cy - n(0));
				this.cx = 0;
				break;
			case 'G':
				this.cx = clampX(n(0) - 1);
				break;
			case 'd':
				this.cy = clampY(n(0) - 1);
				break;
			case 'H':
			case 'f':
				this.cy = clampY(n(0) - 1);
				this.cx = clampX(n(1) - 1);
				break;
			case 'J':
				this.eraseDisplay(n(0, 0));
				if (n(0, 0) === 2) this.cx = this.cy = 0; // ANSI.SYS homes too
				break;
			case 'K': {
				const m = n(0, 0);
				if (m === 0) this.clear(rowStart + this.cx, rowStart + this.cols);
				else if (m === 1) this.clear(rowStart, rowStart + this.cx + 1);
				else this.clear(rowStart, rowStart + this.cols);
				break;
			}
			case 'X':
				this.clear(rowStart + this.cx, rowStart + Math.min(this.cols, this.cx + n(0)));
				break;
			case '@': {
				const k = Math.min(n(0), this.cols - this.cx);
				const row = this.cells.slice(rowStart, rowStart + this.cols);
				row.splice(this.cx, 0, ...Array.from({ length: k }, () => this.blank()));
				row.length = this.cols;
				this.cells.splice(rowStart, this.cols, ...row);
				for (let i = rowStart + this.cx; i < rowStart + this.cols; i++) this.dirty.add(i);
				break;
			}
			case 'P': {
				const k = Math.min(n(0), this.cols - this.cx);
				const row = this.cells.slice(rowStart, rowStart + this.cols);
				row.splice(this.cx, k);
				while (row.length < this.cols) row.push(this.blank());
				this.cells.splice(rowStart, this.cols, ...row);
				for (let i = rowStart + this.cx; i < rowStart + this.cols; i++) this.dirty.add(i);
				break;
			}
			case 'L':
				this.scrollDown(Math.min(n(0), this.rows - this.cy), this.cy);
				break;
			case 'M':
				this.scrollUp(Math.min(n(0), this.rows - this.cy), this.cy);
				break;
			case 'S':
				this.scrollUp(n(0));
				break;
			case 'T':
				this.scrollDown(n(0), 0);
				break;
			case 's':
				this.save();
				break;
			case 'u':
				this.restore();
				break;
			case 'm':
				this.sgr(nums);
				break;
			case 'n':
				if (n(0, 0) === 6) {
					const reply = `\x1b[${this.cy + 1};${this.cx + 1}R`;
					this.onReply?.(Array.from(reply, (c) => c.charCodeAt(0)));
				}
				break;
			case 'h':
			case 'l':
				if (priv && n(0, 0) === 25) this.cursorVisible = final === 'h';
				break;
		}
	}

	private sgr(nums: number[]) {
		if (nums.length === 0) nums = [0];
		for (const raw of nums) {
			const p = Number.isNaN(raw) ? 0 : raw;
			if (p === 0) {
				this.fg = 7;
				this.bg = 0;
				this.bold = this.reverse = false;
			} else if (p === 1) this.bold = true;
			else if (p === 2 || p === 22) this.bold = false;
			else if (p === 7) this.reverse = true;
			else if (p === 27) this.reverse = false;
			else if (p >= 30 && p <= 37) this.fg = p - 30;
			else if (p === 39) this.fg = 7;
			else if (p >= 40 && p <= 47) this.bg = p - 40;
			else if (p === 49) this.bg = 0;
			else if (p >= 90 && p <= 97) {
				this.fg = p - 90;
				this.bold = true;
			} else if (p >= 100 && p <= 107) this.bg = p - 100;
		}
	}
}

/** What a key press sends to the BBS, or null for keys it doesn't use. */
export function keyBytes(e: KeyboardEvent, encode: (ch: string) => number): number[] | null {
	const seq = (s: string) => Array.from(s, (c) => c.charCodeAt(0));
	switch (e.key) {
		case 'Enter':
			return [13];
		case 'Backspace':
			return [8];
		case 'Tab':
			return [9];
		case 'Escape':
			return [27];
		case 'ArrowUp':
			return seq('\x1b[A');
		case 'ArrowDown':
			return seq('\x1b[B');
		case 'ArrowRight':
			return seq('\x1b[C');
		case 'ArrowLeft':
			return seq('\x1b[D');
		case 'Home':
			return seq('\x1b[H');
		case 'End':
			return seq('\x1b[F');
		case 'Delete':
			return seq('\x1b[3~');
		case 'PageUp':
			return seq('\x1b[5~');
		case 'PageDown':
			return seq('\x1b[6~');
		case 'Insert':
			return seq('\x1b[2~');
	}
	if (e.key.length === 1) {
		if (e.ctrlKey && !e.altKey && /[a-z]/i.test(e.key)) return [e.key.toLowerCase().charCodeAt(0) - 96];
		if (e.ctrlKey || e.metaKey) return null;
		return [encode(e.key)];
	}
	return null;
}
