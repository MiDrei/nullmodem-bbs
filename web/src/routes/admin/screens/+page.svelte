<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { listScreens, previewScreen, ApiError, type ScreenSummary } from '$lib/api';
	import { groupScreens } from '$lib/screen-groups';

	let screens = $state<ScreenSummary[]>([]);
	let selected = $state<string | null>(null);
	let html = $state<string | null>(null);
	let loadError = $state<string | null>(null);
	let previewError = $state<string | null>(null);
	let loaded = $state(false);
	let previewLoading = $state(false);
	let filter = $state('');

	let groups = $derived.by(() => {
		const q = filter.trim().toLowerCase();
		return groupScreens(screens.map((s) => s.name))
			.map((g) => ({
				...g,
				items: g.items.filter(
					(i) => !q || i.name.toLowerCase().includes(q) || i.label.toLowerCase().includes(q) || g.title.toLowerCase().includes(q)
				)
			}))
			.filter((g) => g.items.length > 0);
	});
	let selectedLabel = $derived.by(() => {
		for (const g of groupScreens(screens.map((s) => s.name))) {
			const i = g.items.find((x) => x.name === selected);
			if (i) return `${g.title} · ${i.part ? `${i.label} (part)` : i.label}`;
		}
		return '';
	});

	async function load() {
		if (!auth.token) return;
		try {
			screens = await listScreens(auth.token);
			loadError = null;
			// The first in the grouped order (the welcome screen), not by name.
			const first = groupScreens(screens.map((s) => s.name))[0]?.items[0];
			if (first) await select(first.name);
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
		<nav class="flex shrink-0 flex-col gap-1 md:w-64">
			<input class="field field-sm mb-2" bind:value={filter} placeholder="Filter screens…" aria-label="Filter screens" />
			<div class="flex max-h-[70vh] flex-col gap-3 overflow-y-auto pr-1">
				{#each groups as g (g.title)}
					<div>
						<div class="card-label mb-1 px-2">{g.title}</div>
						{#each g.items as item (item.name)}
							<button
								class="flex w-full items-baseline justify-between gap-2 rounded-lg py-1.5 pr-2 text-left transition-colors {item.part
									? 'pl-6 text-[12.5px]'
									: 'pl-2 text-[13px]'} {selected === item.name
									? 'bg-surface text-accent'
									: item.part
										? 'text-muted hover:text-ink'
										: 'text-ink-soft hover:text-ink'}"
								onclick={() => select(item.name)}
								title={item.name}
							>
								<span class="truncate">{item.label}</span>
								{#if !item.part}
									<span class="shrink-0 font-mono text-[10.5px] text-faint">{item.name.replace(/\.ans$/, '')}</span>
								{/if}
							</button>
						{/each}
					</div>
				{:else}
					<p class="px-2 text-sm text-muted">No screen matches.</p>
				{/each}
			</div>
		</nav>

		<div class="min-w-0 flex-1">
			{#if selected}
				<div class="mb-2 flex items-baseline justify-between gap-3">
					<span class="text-[13px] text-ink">{selectedLabel}</span>
					<span class="font-mono text-xs text-faint">{selected}</span>
				</div>
			{/if}
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
