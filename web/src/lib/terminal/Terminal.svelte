<script lang="ts">
	// The web terminal: a Telnet session on the BBS in the browser
	// (internal/web's /api/terminal bridges the WebSocket to Telnet),
	// drawn with the same pixel CP437 font as the ANSI art elsewhere.
	// A hidden text field takes the keys, so phones bring up their
	// keyboard; the key bar has what a phone keyboard lacks.
	import { t } from '$lib/i18n.svelte';
	import { onMount, onDestroy } from 'svelte';
	import { VT, keyBytes } from '$lib/terminal/vt';
	import { drawGlyphCell } from '$lib/cp437-bitmap';
	import { DOS_PALETTE, charToCp437 } from '$lib/cp437';

	const COLS = 80;
	const ROWS = 25;
	const CW = 9;
	const CH = 16;

	let canvas = $state<HTMLCanvasElement | undefined>();
	let input = $state<HTMLTextAreaElement | undefined>();
	let status = $state<'connecting' | 'connected' | 'closed'>('connecting');
	let closeReason = $state('');
	let ctrlNext = $state(false);

	const vt = new VT(COLS, ROWS);
	let ws: WebSocket | null = null;
	let frame = 0;
	let blinkOn = true;
	let blinkTimer: ReturnType<typeof setInterval> | undefined;
	let lastCursor = -1;

	function connect() {
		vt.reset();
		status = 'connecting';
		closeReason = '';
		const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
		ws = new WebSocket(`${proto}//${location.host}/api/terminal`);
		ws.binaryType = 'arraybuffer';
		ws.onopen = () => {
			status = 'connected';
			ws?.send(JSON.stringify({ cols: COLS, rows: ROWS }));
			input?.focus();
		};
		ws.onmessage = (e) => {
			if (e.data instanceof ArrayBuffer) {
				vt.write(new Uint8Array(e.data));
				schedule();
			}
		};
		ws.onclose = (e) => {
			status = 'closed';
			closeReason = e.reason || 'Disconnected.';
		};
		vt.onReply = (bytes) => send(bytes);
	}

	function send(bytes: number[]) {
		if (ws?.readyState === WebSocket.OPEN) ws.send(new Uint8Array(bytes));
	}

	function schedule() {
		if (frame) return;
		// No requestAnimationFrame alone: it doesn't run in a background
		// tab, and the screen would lag behind when you come back.
		frame = requestAnimationFrame(draw) || 0;
		setTimeout(() => {
			if (frame) draw();
		}, 50);
	}

	function draw() {
		if (frame) cancelAnimationFrame(frame);
		frame = 0;
		const ctx = canvas?.getContext('2d');
		if (!ctx) return;
		const cursor = vt.cy * COLS + vt.cx;
		if (lastCursor >= 0) vt.dirty.add(lastCursor);
		vt.dirty.add(cursor);
		for (const i of vt.dirty) {
			const cell = vt.cells[i];
			if (!cell) continue;
			const x = (i % COLS) * CW;
			const y = Math.floor(i / COLS) * CH;
			drawGlyphCell(ctx, cell.ch, x, y, CW, CH, DOS_PALETTE[cell.fg], DOS_PALETTE[cell.bg]);
			if (i === cursor && vt.cursorVisible && blinkOn && status === 'connected') {
				ctx.fillStyle = DOS_PALETTE[cell.fg === cell.bg ? 7 : cell.fg];
				ctx.fillRect(x, y + CH - 3, CW - 1, 2);
			}
		}
		vt.dirty.clear();
		lastCursor = cursor;
	}

	function onKeydown(e: KeyboardEvent) {
		if (status !== 'connected' || e.isComposing) return;
		let bytes = keyBytes(e, charToCp437);
		if (bytes && ctrlNext && bytes.length === 1 && /[a-z]/i.test(e.key)) {
			bytes = [e.key.toLowerCase().charCodeAt(0) - 96];
			ctrlNext = false;
		}
		if (bytes) {
			e.preventDefault();
			send(bytes);
		}
	}

	// Phones' keyboards often send no usable keydown: take what lands
	// in the field instead.
	function onInput() {
		if (!input) return;
		const text = input.value;
		input.value = '';
		if (!text || status !== 'connected') return;
		send(Array.from(text.replace(/\n/g, '\r'), (c) => (c === '\r' ? 13 : charToCp437(c))));
	}

	function onPaste(e: ClipboardEvent) {
		const text = e.clipboardData?.getData('text') ?? '';
		e.preventDefault();
		if (text) send(Array.from(text.replace(/\r?\n/g, '\r'), (c) => (c === '\r' ? 13 : charToCp437(c))));
	}

	function bar(seq: string) {
		send(Array.from(seq, (c) => c.charCodeAt(0)));
		input?.focus();
	}

	onMount(() => {
		const ctx = canvas?.getContext('2d');
		if (canvas && ctx) {
			canvas.width = COLS * CW;
			canvas.height = ROWS * CH;
		}
		connect();
		blinkTimer = setInterval(() => {
			blinkOn = !blinkOn;
			vt.dirty.add(vt.cy * COLS + vt.cx);
			draw();
		}, 530);
	});

	onDestroy(() => {
		clearInterval(blinkTimer);
		ws?.close();
	});
</script>

<div class="flex w-full flex-col items-center gap-2">
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="relative w-full max-w-[1100px] cursor-text" onclick={() => input?.focus()}>
		<canvas bind:this={canvas} class="block w-full bg-black" style="image-rendering: pixelated; aspect-ratio: {COLS * CW} / {ROWS * CH}"></canvas>
		<textarea
			bind:this={input}
			class="absolute top-0 left-0 h-px w-px opacity-0"
			autocapitalize="off"
			autocomplete="off"
			spellcheck="false"
			aria-label={t('web.terminal.input')}
			onkeydown={onKeydown}
			oninput={onInput}
			onpaste={onPaste}
		></textarea>
		{#if status !== 'connected'}
			<div class="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-black/70 text-sm text-ink">
				{#if status === 'connecting'}
					{t('web.terminal.connecting')}
				{:else}
					<span>{closeReason}</span>
					<button class="btn-primary btn-sm" onclick={connect}>{t('web.terminal.connect_again')}</button>
				{/if}
			</div>
		{/if}
	</div>
	<div class="flex flex-wrap justify-center gap-1.5 text-xs">
		<button class="btn-secondary btn-xs" onclick={() => bar('\x1b')}>Esc</button>
		<button class="btn-secondary btn-xs" onclick={() => bar('\t')}>Tab</button>
		<button class="btn-secondary btn-xs {ctrlNext ? 'ring-1 ring-accent' : ''}" onclick={() => { ctrlNext = !ctrlNext; input?.focus(); }}>Ctrl</button>
		<button class="btn-secondary btn-xs" onclick={() => bar('\x1b[A')}>↑</button>
		<button class="btn-secondary btn-xs" onclick={() => bar('\x1b[B')}>↓</button>
		<button class="btn-secondary btn-xs" onclick={() => bar('\x1b[D')}>←</button>
		<button class="btn-secondary btn-xs" onclick={() => bar('\x1b[C')}>→</button>
		<button class="btn-secondary btn-xs" onclick={() => bar('\x1b[5~')}>PgUp</button>
		<button class="btn-secondary btn-xs" onclick={() => bar('\x1b[6~')}>PgDn</button>
		<button class="btn-secondary btn-xs" onclick={() => bar('\x1a')}>^Z</button>
	</div>
</div>
