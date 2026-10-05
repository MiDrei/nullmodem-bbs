<script lang="ts">
	import { t } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import Modal from '$lib/Modal.svelte';
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
			sort_order: 0,
			keep_days: 0,
			public: false
		};
	}

	// Areas without a network; never a network's own name.
	const UNGROUPED = '\u0000';
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
	// Narrows the active tab to areas whose tag, name or description
	// contains it, so one area among hundreds is a few keystrokes away.
	let search = $state('');
	let visibleAreas = $derived.by(() => {
		const q = search.trim().toLowerCase();
		return areas.filter(
			(a) =>
				(activeNetwork === ALL_TAB || (a.network || UNGROUPED) === activeNetwork) &&
				(!q ||
					a.tag.toLowerCase().includes(q) ||
					a.name.toLowerCase().includes(q) ||
					a.description.toLowerCase().includes(q))
		);
	});

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
			loadError = err instanceof ApiError ? err.message : t('web.common.could_not_load_file_areas');
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
			sort_order: area.sort_order,
			keep_days: area.keep_days ?? 0,
			public: area.public ?? false
		};
	}

	async function saveEdit(id: number) {
		if (!auth.token) return;
		saving = true;
		try {
			const updated = await updateFileArea(auth.token, id, draft);
			areas = areas.map((a) => (a.id === id ? updated : a));
			editingId = null;
			toast.push(t('admin.common.saved_name', { NAME: updated.name }), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.common.could_not_save'), 'error');
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
			toast.push(t('admin.common.created_name', { NAME: created.name }), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.common.could_not_create_area'), 'error');
		} finally {
			saving = false;
		}
	}

	async function remove(area: FileArea) {
		if (!auth.token) return;
		if (!confirm(t('admin.file_areas.delete_area_name_this_also', { NAME: area.name }))) return;
		try {
			await deleteFileArea(auth.token, area.id);
			areas = areas.filter((a) => a.id !== area.id);
			toast.push(t('admin.common.deleted_name', { NAME: area.name }), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.common.could_not_delete_area'), 'error');
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
			toast.push(err instanceof ApiError ? err.message : t('web.common.could_not_load_files'), 'error');
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
			toast.push(t('admin.file_areas.uploaded_filename', { FILENAME: uploaded.filename }), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.file_areas.upload_failed'), 'error');
		} finally {
			uploading = false;
		}
	}

	async function removeFile(areaId: number, file: BBSFile) {
		if (!auth.token) return;
		if (!confirm(t('admin.file_areas.delete_file_filename', { FILENAME: file.filename }))) return;
		try {
			await deleteFile(auth.token, file.id);
			filesByArea = { ...filesByArea, [areaId]: filesByArea[areaId].filter((f) => f.id !== file.id) };
			toast.push(t('admin.file_areas.deleted_filename', { FILENAME: file.filename }), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.file_areas.could_not_delete_file'), 'error');
		}
	}

	function startCreate() {
		editingId = null;
		newDraft = emptyDraft();
		creating = true;
	}

	function closeDialog() {
		editingId = null;
		creating = false;
	}

	let filesArea = $derived(areas.find((a) => a.id === expandedAreaId) ?? null);
</script>

<datalist id="groups-list">
	{#each groups as g (g)}
		<option value={g}></option>
	{/each}
</datalist>

<div class="mb-5 flex flex-wrap items-end justify-between gap-4">
	<div>
		<h1 class="page-title">{t('common.file_areas')}</h1>
		<p class="page-subtitle">{t('admin.common.length_areas', { LENGTH: areas.length })}</p>
	</div>
	<div class="flex items-center gap-2.5">
		<input class="field field-sm w-56" type="search" placeholder={t('admin.common.search_tag_name')} bind:value={search} />
		<button class="btn-primary btn-sm shrink-0" onclick={startCreate}>{t('admin.common.new_area')}</button>
	</div>
</div>

{#snippet fields(d: FileAreaInput, isNew: boolean)}
	<div class="grid grid-cols-2 gap-3.5">
		<label class="flex flex-col gap-1.5">
			<span class="text-xs text-muted">{t('admin.common.tag')}</span>
			{#if isNew}
				<input class="field field-sm font-mono" bind:value={d.tag} placeholder="general" />
			{:else}
				<input class="field field-sm font-mono" value={d.tag} disabled title={t('admin.common.the_tag_can_t_be')} />
			{/if}
		</label>
		<label class="flex flex-col gap-1.5">
			<span class="text-xs text-muted">{t('common.name')}</span>
			<input class="field field-sm" bind:value={d.name} />
		</label>
		<label class="col-span-2 flex flex-col gap-1.5">
			<span class="text-xs text-muted">{t('web.common.description')}</span>
			<input class="field field-sm" bind:value={d.description} />
		</label>
		<label class="col-span-2 flex flex-col gap-1.5">
			<span class="text-xs text-muted">{t('admin.common.group')}</span>
			<input
				class="field field-sm"
				bind:value={d.network}
				list="groups-list"
				placeholder={t('admin.common.fsxnet_fidonet_blank_for_ungrouped')}
			/>
		</label>
		<div class="col-span-2 grid grid-cols-3 gap-3.5">
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.common.min_sl_to_download')}</span>
				<input type="number" min="0" max="255" class="field field-sm" bind:value={d.min_sl_download} />
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.common.min_sl_to_upload')}</span>
				<input type="number" min="0" max="255" class="field field-sm" bind:value={d.min_sl_upload} />
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.common.sort_order')}</span>
				<input type="number" class="field field-sm" bind:value={d.sort_order} />
			</label>
		</div>
		<label class="col-span-2 flex items-start gap-2 text-sm">
			<input type="checkbox" class="check mt-0.5" bind:checked={d.public} />
			<span>
				<span class="text-ink">{t('admin.file_areas.public')}</span>
				<span class="block text-xs text-faint">{t('admin.file_areas.anyone_may_download_its_files')}</span>
			</span>
		</label>
		<label class="col-span-2 flex flex-col gap-1.5">
			<span class="text-xs text-muted">{t('admin.common.keep_days')}</span>
			<input type="number" min="-1" class="field field-sm max-w-40" bind:value={d.keep_days} />
			<span class="text-xs text-faint"
				>{t('admin.file_areas.cleanup_limit_maintenance_0_the')}</span
			>
		</label>
	</div>
{/snippet}

{#if creating}
	<Modal title={t('admin.file_areas.new_file_area')} onclose={closeDialog}>
		<form
			onsubmit={(e) => {
				e.preventDefault();
				createNew();
			}}
		>
			{@render fields(newDraft, true)}
			<div class="mt-5 flex justify-end gap-2.5">
				<button type="button" class="btn-secondary btn-sm" onclick={closeDialog}>{t('web.common.cancel')}</button>
				<button type="submit" class="btn-primary btn-sm" disabled={saving}>
					{saving ? t('admin.common.creating') : t('admin.common.create_area')}
				</button>
			</div>
		</form>
	</Modal>
{:else if editingId !== null}
	{@const id = editingId}
	<Modal title="Edit {draft.name || draft.tag}" onclose={closeDialog}>
		<form
			onsubmit={(e) => {
				e.preventDefault();
				saveEdit(id);
			}}
		>
			{@render fields(draft, false)}
			<div class="mt-5 flex justify-end gap-2.5">
				<button type="button" class="btn-secondary btn-sm" onclick={closeDialog}>{t('web.common.cancel')}</button>
				<button type="submit" class="btn-primary btn-sm" disabled={saving}>
					{saving ? t('web.common.saving') : t('web.common.save')}
				</button>
			</div>
		</form>
	</Modal>
{:else if filesArea}
	{@const area = filesArea}
	<Modal title={t('admin.file_areas.files_in', { NAME: area.name })} wide onclose={() => (expandedAreaId = null)}>
		<div class="mb-4 flex flex-wrap items-center gap-2.5">
			<input
				type="file"
				bind:this={fileInput}
				class="text-xs text-muted file:mr-2.5 file:rounded-lg file:border file:border-line-strong file:bg-transparent file:px-3 file:py-1.5 file:text-xs file:text-ink"
			/>
			<input class="field field-sm w-auto flex-1" placeholder={t('web.common.description')} bind:value={uploadDescription} />
			<button class="btn-primary btn-sm" disabled={uploading} onclick={() => upload(area.id)}>
				{uploading ? t('web.common.uploading') : t('web.common.upload')}
			</button>
		</div>
		{#if filesLoading && !filesByArea[area.id]}
			<p class="text-sm text-muted">{t('admin.file_areas.loading_files')}</p>
		{:else if (filesByArea[area.id] ?? []).length === 0}
			<p class="text-sm text-muted">{t('admin.file_areas.no_files_yet')}</p>
		{:else}
			<div class="max-h-[60vh] overflow-auto">
				<table class="w-full text-left text-[13px]">
					<thead class="card-label">
						<tr class="border-b border-line">
							<th class="py-2 pr-3 font-normal">{t('common.filename')}</th>
							<th class="py-2 pr-3 text-right font-normal">{t('common.size')}</th>
							<th class="py-2 pr-3 font-normal">{t('web.common.description')}</th>
							<th class="py-2 pr-3 font-normal">{t('admin.file_areas.uploaded_by')}</th>
							<th class="py-2 pr-3 text-right font-normal">{t('admin.file_areas.dls')}</th>
							<th class="py-2"></th>
						</tr>
					</thead>
					<tbody>
						{#each filesByArea[area.id] as f (f.id)}
							<tr class="border-b border-line">
								<td class="py-1.5 pr-3 font-mono text-xs text-ink">{f.filename}</td>
								<td class="py-1.5 pr-3 text-right font-mono text-xs whitespace-nowrap text-muted">{f.size_human}</td>
								<td class="max-w-xs truncate py-1.5 pr-3 text-muted" title={f.description}>{f.description}</td>
								<td class="py-1.5 pr-3 text-muted">{f.uploaded_by}</td>
								<td class="py-1.5 pr-3 text-right font-mono text-xs text-muted">{f.download_count}</td>
								<td class="py-1.5 text-right">
									<button class="btn-danger btn-xs" onclick={() => removeFile(area.id, f)}>{t('web.common.delete')}</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</Modal>
{/if}

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-muted">{t('web.common.loading')}</p>
{:else}
	{#if networkTabs.length > 1}
		<div class="mb-3 flex flex-wrap gap-x-6 gap-y-1 border-b border-line" role="tablist">
			<button
				role="tab"
				aria-selected={activeNetwork === ALL_TAB}
				class="tab {activeNetwork === ALL_TAB ? 'tab-active' : ''}"
				onclick={() => (activeNetwork = ALL_TAB)}
			>
				{t('web.common.all')} <span class="font-mono text-[11px] text-faint">{areas.length}</span>
			</button>
			{#each networkTabs as [name, count] (name)}
				<button
					role="tab"
					aria-selected={activeNetwork === name}
					class="tab {activeNetwork === name ? 'tab-active' : ''}"
					onclick={() => (activeNetwork = name)}
				>
					{name === UNGROUPED ? t('admin.common.no_network') : name} <span class="font-mono text-[11px] text-faint">{count}</span>
				</button>
			{/each}
		</div>
	{/if}

	{#if visibleAreas.length === 0}
		<p class="py-4 text-sm text-muted">{search ? t('admin.common.no_area_matches') : t('admin.common.no_areas_in_this_group')}</p>
	{:else}
		<div class="overflow-x-auto">
			<table class="w-full min-w-[44rem] table-fixed text-left text-[13px]">
				<thead class="card-label">
					<tr class="border-b border-line">
						<th class="w-52 py-2 pr-3 pl-2 font-normal">{t('admin.common.tag')}</th>
						<th class="py-2 pr-3 font-normal">{t('common.name')}</th>
						{#if activeNetwork === ALL_TAB}<th class="w-28 py-2 pr-3 font-normal">{t('admin.common.group')}</th>{/if}
						<th class="w-14 py-2 pr-3 text-right font-normal" title={t('admin.file_areas.minimum_security_level_to_download')}>DL</th>
						<th class="w-14 py-2 pr-3 text-right font-normal" title={t('admin.file_areas.minimum_security_level_to_upload')}>UL</th>
						<th class="w-[11.5rem] py-2"></th>
					</tr>
				</thead>
				<tbody>
					{#each visibleAreas as area (area.id)}
						<tr
							class="group cursor-pointer border-b border-line transition-colors hover:bg-slate-800/60"
							onclick={() => startEdit(area)}
						>
							<td class="truncate py-1.5 pr-3 pl-2 font-mono text-xs text-muted">{area.tag}</td>
							<td class="py-1.5 pr-3">
								<div class="truncate">
									<span class="text-ink group-hover:text-accent">{area.name}</span>
									{#if area.description && area.description !== area.name}
										<span class="text-faint"> — {area.description}</span>
									{/if}
								</div>
							</td>
							{#if activeNetwork === ALL_TAB}
								<td class="py-1.5 pr-3 whitespace-nowrap">
									{#if area.network}
										<span class="rounded-md border border-line-strong px-1.5 py-0.5 font-mono text-[10.5px] text-muted"
											>{area.network}</span
										>
									{/if}
								</td>
							{/if}
							<td class="py-1.5 pr-3 text-right font-mono text-xs text-muted">{area.min_sl_download}</td>
							<td class="py-1.5 pr-3 text-right font-mono text-xs text-muted">{area.min_sl_upload}</td>
							<td class="py-1.5 pr-2 text-right whitespace-nowrap">
								<button
									class="btn-secondary btn-xs"
									onclick={(e) => {
										e.stopPropagation();
										toggleFiles(area);
									}}>{t('common.files')}</button
								>
								<button
									class="btn-secondary btn-xs ml-1"
									onclick={(e) => {
										e.stopPropagation();
										startEdit(area);
									}}>{t('web.common.edit')}</button
								>
								<button
									class="btn-danger btn-xs ml-1"
									onclick={(e) => {
										e.stopPropagation();
										remove(area);
									}}>{t('web.common.delete')}</button
								>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
{/if}
