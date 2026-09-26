<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { getConfig, putConfig, ApiError, type BBSConfig } from '$lib/api';

	let config = $state<BBSConfig | null>(null);
	let loadError = $state<string | null>(null);
	let saveError = $state<string | null>(null);
	let saveNote = $state<string | null>(null);
	let saving = $state(false);

	function addFTNAddress() {
		if (!config) return;
		config.ftn_addresses = [...config.ftn_addresses, ''];
	}

	function removeFTNAddress(index: number) {
		if (!config) return;
		config.ftn_addresses = config.ftn_addresses.filter((_, i) => i !== index);
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			config = await getConfig(auth.token);
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : 'Could not load configuration.';
		}
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (!config || !auth.token) return;
		saveError = null;
		saveNote = null;
		saving = true;
		try {
			const res = await putConfig(auth.token, config);
			config = res.config;
			saveNote = res.note;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			saveError = err instanceof ApiError ? err.message : 'Could not save configuration.';
		} finally {
			saving = false;
		}
	}
</script>

<div class="mb-6 flex items-center justify-between">
	<h1 class="page-title">BinkP</h1>
	<a
		href="/admin/binkp/uplinks"
		class="btn-secondary btn-sm"
	>
		Uplinks (Nodes/Points) &rarr;
	</a>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !config}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	<form class="flex flex-col gap-6" onsubmit={handleSubmit}>
		<section class="flex flex-col gap-4 rounded-xl border border-line p-4">
			<div class="flex items-center justify-between">
				<h2 class="card-label">
					FTN Address(es)
				</h2>
				<button
					type="button"
					class="btn-secondary btn-xs"
					onclick={addFTNAddress}
				>
					+ Add Address
				</button>
			</div>
			<p class="text-xs text-slate-500">
				Your node address(es) on whichever FTN-compatible network(s) you're a member of
				(FidoNet, fsxNet, etc.), if any. The first is "primary": stamped on outgoing netmail.
				More than one is only needed if a single uplink presents you with more than one network
				in the same BinkP session -- which of them a given uplink actually sees is configured per
				uplink (see the Uplinks page).
			</p>
			{#if config.ftn_addresses.length === 0}
				<p class="text-sm text-slate-500">None configured.</p>
			{/if}
			{#each config.ftn_addresses as _, i (i)}
				<div class="flex items-center gap-2">
					<input
						class="flex-1 field font-mono"
						bind:value={config.ftn_addresses[i]}
						placeholder="e.g. 1:234/56.0"
					/>
					<button
						type="button"
						class="btn-danger btn-sm"
						onclick={() => removeFTNAddress(i)}
					>
						Remove
					</button>
				</div>
			{/each}
		</section>

		{#if saveError}
			<p class="text-sm text-red-400">{saveError}</p>
		{/if}
		{#if saveNote}
			<p class="text-sm text-amber-400">{saveNote}</p>
		{/if}

		<button
			type="submit"
			disabled={saving}
			class="btn-primary"
		>
			{saving ? 'Saving…' : 'Save changes'}
		</button>
	</form>
{/if}
