<script lang="ts">
	import { formatDateTime } from '$lib/datetime';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import FilePreviewModal from '$lib/FilePreviewModal.svelte';
	import {
		getBBSFile,
		downloadBBSFile,
		previewBBSFile,
		previewBBSFileImageURL,
		previewBBSFileEntry,
		previewBBSFileEntryImageURL,
		guessFilePreviewKind,
		ApiError,
		type BBSFile,
		type FilePreview
	} from '$lib/api';

	const fileId = Number(page.params.id);

	let file = $state<BBSFile | null>(null);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	// Shown outright, no click needed -- unlike file-areas/[id]'s list
	// (many rows, so a .zip's contents stay behind a Preview click
	// there), this page is about exactly one file already.
	let archiveListing = $state<{ loading: boolean; error: string | null; preview: FilePreview | null } | null>(
		null
	);

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

	onMount(async () => {
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		try {
			// Loading this page marks the file read, same as opening a
			// message does -- the only other way is downloading it.
			file = await getBBSFile(bbsAuth.token, fileId);
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load file.';
			loaded = true;
			return;
		}
		loaded = true;

		if (guessFilePreviewKind(file.filename) === 'archive') {
			archiveListing = { loading: true, error: null, preview: null };
			try {
				const p = await previewBBSFile(bbsAuth.token, fileId);
				archiveListing = { loading: false, error: null, preview: p };
			} catch (err) {
				if (await handleAuthError(err)) return;
				const message = err instanceof ApiError ? err.message : 'Could not load contents.';
				archiveListing = { loading: false, error: message, preview: null };
			}
		}
	});

	async function download() {
		if (!bbsAuth.token || !file) return;
		try {
			await downloadBBSFile(bbsAuth.token, file.id, file.filename);
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not download file.', 'error');
		}
	}

	async function openPreview() {
		if (!file || !bbsAuth.token) return;
		modalOpen = true;
		modalTitle = file.filename;
		modalLoading = true;
		modalError = null;
		modalPreview = null;
		if (modalImageURL) URL.revokeObjectURL(modalImageURL);
		modalImageURL = null;
		try {
			const p = await previewBBSFile(bbsAuth.token, file.id);
			modalPreview = p;
			if (p.kind === 'image') {
				modalImageURL = await previewBBSFileImageURL(bbsAuth.token, file.id);
			}
		} catch (err) {
			if (await handleAuthError(err)) return;
			modalError = err instanceof ApiError ? err.message : 'Could not load preview.';
		} finally {
			modalLoading = false;
		}
	}

	async function openEntryPreview(entryName: string) {
		if (!file || !bbsAuth.token) return;
		modalOpen = true;
		modalTitle = `${file.filename} → ${entryName}`;
		modalLoading = true;
		modalError = null;
		modalPreview = null;
		if (modalImageURL) URL.revokeObjectURL(modalImageURL);
		modalImageURL = null;
		try {
			const p = await previewBBSFileEntry(bbsAuth.token, file.id, entryName);
			modalPreview = p;
			if (p.kind === 'image') {
				modalImageURL = await previewBBSFileEntryImageURL(bbsAuth.token, file.id, entryName);
			}
		} catch (err) {
			if (await handleAuthError(err)) return;
			modalError = err instanceof ApiError ? err.message : 'Could not load preview.';
		} finally {
			modalLoading = false;
		}
	}
</script>

<a href={file ? `/file-areas/${file.area_id}` : '/file-areas'} class="back-link">&larr; Files</a>

{#if loadError}
	<p class="mt-4 text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="mt-4 text-sm text-muted">Loading…</p>
{:else if file}
	{@const kind = guessFilePreviewKind(file.filename)}
	<div class="mt-1.5 mb-5">
		<h1 class="font-mono text-xl font-semibold break-all text-ink-strong">{file.filename}</h1>
		<div class="mt-1 text-[12.5px] text-muted">
			<span class="font-mono">{file.size_human}</span> &middot; uploaded by
			<span class="text-slate-400">{file.uploaded_by}</span>
			&middot; {formatDateTime(file.uploaded_at)} &middot;
			{file.download_count}
			{file.download_count === 1 ? 'download' : 'downloads'}
		</div>
	</div>

	<div class="body-panel mb-5 font-sans whitespace-pre-wrap">
		{#if file.description}{file.description}{:else}<span class="text-faint">No description.</span>{/if}
	</div>

	<div class="mb-7 flex gap-2.5">
		<button class="btn-primary px-5" onclick={download}>Download</button>
		{#if kind === 'image' || kind === 'text'}
			<button class="btn-secondary" onclick={openPreview}>Preview</button>
		{/if}
	</div>

	{#if kind === 'archive'}
		<div class="card">
			<h2 class="card-label mb-3">Contents</h2>
			{#if archiveListing?.loading}
				<p class="text-sm text-faint">Reading archive contents…</p>
			{:else if archiveListing?.error}
				<p class="text-sm text-red-400">{archiveListing.error}</p>
			{:else if archiveListing?.preview?.kind === 'archive'}
				<ul class="flex flex-col gap-1.5">
					{#each archiveListing.preview.entries ?? [] as entry}
						<li class="flex items-center justify-between gap-4 text-[13px]">
							<span class="truncate font-mono text-ink-soft">{entry.name}</span>
							<div class="flex shrink-0 items-center gap-3">
								<span class="list-meta">{entry.size_bytes.toLocaleString()} B</span>
								{#if guessFilePreviewKind(entry.name) === 'image' || guessFilePreviewKind(entry.name) === 'text'}
									<button
										class="text-xs text-faint transition-colors hover:text-accent"
										onclick={() => openEntryPreview(entry.name)}
									>
										Preview
									</button>
								{/if}
							</div>
						</li>
					{/each}
				</ul>
				{#if archiveListing.preview.entries_truncated}
					<p class="mt-2 text-xs text-faint">List truncated.</p>
				{/if}
			{/if}
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
