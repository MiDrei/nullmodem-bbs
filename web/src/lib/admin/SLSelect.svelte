<script lang="ts">
	// A security level chosen by its name ("20 – Regular user"): the
	// named levels between min and max, plus the current value when it
	// has no name, so nothing set before gets lost.
	import { onMount } from 'svelte';
	import { levels } from '$lib/admin/levels.svelte';

	let {
		value = $bindable(),
		min = 0,
		max = 255,
		class: cls = 'field field-sm',
		disabled = false,
		label = ''
	}: { value: number; min?: number; max?: number; class?: string; disabled?: boolean; label?: string } = $props();

	onMount(() => {
		levels.load();
	});

	const options = $derived.by(() => {
		const named = levels.list.filter((l) => l.level >= min && l.level <= max).map((l) => l.level);
		if (value !== undefined && value !== null && !named.includes(value)) named.push(value);
		return named.sort((a, b) => a - b);
	});
</script>

<select class={cls} bind:value {disabled} aria-label={label || undefined}>
	{#each options as o (o)}
		<option value={o}>{levels.label(o)}</option>
	{/each}
</select>
