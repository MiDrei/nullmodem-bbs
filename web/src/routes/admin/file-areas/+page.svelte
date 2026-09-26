<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listFileAreas,
		createFileArea,
		updateFileArea,
		deleteFileArea,
		listAreaFiles,
		uploadAreaFile,
		deleteFile,
		listGroups,
		ApiError,
		type FileArea,
		type FileAreaInput,
		type BBSFile
	} from '$lib/api';

	function emptyDraft(): FileAreaInput {
		return {
			tag: '',
			name: '',
			description: '',
			network: '',
			min_sl_download: 0,
			min_sl_upload: 0,
			sort_order: 0
		};
	}

	const UNGROUPED = 'Ungrouped';
	const ALL_TAB = '__all__';

	let areas = $state<FileArea[]>([]);
	let groups = $state<string[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);
	let activeNetwork = $state(ALL_TAB);

	// One tab per distinct Group, in the order areas already arrive in
	// (network, sort_order, name -- see ListAreas) so a long list (a
	// real hub can easily carry hundreds of areas across a handful of
	// networks) doesn't force one huge scroll to find anything.
	let networkTabs = $derived.by(() => {
		const counts = new Map<string, number>();
		for (const a of areas) {
			const key = a.network || UNGROUPED;
			counts.set(key, (counts.get(key) ?? 0) + 1);
		}
		return [...counts.entries()];
	});
	let visibleAreas = $derived(
		activeNetwork === ALL_TAB ? areas : areas.filter((a) => (a.network || UNGROUPED) === activeNetwork)
	);

	let editingId = $state<number | null>(null);
	let draft = $state<FileAreaInput>(emptyDraft());
	let saving = $state(false);

	let creating = $state(false);
	let newDraft = $state<FileAreaInput>(emptyDraft());

	let expandedAreaId = $state<number | null>(null);
	let filesByArea = $state<Record<number, BBSFile[]>>({});
	let filesLoading = $state(false);
	let uploadDescription = $state('');
	let uploading = $state(false);
	let fileInput = $state<HTMLInputElement | null>(null);

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
			areas = await listFileAreas(auth.token);
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load file areas.';
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

	function startEdit(area: FileArea) {
		editingId = area.id;
		draft = {
			tag: area.tag,
			name: area.name,
			description: area.description,
			network: area.network,
			min_sl_download: area.min_sl_download,
			min_sl_upload: area.min_sl_upload,
			sort_order: area.sort_order
		};
	}

	async function saveEdit(id: number) {
		if (!auth.token) return;
		saving = true;
		try {
			const updated = await updateFileArea(auth.token, id, draft);
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
			const created = await createFileArea(auth.token, newDraft);
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

	async function remove(area: FileArea) {
		if (!auth.token) return;
		if (!confirm(`Delete area "${area.name}"? This also deletes all its files from disk.`)) return;
		try {
			await deleteFileArea(auth.token, area.id);
			areas = areas.filter((a) => a.id !== area.id);
			toast.push(`Deleted "${area.name}".`, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not delete area.', 'error');
		}
	}

	async function toggleFiles(area: FileArea) {
		if (expandedAreaId === area.id) {
			expandedAreaId = null;
			return;
		}
		expandedAreaId = area.id;
		if (!filesByArea[area.id]) {
			await loadFiles(area.id);
		}
	}

	async function loadFiles(areaId: number) {
		if (!auth.token) return;
		filesLoading = true;
		try {
			filesByArea = { ...filesByArea, [areaId]: await listAreaFiles(auth.token, areaId) };
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not load files.', 'error');
		} finally {
			filesLoading = false;
		}
	}

	async function upload(areaId: number) {
		if (!auth.token || !fileInput?.files?.length) return;
		uploading = true;
		try {
			const uploaded = await uploadAreaFile(auth.token, areaId, fileInput.files[0], uploadDescription);
			filesByArea = { ...filesByArea, [areaId]: [...(filesByArea[areaId] ?? []), uploaded] };
			uploadDescription = '';
			if (fileInput) fileInput.value = '';
			toast.push(`Uploaded ${uploaded.filename}.`, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Upload failed.', 'error');
		} finally {
			uploading = false;
		}
	}

	async function removeFile(areaId: number, file: BBSFile) {
		if (!auth.token) return;
		if (!confirm(`Delete file "${file.filename}"?`)) return;
		try {
			await deleteFile(auth.token, file.id);
			filesByArea = { ...filesByArea, [areaId]: filesByArea[areaId].filter((f) => f.id !== file.id) };
			toast.push(`Deleted ${file.filename}.`, 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not delete file.', 'error');
		}
	}
</script>

<datalist id="groups-list">
	{#each groups as g (g)}
		<option value={g}></option>
	{/each}
</datalist>

<div class="mb-6 flex items-center justify-between">
	<h1 class="page-title">File Areas</h1>
	<button
		class="btn-primary btn-sm"
		onclick={() => (creating = !creating)}
	>
		{creating ? 'Cancel' : '+ New Area'}
	</button>
</div>

{#if creating}
	<div class="mb-6 rounded-xl border border-line p-4">
		<h2 class="mb-4 card-label">New Area</h2>
		<div class="grid grid-cols-2 gap-4">
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Tag</span>
				<input
					class="field field-sm font-mono"
					bind:value={newDraft.tag}
					placeholder="general"
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Name</span>
				<input
					class="field field-sm"
					bind:value={newDraft.name}
				/>
			</label>
			<label class="col-span-2 flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Description</span>
				<input
					class="field field-sm"
					bind:value={newDraft.description}
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Group</span>
				<input
					class="field field-sm"
					bind:value={newDraft.network}
					list="groups-list"
					placeholder="fsxNet, FidoNet… (blank for ungrouped)"
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Min SL to download</span>
				<input
					type="number"
					min="0"
					max="255"
					class="field field-sm"
					bind:value={newDraft.min_sl_download}
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Min SL to upload</span>
				<input
					type="number"
					min="0"
					max="255"
					class="field field-sm"
					bind:value={newDraft.min_sl_upload}
				/>
			</label>
		</div>
		<button
			class="mt-4 btn-primary btn-sm"
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
	{#if areas.length > 0}
		<div class="mb-4 flex flex-wrap gap-1 border-b border-slate-800">
			<button
				class="border-b-2 px-3 py-2 text-sm font-medium transition {activeNetwork === ALL_TAB
					? 'border-cyan-400 text-slate-100'
					: 'border-transparent text-slate-500 hover:text-slate-300'}"
				onclick={() => (activeNetwork = ALL_TAB)}
			>
				All ({areas.length})
			</button>
			{#each networkTabs as [name, count] (name)}
				<button
					class="border-b-2 px-3 py-2 text-sm font-medium transition {activeNetwork === name
						? 'border-cyan-400 text-slate-100'
						: 'border-transparent text-slate-500 hover:text-slate-300'}"
					onclick={() => (activeNetwork = name)}
				>
					{name} ({count})
				</button>
			{/each}
		</div>
	{/if}
	<div class="flex flex-col gap-4">
		{#each visibleAreas as area (area.id)}
			<div class="rounded-xl border border-line p-4">
				{#if editingId === area.id}
					<div class="grid grid-cols-2 gap-4">
						<label class="flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Name</span>
							<input
								class="field field-sm"
								bind:value={draft.name}
							/>
						</label>
						<label class="flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Sort order</span>
							<input
								type="number"
								class="field field-sm"
								bind:value={draft.sort_order}
							/>
						</label>
						<label class="col-span-2 flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Description</span>
							<input
								class="field field-sm"
								bind:value={draft.description}
							/>
						</label>
						<label class="flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Group</span>
							<input
								class="field field-sm"
								bind:value={draft.network}
								list="groups-list"
								placeholder="fsxNet, FidoNet… (blank for ungrouped)"
							/>
						</label>
						<label class="flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Min SL to download</span>
							<input
								type="number"
								min="0"
								max="255"
								class="field field-sm"
								bind:value={draft.min_sl_download}
							/>
						</label>
						<label class="flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Min SL to upload</span>
							<input
								type="number"
								min="0"
								max="255"
								class="field field-sm"
								bind:value={draft.min_sl_upload}
							/>
						</label>
					</div>
					<div class="mt-4 flex gap-2">
						<button
							class="btn-primary btn-sm"
							disabled={saving}
							onclick={() => saveEdit(area.id)}
						>
							{saving ? 'Saving…' : 'Save'}
						</button>
						<button
							class="btn-secondary btn-sm"
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
										class="ml-2 rounded-md border border-line-strong px-1.5 py-0.5 font-mono text-[10.5px] text-muted"
										>{area.network}</span
									>
								{/if}
							</div>
							<div class="text-sm text-slate-400">{area.description}</div>
							<div class="mt-1 text-xs text-slate-500">
								Download: SL {area.min_sl_download} &middot; Upload: SL {area.min_sl_upload}
							</div>
						</div>
						<div class="flex gap-2">
							<button
								class="btn-secondary btn-sm"
								onclick={() => toggleFiles(area)}
							>
								{expandedAreaId === area.id ? 'Hide files' : 'Manage files'}
							</button>
							<button
								class="btn-secondary btn-sm"
								onclick={() => startEdit(area)}
							>
								Edit
							</button>
							<button
								class="btn-danger btn-sm"
								onclick={() => remove(area)}
							>
								Delete
							</button>
						</div>
					</div>

					{#if expandedAreaId === area.id}
						<div class="mt-4 border-t border-slate-800 pt-4">
							<div class="mb-3 flex flex-wrap items-center gap-2">
								<input
									type="file"
									bind:this={fileInput}
									class="text-xs text-slate-400 file:mr-2 file:rounded file:border-0 file:bg-slate-800 file:px-2 file:py-1 file:text-slate-200"
								/>
								<input
									class="field field-sm"
									placeholder="Description"
									bind:value={uploadDescription}
								/>
								<button
									class="btn-primary btn-sm"
									disabled={uploading}
									onclick={() => upload(area.id)}
								>
									{uploading ? 'Uploading…' : 'Upload'}
								</button>
							</div>

							{#if filesLoading && !filesByArea[area.id]}
								<p class="text-sm text-slate-500">Loading files…</p>
							{:else if (filesByArea[area.id] ?? []).length === 0}
								<p class="text-sm text-slate-500">No files yet.</p>
							{:else}
								<div class="overflow-x-auto">
									<table class="w-full text-left text-sm">
										<thead class="card-label">
											<tr class="border-b border-slate-800">
												<th class="py-1 pr-3">Filename</th>
												<th class="py-1 pr-3">Size</th>
												<th class="py-1 pr-3">Description</th>
												<th class="py-1 pr-3">Uploaded By</th>
												<th class="py-1 pr-3">Downloads</th>
												<th class="py-1"></th>
											</tr>
										</thead>
										<tbody>
											{#each filesByArea[area.id] as f (f.id)}
												<tr class="border-b border-line">
													<td class="py-1 pr-3 text-slate-100">{f.filename}</td>
													<td class="py-1 pr-3 text-slate-400">{f.size_human}</td>
													<td class="max-w-xs truncate py-1 pr-3 text-slate-400" title={f.description}
													>{f.description}</td
												>
													<td class="py-1 pr-3 text-slate-400">{f.uploaded_by}</td>
													<td class="py-1 pr-3 text-slate-400">{f.download_count}</td>
													<td class="py-1">
														<button
															class="btn-danger btn-xs"
															onclick={() => removeFile(area.id, f)}
														>
															Delete
														</button>
													</td>
												</tr>
											{/each}
										</tbody>
									</table>
								</div>
							{/if}
						</div>
					{/if}
				{/if}
			</div>
		{:else}
			<p class="text-sm text-slate-500">No areas in this group.</p>
		{/each}
	</div>
{/if}