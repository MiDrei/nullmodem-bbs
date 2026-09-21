<script lang="ts">
	// True root layout, shared by both the sysop admin panel
	// (routes/admin/*, its own +layout.svelte) and the BBS user portal
	// (routes/(portal)/*, its own +layout.svelte) -- deliberately no
	// nav/chrome of its own so neither section leaks into the other;
	// app.css is imported once here rather than in each section's own
	// layout since Vite/SvelteKit resolve it for the whole tree from
	// wherever it's first imported.
	import '../app.css';

	let { children } = $props();
</script>

{@render children()}
