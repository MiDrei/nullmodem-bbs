<script lang="ts">
	// The reader on a wide screen (tablet in landscape, desktop): areas,
	// the open area's messages and the open message side by side. The
	// area list folds away for a wider message, remembered on the device.
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { replaceState, afterNavigate } from '$app/navigation';
	import AreaListPane from '$lib/reader/AreaListPane.svelte';
	import MessageListPane from '$lib/reader/MessageListPane.svelte';
	import MessagePane from '$lib/reader/MessagePane.svelte';
	import NetmailListPane from '$lib/reader/NetmailListPane.svelte';
	import NetmailPane from '$lib/reader/NetmailPane.svelte';

	const COLLAPSED_KEY = 'nullmodem.reader.areasCollapsed';

	function num(name: string): number | null {
		const v = Number(page.url.searchParams.get(name));
		return v > 0 ? v : null;
	}

	// Where it opens: /reader?a=<area>&m=<message>, or n=1 for netmail
	// -- what the one-pane pages redirect to on a wide screen.
	let areaId = $state<number | null>(num('a'));
	let netmail = $state(page.url.searchParams.get('n') === '1');
	let messageId = $state<number | null>(num('m'));
	let collapsed = $state(false);
	let reloadKey = $state(0);

	// Nothing open yet: the area list stays, folded or not.
	let showAreas = $derived(!collapsed || (areaId == null && !netmail));

	// The open area and message go into the address, so a reload or
	// turning the tablet upright keeps them -- but only once SvelteKit's
	// router is up: replaceState before that threw, which aborted the
	// router's start and left the reader dead (message pane empty,
	// nothing clickable) until a reload.
	let routerReady = $state(false);
	afterNavigate(() => {
		routerReady = true;
	});

	$effect(() => {
		const q = new URLSearchParams();
		if (netmail) q.set('n', '1');
		else if (areaId != null) q.set('a', String(areaId));
		if (messageId != null) q.set('m', String(messageId));
		if (!routerReady) return;
		const s = q.toString();
		try {
			replaceState(s ? `/reader?${s}` : '/reader', {});
		} catch {
			// Only the address isn't updated; the reader works on.
		}
	});

	function openArea(id: number) {
		areaId = id;
		netmail = false;
		messageId = null;
	}

	function openNetmail() {
		netmail = true;
		areaId = null;
		messageId = null;
	}

	function toggle() {
		collapsed = !collapsed;
		try {
			localStorage.setItem(COLLAPSED_KEY, collapsed ? '1' : '');
		} catch {
			// Not remembered then.
		}
	}

	onMount(() => {
		try {
			collapsed = localStorage.getItem(COLLAPSED_KEY) === '1';
		} catch {
			// Default: open.
		}
	});
</script>

{#snippet toggleButton()}
	<button
		class="r-btn text-lg"
		onclick={toggle}
		aria-label={collapsed ? 'Show the areas' : 'Hide the areas'}
		title={collapsed ? 'Show the areas' : 'Hide the areas'}
	>
		{collapsed ? '»' : '«'}
	</button>
{/snippet}

<div class="split">
	{#if showAreas}
		<section class="pane w-[19rem] shrink-0 border-r border-line">
			<AreaListPane
				selectedAreaId={areaId}
				netmailSelected={netmail}
				{reloadKey}
				onArea={openArea}
				onNetmail={openNetmail}
			/>
		</section>
	{/if}

	<section class="pane w-[24rem] shrink-0 border-r border-line">
		{#if netmail}
			<NetmailListPane selectedId={messageId} onOpen={(id) => (messageId = id)} headerStart={toggleButton} />
		{:else if areaId != null}
			{#key areaId}
				<MessageListPane
					{areaId}
					selectedId={messageId}
					onOpen={(id) => (messageId = id)}
					onChanged={() => reloadKey++}
					headerStart={toggleButton}
				/>
			{/key}
		{:else}
			<p class="r-note">Choose an area.</p>
		{/if}
	</section>

	<section class="pane min-w-0 flex-1">
		{#if messageId != null && netmail}
			<NetmailPane id={messageId} onOpen={(id) => (messageId = id)} onRead={() => reloadKey++} />
		{:else if messageId != null}
			<MessagePane
				id={messageId}
				onOpen={(id) => (messageId = id)}
				onRead={(a) => {
					if (areaId == null) areaId = a;
					reloadKey++;
				}}
			/>
		{:else}
			<p class="r-note pt-24">Choose a message.</p>
		{/if}
	</section>
</div>

<style>
	.split {
		display: flex;
		height: calc(100dvh - env(safe-area-inset-top) - env(safe-area-inset-bottom));
	}
	.pane {
		height: 100%;
		overflow-y: auto;
		/* Room for the scrollbar always, so content never changes width
		   when one appears. */
		scrollbar-gutter: stable;
		overscroll-behavior: contain;
	}
	/* Inside a pane its own scroll box is the reference, not the page. */
	.pane :global(.r-bar) {
		top: 0;
	}
	.pane :global(.r-full) {
		min-height: 100%;
	}
</style>
