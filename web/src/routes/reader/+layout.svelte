<script lang="ts">
	// The mobile reader: a small installable web app (add to home
	// screen) for reading and answering echomail and netmail on a
	// phone or tablet -- just that, none of the portal. Its own
	// manifest scope is /reader/, so the installed app stays in here.
	import { t } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import Toaster from '$lib/Toaster.svelte';
	import { startOffline } from '$lib/reader/offline.svelte';

	onMount(startOffline);

	let { children } = $props();
</script>

<svelte:head>
	<title>{t('web.home.reader')}</title>
	<link rel="manifest" href="/reader/manifest.json" />
	<link rel="apple-touch-icon" href="/reader/icon-180.png" />
	<meta name="theme-color" content="#000000" />
	<meta name="apple-mobile-web-app-capable" content="yes" />
	<meta name="mobile-web-app-capable" content="yes" />
	<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent" />
	<meta name="apple-mobile-web-app-title" content="Reader" />
	<meta
		name="viewport"
		content="width=device-width, initial-scale=1, maximum-scale=1, viewport-fit=cover"
	/>
</svelte:head>

<div class="reader min-h-dvh bg-black text-ink">
	{@render children()}
</div>

<Toaster />

<style>
	/* Never wider than the screen: an iPhone showed the whole page a
	   bit wider than its display, cutting off the right edge. The page
	   itself can't scroll sideways; wide content (ANSI art) scrolls in
	   its own box. */
	:global(html:has(.reader)),
	:global(body:has(.reader)) {
		overflow-x: hidden;
		max-width: 100%;
		-webkit-text-size-adjust: 100%;
		text-size-adjust: 100%;
	}
	/* iOS zooms the whole page into any field with text smaller than
	   16px, and stays zoomed -- which cut off the right edge. */
	.reader :global(input),
	.reader :global(textarea),
	.reader :global(select) {
		font-size: max(16px, 1rem);
	}
	.reader {
		width: 100%;
		max-width: 100vw;
		overflow-x: clip;
		padding: env(safe-area-inset-top) env(safe-area-inset-right) env(safe-area-inset-bottom)
			env(safe-area-inset-left);
		-webkit-tap-highlight-color: transparent;
		overflow-wrap: anywhere;
	}
	/* Top bar and list rows shared by the reader's pages. */
	.reader :global(.r-bar) {
		position: sticky;
		top: env(safe-area-inset-top);
		z-index: 10;
		display: flex;
		align-items: center;
		gap: 0.5rem;
		min-height: 3.25rem;
		width: 100%;
		padding: 0 0.5rem 0 0.75rem;
		background: rgb(0 0 0 / 0.92);
		backdrop-filter: blur(8px);
		border-bottom: 1px solid var(--color-line);
	}
	.reader :global(.r-title) {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-weight: 600;
		font-size: 1.05rem;
		color: var(--color-ink-strong);
	}
	.reader :global(.r-btn) {
		flex-shrink: 0;
		min-width: 2.5rem;
		min-height: 2.75rem;
		padding: 0 0.45rem;
		white-space: nowrap;
		border-radius: 0.6rem;
		color: var(--color-accent);
		font-size: 1rem;
	}
	.reader :global(.r-btn:disabled) {
		color: var(--color-faint);
	}
	.reader :global(.r-row) {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		width: 100%;
		min-width: 0;
		min-height: 3.5rem;
		padding: 0.6rem 1rem;
		border-bottom: 1px solid var(--color-line);
		text-align: left;
	}
	.reader :global(.r-row:active) {
		background: var(--color-surface);
	}
	.reader :global(.r-badge) {
		flex-shrink: 0;
		min-width: 1.6rem;
		padding: 0.1rem 0.45rem;
		border-radius: 999px;
		background: var(--color-accent);
		color: #000;
		font-size: 0.8rem;
		font-weight: 600;
		text-align: center;
	}
	.reader :global(.r-section) {
		padding: 1rem 1rem 0.35rem;
		font-size: 0.75rem;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--color-muted);
	}
	/* A full screen (or pane) of the reader, below its safe areas. */
	.reader :global(.r-full) {
		min-height: calc(100dvh - env(safe-area-inset-top) - env(safe-area-inset-bottom));
	}
	.reader :global(.r-note) {
		padding: 2rem 1rem;
		text-align: center;
		color: var(--color-muted);
	}
</style>
