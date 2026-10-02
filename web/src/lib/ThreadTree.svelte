<script lang="ts">
	// A message's thread as an indented list: who answered whom, the
	// open message marked, unread ones bold. Shared by the portal
	// (links) and the reader app (onselect).
	import type { ThreadEntry } from '$lib/api';
	import { formatDate } from '$lib/datetime';

	let {
		entries,
		current,
		href,
		onselect
	}: {
		entries: ThreadEntry[];
		current: number;
		href?: (id: number) => string;
		onselect?: (id: number) => void;
	} = $props();

	// Deep threads stay readable on a phone: indentation stops growing.
	const indent = (d: number) => Math.min(d, 6) * 14;
</script>

{#snippet row(e: ThreadEntry, here: boolean)}
	{#if e.depth > 0}<span class="shrink-0 text-faint" aria-hidden="true">{e.guessed ? '┄' : '└'}</span>{/if}
	<span class="min-w-0 flex-1 truncate {here ? '' : e.read ? 'text-ink-soft' : 'font-semibold text-ink-strong'}">
		{e.from_name}{#if e.depth === 0}<span class="font-normal text-faint">{` · ${e.subject}`}</span>{/if}
	</span>
	<span class="shrink-0 text-[11px] text-faint">{formatDate(e.posted_at)}</span>
{/snippet}

<ol class="flex flex-col">
	{#each entries as e (e.id)}
		{@const here = e.id === current}
		{@const cls = `flex w-full items-baseline gap-2 rounded-md py-1.5 pr-2 text-left text-[13px] transition-colors ${here ? 'bg-surface text-accent' : 'hover:bg-surface/60'}`}
		<li>
			{#if href}
				<a href={href(e.id)} data-sveltekit-replacestate class={cls} style="padding-left: {indent(e.depth) + 8}px" aria-current={here ? 'true' : undefined}>
					{@render row(e, here)}
				</a>
			{:else}
				<button type="button" onclick={() => onselect?.(e.id)} class={cls} style="padding-left: {indent(e.depth) + 8}px" aria-current={here ? 'true' : undefined}>
					{@render row(e, here)}
				</button>
			{/if}
		</li>
	{/each}
</ol>
