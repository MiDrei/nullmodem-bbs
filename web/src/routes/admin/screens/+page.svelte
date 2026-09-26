<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { listScreens, previewScreen, ApiError, type ScreenSummary } from '$lib/api';

	let screens = $state<ScreenSummary[]>([]);
	let selected = $state<string | null>(null);
	let html = $state<string | null>(null);
	let loadError = $state<string | null>(null);
	let previewError = $state<string | null>(null);
	let loaded = $state(false);
	let previewLoading = $state(false);

	async function load() {
		if (!auth.token) return;
		try {
			screens = await listScreens(auth.token);
			loadError = null;
			if (screens.length > 0) await select(screens[0].name);
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : 'Could not load screens.';
		} finally {
			loaded = true;
		}
	}

	async function select(name: string) {
		if (!auth.token) return;
		selected = name;
		previewLoading = true;
		previewError = null;
		try {
			const preview = await previewScreen(auth.token, name);
			html = preview.html;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			html = null;
			previewError = err instanceof ApiError ? err.message : 'Could not render this screen.';
		} finally {
			previewLoading = false;
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		await load();
	});
</script>

<div class="mb-6">
	<h1 class="page-title">Screens</h1>
	<p class="mt-1 text-sm text-slate-500">
		Live preview of the ANSI/CP437 screen files under the configured screens directory, rendered
		as a browser would see a real terminal client display them. Placeholders like {'{BBSNAME}'} are
		filled in with sample values.
	</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else if screens.length === 0}
	<p class="text-sm text-slate-500">No .ans screens found.</p>
{:else}
	<div class="flex flex-col gap-6 md:flex-row">
		<nav class="flex shrink-0 flex-row flex-wrap gap-2 md:w-48 md:flex-col">
			{#each screens as screen (screen.name)}
				<button
					class="rounded border px-3 py-1.5 text-left text-sm {selected === screen.name
						? 'border-cyan-600 bg-cyan-950 text-cyan-300'
						: 'border-slate-800 text-slate-400 hover:border-slate-700 hover:text-slate-100'}"
					onclick={() => select(screen.name)}
				>
					{screen.name}
				</button>
			{/each}
		</nav>

		<div class="min-w-0 flex-1">
			{#if previewLoading}
				<p class="text-sm text-slate-400">Rendering…</p>
			{:else if previewError}
				<p class="text-sm text-red-400">{previewError}</p>
			{:else if html}
				<div class="overflow-x-auto rounded-xl border border-line bg-ansi p-4">
					<div class="inline-block font-mono text-sm leading-tight whitespace-pre">
						{@html html}
					</div>
				</div>
			{/if}
		</div>
	</div>
{/if}

<style>
	/* -global- so it survives being referenced from the raw {@html}
	   preview markup, which isn't compiled by Svelte's CSS scoping. */
	@keyframes -global-ansi-blink {
		50% {
			opacity: 0;
		}
	}
</style>
