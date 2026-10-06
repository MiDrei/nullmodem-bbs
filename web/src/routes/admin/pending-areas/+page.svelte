<script lang="ts">
	import SLSelect from '$lib/admin/SLSelect.svelte';
	import { t } from '$lib/i18n.svelte';
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
	// Which list is shown. Starts on whichever has something waiting
	// (messages first) once loaded -- see load().
	let tab = $state<'message' | 'file'>('message');

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
			if (!loaded && messageAreas.length === 0 && fileAreas.length > 0) tab = 'file';
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : t('admin.pending_areas.could_not_load_pending_areas');
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
			toast.push(t('admin.pending_areas.approved_name_it_s_now', { NAME: messageDrafts[area.id].name }), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.pending_areas.could_not_approve'), 'error');
		} finally {
			busyID = null;
		}
	}

	async function rejectMessage(area: MessageArea) {
		if (!auth.token) return;
		if (!confirm(t('admin.pending_areas.reject_and_delete_area_tag', { TAG: area.tag })))
			return;
		busyID = `m${area.id}`;
		try {
			await deleteMessageArea(auth.token, area.id);
			messageAreas = messageAreas.filter((a) => a.id !== area.id);
			toast.push(t('admin.pending_areas.rejected_tag', { TAG: area.tag }), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.pending_areas.could_not_reject'), 'error');
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
			toast.push(t('admin.pending_areas.approved_name_it_s_now', { NAME: fileDrafts[area.id].name }), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.pending_areas.could_not_approve'), 'error');
		} finally {
			busyID = null;
		}
	}

	async function rejectFile(area: FileArea) {
		if (!auth.token) return;
		if (!confirm(t('admin.pending_areas.reject_and_delete_area_tag_2', { TAG: area.tag }))) return;
		busyID = `f${area.id}`;
		try {
			await deleteFileArea(auth.token, area.id);
			fileAreas = fileAreas.filter((a) => a.id !== area.id);
			toast.push(t('admin.pending_areas.rejected_tag', { TAG: area.tag }), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.pending_areas.could_not_reject'), 'error');
		} finally {
			busyID = null;
		}
	}
</script>

<h1 class="mb-2 page-title">{t('admin.common.pending_areas')}</h1>
<p class="mb-6 text-sm text-slate-400">
	{t('admin.pending_areas.areas_the_binkp_tosser_auto')}
</p>

<datalist id="groups-list">
	{#each groups as g (g)}
		<option value={g}></option>
	{/each}
</datalist>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">{t('web.common.loading')}</p>
{:else}
	<div class="mb-5 flex gap-6 border-b border-line" role="tablist" aria-label={t('admin.pending_areas.area_type')}>
		<button
			role="tab"
			aria-selected={tab === 'message'}
			class="tab {tab === 'message' ? 'tab-active' : ''}"
			onclick={() => (tab = 'message')}
		>
			{t('common.message_areas')}
			<span class="ml-1.5 rounded-md px-1.5 py-0.5 font-mono text-[11px] {messageAreas.length > 0
					? 'bg-accent text-white'
					: 'text-faint'}">{messageAreas.length}</span
			>
		</button>
		<button
			role="tab"
			aria-selected={tab === 'file'}
			class="tab {tab === 'file' ? 'tab-active' : ''}"
			onclick={() => (tab = 'file')}
		>
			{t('common.file_areas')}
			<span class="ml-1.5 rounded-md px-1.5 py-0.5 font-mono text-[11px] {fileAreas.length > 0
					? 'bg-accent text-white'
					: 'text-faint'}">{fileAreas.length}</span
			>
		</button>
	</div>

	{#if tab === 'message'}
	<section>
		{#if messageAreas.length === 0}
			<p class="text-sm text-slate-500">{t('admin.pending_areas.no_pending_message_areas')}</p>
		{:else}
			<div class="flex flex-col gap-4">
				{#each messageAreas as area (area.id)}
					{@const draft = messageDrafts[area.id]}
					<div class="rounded-xl border border-line p-4">
						<div class="mb-3 font-mono text-xs text-slate-500">{area.tag}</div>
						<div class="grid grid-cols-2 gap-4">
							<label class="flex flex-col gap-1 text-sm">
								<span class="text-slate-400">{t('common.name')}</span>
								<input
									class="field field-sm"
									bind:value={draft.name}
								/>
							</label>
							<label class="flex flex-col gap-1 text-sm">
								<span class="text-slate-400">{t('admin.common.group')}</span>
								<input
									class="field field-sm"
									bind:value={draft.network}
									list="groups-list"
									placeholder={t('admin.common.fsxnet_fidonet_blank_for_ungrouped')}
								/>
							</label>
							<label class="col-span-2 flex flex-col gap-1 text-sm">
								<span class="text-slate-400">{t('web.common.description')}</span>
								<input
									class="field field-sm"
									bind:value={draft.description}
								/>
							</label>
							<label class="flex flex-col gap-1 text-sm">
								<span class="text-slate-400">{t('admin.common.min_sl_to_read')}</span>
								<SLSelect bind:value={draft.min_sl_read} />
							</label>
							<label class="flex flex-col gap-1 text-sm">
								<span class="text-slate-400">{t('admin.common.min_sl_to_post')}</span>
								<SLSelect bind:value={draft.min_sl_write} />
							</label>
						</div>
						<div class="mt-4 flex gap-2">
							<button
								class="btn-primary btn-sm"
								disabled={busyID === `m${area.id}`}
								onclick={() => approveMessage(area)}
							>
								{busyID === `m${area.id}` ? t('admin.pending_areas.approving') : t('admin.pending_areas.save_approve')}
							</button>
							<button
								class="btn-danger btn-sm"
								disabled={busyID === `m${area.id}`}
								onclick={() => rejectMessage(area)}
							>
								{t('admin.pending_areas.reject')}
							</button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</section>

	{:else}
	<section>
		{#if fileAreas.length === 0}
			<p class="text-sm text-slate-500">{t('admin.pending_areas.no_pending_file_areas')}</p>
		{:else}
			<div class="flex flex-col gap-4">
				{#each fileAreas as area (area.id)}
					{@const draft = fileDrafts[area.id]}
					<div class="rounded-xl border border-line p-4">
						<div class="mb-3 font-mono text-xs text-slate-500">{area.tag}</div>
						<div class="grid grid-cols-2 gap-4">
							<label class="flex flex-col gap-1 text-sm">
								<span class="text-slate-400">{t('common.name')}</span>
								<input
									class="field field-sm"
									bind:value={draft.name}
								/>
							</label>
							<label class="flex flex-col gap-1 text-sm">
								<span class="text-slate-400">{t('admin.common.group')}</span>
								<input
									class="field field-sm"
									bind:value={draft.network}
									list="groups-list"
									placeholder={t('admin.common.fsxnet_fidonet_blank_for_ungrouped')}
								/>
							</label>
							<label class="col-span-2 flex flex-col gap-1 text-sm">
								<span class="text-slate-400">{t('web.common.description')}</span>
								<input
									class="field field-sm"
									bind:value={draft.description}
								/>
							</label>
							<label class="flex flex-col gap-1 text-sm">
								<span class="text-slate-400">{t('admin.common.min_sl_to_download')}</span>
								<SLSelect bind:value={draft.min_sl_download} />
							</label>
							<label class="flex flex-col gap-1 text-sm">
								<span class="text-slate-400">{t('admin.common.min_sl_to_upload')}</span>
								<SLSelect bind:value={draft.min_sl_upload} />
							</label>
						</div>
						<div class="mt-4 flex gap-2">
							<button
								class="btn-primary btn-sm"
								disabled={busyID === `f${area.id}`}
								onclick={() => approveFile(area)}
							>
								{busyID === `f${area.id}` ? t('admin.pending_areas.approving') : t('admin.pending_areas.save_approve')}
							</button>
							<button
								class="btn-danger btn-sm"
								disabled={busyID === `f${area.id}`}
								onclick={() => rejectFile(area)}
							>
								{t('admin.pending_areas.reject')}
							</button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</section>
	{/if}
{/if}
