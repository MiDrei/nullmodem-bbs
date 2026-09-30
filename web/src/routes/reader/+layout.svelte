<script lang="ts">
	// The mobile reader: a small installable web app (add to home
	// screen) for reading and answering echomail and netmail on a
	// phone or tablet -- just that, none of the portal. Its own
	// manifest scope is /reader/, so the installed app stays in here.
	import Toaster from '$lib/Toaster.svelte';

	let { children } = $props();
</script>

<svelte:head>
	<title>Reader</title>
	<link rel="manifest" href="/reader/manifest.json" />
	<link rel="apple-touch-icon" href="/reader/icon-180.png" />
	<meta name="theme-color" content="#000000" />
	<meta name="apple-mobile-web-app-capable" content="yes" />
	<meta name="mobile-web-app-capable" content="yes" />
	<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent" />
	<meta name="apple-mobile-web-app-title" content="Reader" />
	<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
</svelte:head>

<div class="reader min-h-dvh bg-black text-ink">
	{@render children()}
</div>

<Toaster />

<style>
	.reader {
		padding: env(safe-area-inset-top) env(safe-area-inset-right) env(safe-area-inset-bottom)
			env(safe-area-inset-left);
		-webkit-tap-highlight-color: transparent;
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
		padding: 0 0.75rem;
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
		min-width: 2.75rem;
		min-height: 2.75rem;
		padding: 0 0.6rem;
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
		min-height: 3.5rem;
		padding: 0.6rem 1rem;
		border-bottom: 1px solid var(--color-line);
		text-align: left;
	}
	.reader :global(.r-row:active) {
		background: var(--color-surface);
	}
	.reader :global(.r-badge) {
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
	.reader :global(.r-note) {
		padding: 2rem 1rem;
		text-align: center;
		color: var(--color-muted);
	}
</style>
