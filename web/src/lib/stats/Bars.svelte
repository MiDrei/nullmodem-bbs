<script lang="ts">
	// A plain bar chart in SVG: one bar per value (two stacked, if b is
	// given), each with its tooltip; labels under the first and last.
	let {
		values,
		b,
		titles,
		first = '',
		last = '',
		height = 72
	}: {
		values: number[];
		/** Stacked on top, in the second colour. */
		b?: number[];
		titles: string[];
		first?: string;
		last?: string;
		height?: number;
	} = $props();

	const max = $derived(Math.max(1, ...values.map((v, i) => v + (b?.[i] ?? 0))));
	const w = 10;
	const gap = 2;
</script>

<div>
	<svg viewBox="0 0 {values.length * w} {height}" preserveAspectRatio="none" class="block w-full" style="height: {height}px" role="img">
		{#each values as v, i (i)}
			{@const hb = ((b?.[i] ?? 0) / max) * (height - 2)}
			{@const ha = (v / max) * (height - 2)}
			<g>
				<title>{titles[i]}</title>
				<rect x={i * w} y="0" width={w} height={height} fill="transparent" />
				{#if v > 0}<rect x={i * w + gap / 2} y={height - ha} width={w - gap} height={ha} rx="1.5" class="fill-accent" />{/if}
				{#if hb > 0}<rect x={i * w + gap / 2} y={height - ha - hb} width={w - gap} height={hb} rx="1.5" class="fill-accent/40" />{/if}
			</g>
		{/each}
	</svg>
	{#if first || last}
		<div class="mt-1 flex justify-between font-mono text-[10.5px] text-faint">
			<span>{first}</span><span>{last}</span>
		</div>
	{/if}
</div>
