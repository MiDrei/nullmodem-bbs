<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { avatarGradient, initials } from '$lib/avatar';
	import { listBBSMessageAreas, ApiError, type BBSMessageArea } from '$lib/api';

	let areas = $state<BBSMessageArea[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			bbsAuth.clear();
			await goto('/login');
			return true;
		}
		return false;
	}

	onMount(async () => {
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		try {
			areas = await listBBSMessageAreas(bbsAuth.token);
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load message areas.';
		} finally {
			loaded = true;
		}
	});

	// Areas already arrive grouped by network (see ListAreaStats'
	// ORDER BY) -- fold them into named sections here instead of
	// repeating each area's network as a per-row label.
	let groups = $derived.by(() => {
		const out: { network: string; areas: BBSMessageArea[] }[] = [];
		for (const area of areas) {
			const label = area.network || 'Local';
			const last = out[out.length - 1];
			if (last && last.network === label) last.areas.push(area);
			else out.push({ network: label, areas: [area] });
		}
		return out;
	});
</script>

<div class="mb-6">
	<h1 class="text-2xl font-bold tracking-tight text-slate-100">Message Areas</h1>
	<p class="mt-1 text-sm text-slate-500">Echomail boards you can read and post to.</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else if areas.length === 0}
	<p class="text-sm text-slate-400">No message areas available to you yet.</p>
{:else}
	<div class="flex flex-col gap-6">
		{#each groups as group (group.network)}
			<div>
				<h2 class="mb-2 px-1 text-xs font-semibold tracking-widest text-slate-500 uppercase">
					{group.network}
				</h2>
				<div class="overflow-hidden rounded-2xl border border-slate-800/60 bg-slate-900/40">
					{#each group.areas as area, i (area.id)}
						<a
							href="/message-areas/{area.id}"
							class="group flex items-center gap-3 px-4 py-2.5 transition hover:bg-slate-800/60 {i > 0
								? 'border-t border-slate-800/60'
								: ''} {area.new > 0 ? 'border-l-2 border-l-fuchsia-400' : 'border-l-2 border-l-transparent'}"
						>
							<div
								class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-gradient-to-br text-xs font-bold text-white {avatarGradient(
									area.name
								)}"
							>
								{initials(area.name)}
							</div>
							<div class="min-w-0 flex-1">
								<div
									class="truncate text-sm font-medium {area.new > 0
										? 'text-slate-100'
										: 'text-slate-300'} transition group-hover:text-white"
								>
									{area.name}
								</div>
								{#if area.description}
									<div class="truncate text-xs text-slate-500">{area.description}</div>
								{/if}
							</div>
							{#if area.new > 0}
								<span
									class="rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-2 py-0.5 text-xs font-semibold text-white"
								>
									{area.new} new
								</span>
							{/if}
							<span class="w-12 shrink-0 text-right text-xs text-slate-500">{area.total}</span>
						</a>
					{/each}
				</div>
			</div>
		{/each}
	</div>
{/if}
