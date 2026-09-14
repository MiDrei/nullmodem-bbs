<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listMessageAreas,
		createMessageArea,
		updateMessageArea,
		deleteMessageArea,
		listGroups,
		ApiError,
		type MessageArea,
		type MessageAreaInput
	} from '$lib/api';

	function emptyDraft(): MessageAreaInput {
		return {
			tag: '',
			name: '',
			description: '',
			network: '',
			min_sl_read: 0,
			min_sl_write: 0,
			sort_order: 0
		};
	}

	let areas = $state<MessageArea[]>([]);
	let groups = $state<string[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let editingId = $state<number | null>(null);
	let draft = $state<MessageAreaInput>(emptyDraft());
	let saving = $state(false);

	let creating = $state(false);
	let newDraft = $state<MessageAreaInput>(emptyDraft());

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/login');
			return true;
		}
		return false;
	}

	async function load() {
		if (!auth.token) return;
		try {
			areas = await listMessageAreas(auth.token);
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load message areas.';
		} finally {
			loaded = true;
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/login');
			return;
		}
		await load();
		try {
			groups = await listGroups(auth.token);
		} catch {
			// Non-critical: the Group field just falls back to free text.
		}
	});

	function startEdit(area: MessageArea) {
		editingId = area.id;
		draft = {
			tag: area.tag,
			name: area.name,
			description: area.description,
			network: area.network,
			min_sl_read: area.min_sl_read,
			min_sl_write: area.min_sl_write,
			sort_order: area.sort_order
		};
	}

	async function saveEdit(id: number) {
		if (!auth.token) return;
		saving = true;
		try {
			const updated = await updateMessageArea(auth.token, id, draft);
			areas = areas.map((a) => (a.id === id ? updated : a));
			editingId = null;
			toast.push(`Saved "${updated.name}".`, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not save.', 'error');
		} finally {
			saving = false;
		}
	}

	async function createNew() {
		if (!auth.token) return;
		saving = true;
		try {
			const created = await createMessageArea(auth.token, newDraft);
			areas = [...areas, created];
			creating = false;
			newDraft = emptyDraft();
			toast.push(`Created "${created.name}".`, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not create area.', 'error');
		} finally {
			saving = false;
		}
	}

	async function remove(area: MessageArea) {
		if (!auth.token) return;
		if (!confirm(`Delete area "${area.name}"? This also deletes all its messages.`)) return;
		try {
			await deleteMessageArea(auth.token, area.id);
			areas = areas.filter((a) => a.id !== area.id);
			toast.push(`Deleted "${area.name}".`, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not delete area.', 'error');
		}
	}
</script>

<datalist id="groups-list">
	{#each groups as g (g)}
		<option value={g}></option>
	{/each}
</datalist>

<div class="mb-6 flex items-center justify-between">
	<h1 class="text-xl font-semibold text-slate-100">Message Areas</h1>
	<button
		class="rounded bg-cyan-600 px-3 py-1.5 text-sm text-white hover:bg-cyan-500"
		onclick={() => (creating = !creating)}
	>
		{creating ? 'Cancel' : '+ New Area'}
	</button>
</div>

{#if creating}
	<div class="mb-6 rounded border border-slate-800 p-4">
		<h2 class="mb-4 text-sm font-semibold tracking-wide text-cyan-400 uppercase">New Area</h2>
		<div class="grid grid-cols-2 gap-4">
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Tag</span>
				<input
					class="rounded border border-slate-700 bg-slate-900 px-2 py-1 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={newDraft.tag}
					placeholder="general"
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Name</span>
				<input
					class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={newDraft.name}
				/>
			</label>
			<label class="col-span-2 flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Description</span>
				<input
					class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={newDraft.description}
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Group</span>
				<input
					class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={newDraft.network}
					list="groups-list"
					placeholder="fsxNet, FidoNet… (blank for ungrouped)"
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Min SL to read</span>
				<input
					type="number"
					min="0"
					max="255"
					class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={newDraft.min_sl_read}
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Min SL to post</span>
				<input
					type="number"
					min="0"
					max="255"
					class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={newDraft.min_sl_write}
				/>
			</label>
		</div>
		<button
			class="mt-4 rounded bg-cyan-600 px-3 py-1.5 text-sm text-white hover:bg-cyan-500 disabled:opacity-50"
			disabled={saving}
			onclick={createNew}
		>
			{saving ? 'Creating…' : 'Create'}
		</button>
	</div>
{/if}

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	<div class="flex flex-col gap-4">
		{#each areas as area (area.id)}
			<div class="rounded border border-slate-800 p-4">
				{#if editingId === area.id}
					<div class="grid grid-cols-2 gap-4">
						<label class="flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Name</span>
							<input
								class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
								bind:value={draft.name}
							/>
						</label>
						<label class="flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Sort order</span>
							<input
								type="number"
								class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
								bind:value={draft.sort_order}
							/>
						</label>
						<label class="col-span-2 flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Description</span>
							<input
								class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
								bind:value={draft.description}
							/>
						</label>
						<label class="flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Group</span>
							<input
								class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
								bind:value={draft.network}
								list="groups-list"
								placeholder="fsxNet, FidoNet… (blank for ungrouped)"
							/>
						</label>
						<label class="flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Min SL to read</span>
							<input
								type="number"
								min="0"
								max="255"
								class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
								bind:value={draft.min_sl_read}
							/>
						</label>
						<label class="flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Min SL to post</span>
							<input
								type="number"
								min="0"
								max="255"
								class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
								bind:value={draft.min_sl_write}
							/>
						</label>
					</div>
					<div class="mt-4 flex gap-2">
						<button
							class="rounded bg-cyan-600 px-3 py-1 text-sm text-white hover:bg-cyan-500 disabled:opacity-50"
							disabled={saving}
							onclick={() => saveEdit(area.id)}
						>
							{saving ? 'Saving…' : 'Save'}
						</button>
						<button
							class="rounded border border-slate-700 px-3 py-1 text-sm hover:bg-slate-800"
							onclick={() => (editingId = null)}
						>
							Cancel
						</button>
					</div>
				{:else}
					<div class="flex items-start justify-between">
						<div>
							<div class="font-mono text-xs text-slate-500">{area.tag}</div>
							<div class="text-slate-100">
								{area.name}
								{#if area.network}
									<span
										class="ml-2 rounded bg-fuchsia-950 px-1.5 py-0.5 text-xs text-fuchsia-400"
										>{area.network}</span
									>
								{/if}
							</div>
							<div class="text-sm text-slate-400">{area.description}</div>
							<div class="mt-1 text-xs text-slate-500">
								Read: SL {area.min_sl_read} &middot; Post: SL {area.min_sl_write}
							</div>
						</div>
						<div class="flex gap-2">
							<button
								class="rounded border border-slate-700 px-3 py-1 text-sm hover:bg-slate-800"
								onclick={() => startEdit(area)}
							>
								Edit
							</button>
							<button
								class="rounded border border-red-800 px-3 py-1 text-sm text-red-400 hover:bg-red-950"
								onclick={() => remove(area)}
							>
								Delete
							</button>
						</div>
					</div>
				{/if}
			</div>
		{/each}
	</div>
{/if}
