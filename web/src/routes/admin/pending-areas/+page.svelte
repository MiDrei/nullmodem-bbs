<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listPendingAreas,
		updateMessageArea,
		approvePendingMessageArea,
		updateFileArea,
		approvePendingFileArea,
		deleteMessageArea,
		deleteFileArea,
		listGroups,
		ApiError,
		type MessageArea,
		type MessageAreaInput,
		type FileArea,
		type FileAreaInput
	} from '$lib/api';

	let messageAreas = $state<MessageArea[]>([]);
	let fileAreas = $state<FileArea[]>([]);
	let messageDrafts = $state<Record<number, MessageAreaInput>>({});
	let fileDrafts = $state<Record<number, FileAreaInput>>({});
	let groups = $state<string[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);
	let busyID = $state<string | null>(null);

	function messageDraftFrom(area: MessageArea): MessageAreaInput {
		return {
			tag: area.tag,
			name: area.name,
			description: area.description,
			network: area.network,
			min_sl_read: area.min_sl_read,
			min_sl_write: area.min_sl_write,
			sort_order: area.sort_order
		};
	}

	function fileDraftFrom(area: FileArea): FileAreaInput {
		return {
			tag: area.tag,
			name: area.name,
			description: area.description,
			network: area.network,
			min_sl_download: area.min_sl_download,
			min_sl_upload: area.min_sl_upload,
			sort_order: area.sort_order
		};
	}

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return true;
		}
		return false;
	}

	async function load() {
		if (!auth.token) return;
		try {
			const res = await listPendingAreas(auth.token);
			messageAreas = res.message_areas;
			fileAreas = res.file_areas;
			messageDrafts = Object.fromEntries(messageAreas.map((a) => [a.id, messageDraftFrom(a)]));
			fileDrafts = Object.fromEntries(fileAreas.map((a) => [a.id, fileDraftFrom(a)]));
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load pending areas.';
		} finally {
			loaded = true;
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		await load();
		try {
			groups = await listGroups(auth.token);
		} catch {
			// Non-critical: the Group field just falls back to free text.
		}
	});

	async function approveMessage(area: MessageArea) {
		if (!auth.token) return;
		busyID = `m${area.id}`;
		try {
			await updateMessageArea(auth.token, area.id, messageDrafts[area.id]);
			await approvePendingMessageArea(auth.token, area.id);
			messageAreas = messageAreas.filter((a) => a.id !== area.id);
			toast.push(`Approved "${messageDrafts[area.id].name}". It's now visible in the BBS.`, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not approve.', 'error');
		} finally {
			busyID = null;
		}
	}

	async function rejectMessage(area: MessageArea) {
		if (!auth.token) return;
		if (!confirm(`Reject and delete area "${area.tag}"? Any messages already tossed into it are lost.`))
			return;
		busyID = `m${area.id}`;
		try {
			await deleteMessageArea(auth.token, area.id);
			messageAreas = messageAreas.filter((a) => a.id !== area.id);
			toast.push(`Rejected "${area.tag}".`, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not reject.', 'error');
		} finally {
			busyID = null;
		}
	}

	async function approveFile(area: FileArea) {
		if (!auth.token) return;
		busyID = `f${area.id}`;
		try {
			await updateFileArea(auth.token, area.id, fileDrafts[area.id]);
			await approvePendingFileArea(auth.token, area.id);
			fileAreas = fileAreas.filter((a) => a.id !== area.id);
			toast.push(`Approved "${fileDrafts[area.id].name}". It's now visible in the BBS.`, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not approve.', 'error');
		} finally {
			busyID = null;
		}
	}

	async function rejectFile(area: FileArea) {
		if (!auth.token) return;
		if (!confirm(`Reject and delete area "${area.tag}"?`)) return;
		busyID = `f${area.id}`;
		try {
			await deleteFileArea(auth.token, area.id);
			fileAreas = fileAreas.filter((a) => a.id !== area.id);
			toast.push(`Rejected "${area.tag}".`, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not reject.', 'error');
		} finally {
			busyID = null;
		}
	}
</script>

<h1 class="mb-2 text-xl font-semibold text-slate-100">Pending Areas</h1>
<p class="mb-6 text-sm text-slate-400">
	Areas the BinkP tosser auto-created for an echo it hadn't seen before. They stay invisible
	everywhere in the BBS -- and out of the normal Message Areas / File Areas lists -- until you
	review and approve them here. Adjust name, description, group, and security levels below before
	approving; the tag itself is fixed (it must match the network's own AREA/echo tag).
</p>

<datalist id="groups-list">
	{#each groups as g (g)}
		<option value={g}></option>
	{/each}
</datalist>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	<section class="mb-8">
		<h2 class="mb-4 text-sm font-semibold tracking-wide text-cyan-400 uppercase">
			Message Areas
		</h2>
		{#if messageAreas.length === 0}
			<p class="text-sm text-slate-500">No pending message areas.</p>
		{:else}
			<div class="flex flex-col gap-4">
				{#each messageAreas as area (area.id)}
					{@const draft = messageDrafts[area.id]}
					<div class="rounded border border-slate-800 p-4">
						<div class="mb-3 font-mono text-xs text-slate-500">{area.tag}</div>
						<div class="grid grid-cols-2 gap-4">
							<label class="flex flex-col gap-1 text-sm">
								<span class="text-slate-400">Name</span>
								<input
									class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
									bind:value={draft.name}
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
							<label class="col-span-2 flex flex-col gap-1 text-sm">
								<span class="text-slate-400">Description</span>
								<input
									class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
									bind:value={draft.description}
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
								disabled={busyID === `m${area.id}`}
								onclick={() => approveMessage(area)}
							>
								{busyID === `m${area.id}` ? 'Approving…' : 'Save & Approve'}
							</button>
							<button
								class="rounded border border-red-800 px-3 py-1 text-sm text-red-400 hover:bg-red-950 disabled:opacity-50"
								disabled={busyID === `m${area.id}`}
								onclick={() => rejectMessage(area)}
							>
								Reject
							</button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</section>

	<section>
		<h2 class="mb-4 text-sm font-semibold tracking-wide text-cyan-400 uppercase">File Areas</h2>
		{#if fileAreas.length === 0}
			<p class="text-sm text-slate-500">No pending file areas.</p>
		{:else}
			<div class="flex flex-col gap-4">
				{#each fileAreas as area (area.id)}
					{@const draft = fileDrafts[area.id]}
					<div class="rounded border border-slate-800 p-4">
						<div class="mb-3 font-mono text-xs text-slate-500">{area.tag}</div>
						<div class="grid grid-cols-2 gap-4">
							<label class="flex flex-col gap-1 text-sm">
								<span class="text-slate-400">Name</span>
								<input
									class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
									bind:value={draft.name}
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
							<label class="col-span-2 flex flex-col gap-1 text-sm">
								<span class="text-slate-400">Description</span>
								<input
									class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
									bind:value={draft.description}
								/>
							</label>
							<label class="flex flex-col gap-1 text-sm">
								<span class="text-slate-400">Min SL to download</span>
								<input
									type="number"
									min="0"
									max="255"
									class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
									bind:value={draft.min_sl_download}
								/>
							</label>
							<label class="flex flex-col gap-1 text-sm">
								<span class="text-slate-400">Min SL to upload</span>
								<input
									type="number"
									min="0"
									max="255"
									class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-slate-100 focus:border-cyan-500 focus:outline-none"
									bind:value={draft.min_sl_upload}
								/>
							</label>
						</div>
						<div class="mt-4 flex gap-2">
							<button
								class="rounded bg-cyan-600 px-3 py-1 text-sm text-white hover:bg-cyan-500 disabled:opacity-50"
								disabled={busyID === `f${area.id}`}
								onclick={() => approveFile(area)}
							>
								{busyID === `f${area.id}` ? 'Approving…' : 'Save & Approve'}
							</button>
							<button
								class="rounded border border-red-800 px-3 py-1 text-sm text-red-400 hover:bg-red-950 disabled:opacity-50"
								disabled={busyID === `f${area.id}`}
								onclick={() => rejectFile(area)}
							>
								Reject
							</button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</section>
{/if}
