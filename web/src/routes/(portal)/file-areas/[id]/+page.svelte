<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import FilePreviewModal from '$lib/FilePreviewModal.svelte';
	import {
		listBBSFileAreas,
		listBBSAreaFiles,
		uploadBBSAreaFile,
		downloadBBSFile,
		previewBBSFile,
		previewBBSFileImageURL,
		previewBBSFileEntry,
		previewBBSFileEntryImageURL,
		guessFilePreviewKind,
		ApiError,
		type BBSFileArea,
		type BBSFile,
		type FilePreview
	} from '$lib/api';

	const areaId = Number(page.params.id);

	let area = $state<BBSFileArea | null>(null);
	let files = $state<BBSFile[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let uploadDescription = $state('');
	let uploading = $state(false);
	let fileInput = $state<HTMLInputElement | null>(null);

	// A .zip's contents list is fetched lazily, on Preview -- an area
	// can hold many .zip files, and always showing every one's contents
	// inline would use up too much vertical space in the list.
	let expandedArchiveId = $state<number | null>(null);
	let archiveListings = $state<Record<number, { loading: boolean; error: string | null; preview: FilePreview | null }>>(
		{}
	);

	// A single shared modal for an actual preview's content (image/text)
	// -- opened either from a top-level file or from one entry inside a
	// .zip's contents list.
	let modalOpen = $state(false);
	let modalTitle = $state('');
	let modalLoading = $state(false);
	let modalError = $state<string | null>(null);
	let modalPreview = $state<FilePreview | null>(null);
	let modalImageURL = $state<string | null>(null);

	function closeModal() {
		modalOpen = false;
		if (modalImageURL) URL.revokeObjectURL(modalImageURL);
		modalImageURL = null;
		modalPreview = null;
	}

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			bbsAuth.clear();
			await goto('/login');
			return true;
		}
		return false;
	}

	async function load() {
		if (!bbsAuth.token) return;
		try {
			const [areas, areaFiles] = await Promise.all([
				listBBSFileAreas(bbsAuth.token),
				listBBSAreaFiles(bbsAuth.token, areaId)
			]);
			area = areas.find((a) => a.id === areaId) ?? null;
			files = areaFiles;
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load files.';
		} finally {
			loaded = true;
		}
	}

	async function toggleArchivePreview(f: BBSFile) {
		if (expandedArchiveId === f.id) {
			expandedArchiveId = null;
			return;
		}
		expandedArchiveId = f.id;
		if (archiveListings[f.id] || !bbsAuth.token) return;
		archiveListings = { ...archiveListings, [f.id]: { loading: true, error: null, preview: null } };
		try {
			const p = await previewBBSFile(bbsAuth.token, f.id);
			archiveListings = { ...archiveListings, [f.id]: { loading: false, error: null, preview: p } };
		} catch (err) {
			if (await handleAuthError(err)) return;
			const message = err instanceof ApiError ? err.message : 'Could not load contents.';
			archiveListings = { ...archiveListings, [f.id]: { loading: false, error: message, preview: null } };
		}
	}

	onMount(async () => {
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		await load();
	});

	async function upload() {
		if (!bbsAuth.token || !fileInput?.files?.length) return;
		uploading = true;
		try {
			await uploadBBSAreaFile(bbsAuth.token, areaId, fileInput.files[0], uploadDescription);
			uploadDescription = '';
			if (fileInput) fileInput.value = '';
			await load();
			toast.push('File uploaded.', 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not upload file.', 'error');
		} finally {
			uploading = false;
		}
	}

	async function download(f: BBSFile) {
		if (!bbsAuth.token) return;
		try {
			await downloadBBSFile(bbsAuth.token, f.id, f.filename);
			await load();
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not download file.', 'error');
		}
	}

	async function openPreview(f: BBSFile) {
		modalOpen = true;
		modalTitle = f.filename;
		modalLoading = true;
		modalError = null;
		modalPreview = null;
		if (modalImageURL) URL.revokeObjectURL(modalImageURL);
		modalImageURL = null;
		if (!bbsAuth.token) return;
		try {
			const p = await previewBBSFile(bbsAuth.token, f.id);
			modalPreview = p;
			if (p.kind === 'image') {
				modalImageURL = await previewBBSFileImageURL(bbsAuth.token, f.id);
			}
		} catch (err) {
			if (await handleAuthError(err)) return;
			modalError = err instanceof ApiError ? err.message : 'Could not load preview.';
		} finally {
			modalLoading = false;
		}
	}

	async function openEntryPreview(f: BBSFile, entryName: string) {
		modalOpen = true;
		modalTitle = `${f.filename} → ${entryName}`;
		modalLoading = true;
		modalError = null;
		modalPreview = null;
		if (modalImageURL) URL.revokeObjectURL(modalImageURL);
		modalImageURL = null;
		if (!bbsAuth.token) return;
		try {
			const p = await previewBBSFileEntry(bbsAuth.token, f.id, entryName);
			modalPreview = p;
			if (p.kind === 'image') {
				modalImageURL = await previewBBSFileEntryImageURL(bbsAuth.token, f.id, entryName);
			}
		} catch (err) {
			if (await handleAuthError(err)) return;
			modalError = err instanceof ApiError ? err.message : 'Could not load preview.';
		} finally {
			modalLoading = false;
		}
	}
</script>

<a href="/file-areas" class="back-link">&larr; Files</a>

{#if loadError}
	<p class="mt-4 text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="mt-4 text-sm text-muted">Loading…</p>
{:else}
	<h1 class="page-title mt-1.5 mb-5">{area?.name ?? 'Area'}</h1>

	<div class="card mb-6 flex flex-wrap items-center gap-2.5 p-4">
		<input
			type="file"
			bind:this={fileInput}
			class="text-xs text-muted file:mr-2.5 file:rounded-lg file:border file:border-line-strong file:bg-transparent file:px-3 file:py-1.5 file:text-xs file:text-ink hover:file:border-accent hover:file:text-accent"
		/>
		<input class="field w-auto flex-1 py-2" placeholder="Description" bind:value={uploadDescription} />
		<button class="btn-primary py-2" disabled={uploading} onclick={upload}>
			{uploading ? 'Uploading…' : 'Upload'}
		</button>
	</div>

	{#if files.length === 0}
		<p class="text-sm text-muted">No files in this area yet.</p>
	{:else}
		<div class="flex flex-col">
			{#each files as f, i (f.id)}
				{@const kind = guessFilePreviewKind(f.filename)}
				{@const archiveEntry = archiveListings[f.id]}
				<div class="list-row {f.unread ? 'list-row-unread' : ''}">
					<span class="list-num">{String(i + 1).padStart(2, '0')}</span>
					<div class="min-w-0 flex-1">
						<a
							href="/files/{f.id}"
							class="block truncate font-mono text-[13px] hover:text-accent {f.unread
								? 'font-semibold text-white'
								: 'text-slate-100'}"
						>
							{f.filename}
						</a>
						<div class="mt-0.5 truncate text-xs text-faint">
							{f.description || '—'}<span class="text-dim">&nbsp;· {f.uploaded_by}</span>
						</div>
					</div>
					{#if f.unread}
						<span class="badge-new">NEW</span>
					{/if}
					<span class="list-meta w-16 shrink-0 text-right">{f.size_human}</span>
					<div class="flex shrink-0 justify-end gap-1.5">
						{#if kind === 'image' || kind === 'text'}
							<button class="btn-secondary px-3 py-1.5 text-xs" onclick={() => openPreview(f)}>
								Preview
							</button>
						{:else if kind === 'archive'}
							<button class="btn-secondary px-3 py-1.5 text-xs" onclick={() => toggleArchivePreview(f)}>
								{expandedArchiveId === f.id ? 'Hide' : 'Preview'}
							</button>
						{/if}
						<button class="btn-primary px-3 py-1.5 text-xs" onclick={() => download(f)}>
							Download
						</button>
					</div>
				</div>
				{#if kind === 'archive' && expandedArchiveId === f.id}
					<div class="border-b border-line bg-sunken py-2.5 pr-3 pl-12">
						{#if archiveEntry?.loading}
							<p class="text-xs text-faint">Reading archive contents…</p>
						{:else if archiveEntry?.error}
							<p class="text-xs text-red-400">{archiveEntry.error}</p>
						{:else if archiveEntry?.preview?.kind === 'archive'}
							<ul class="flex flex-col gap-1">
								{#each archiveEntry.preview.entries ?? [] as entry}
									<li class="flex items-center justify-between gap-4 text-xs">
										<span class="truncate font-mono text-muted">{entry.name}</span>
										<div class="flex shrink-0 items-center gap-3">
											<span class="list-meta">{entry.size_bytes.toLocaleString()} B</span>
											{#if guessFilePreviewKind(entry.name) === 'image' || guessFilePreviewKind(entry.name) === 'text'}
												<button
													class="text-faint transition-colors hover:text-accent"
													onclick={() => openEntryPreview(f, entry.name)}
												>
													Preview
												</button>
											{/if}
										</div>
									</li>
								{/each}
							</ul>
							{#if archiveEntry.preview.entries_truncated}
								<p class="mt-1 text-xs text-faint">List truncated.</p>
							{/if}
						{/if}
					</div>
				{/if}
			{/each}
		</div>
	{/if}
{/if}

{#if modalOpen}
	<FilePreviewModal
		title={modalTitle}
		loading={modalLoading}
		error={modalError}
		preview={modalPreview}
		imageURL={modalImageURL}
		onclose={closeModal}
	/>
{/if}
