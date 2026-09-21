<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listBBSFileAreas,
		listBBSAreaFiles,
		uploadBBSAreaFile,
		downloadBBSFile,
		ApiError,
		type BBSFileArea,
		type BBSFile
	} from '$lib/api';

	const areaId = Number(page.params.id);

	let area = $state<BBSFileArea | null>(null);
	let files = $state<BBSFile[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let uploadDescription = $state('');
	let uploading = $state(false);
	let fileInput = $state<HTMLInputElement | null>(null);

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
</script>

<a href="/file-areas" class="text-sm text-cyan-400 hover:text-cyan-300">&larr; Files</a>

{#if loadError}
	<p class="mt-4 text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="mt-4 text-sm text-slate-400">Loading…</p>
{:else}
	<h1 class="mt-2 mb-6 text-2xl font-bold tracking-tight text-slate-100">{area?.name ?? 'Area'}</h1>

	<div class="mb-6 flex flex-wrap items-center gap-2 rounded-2xl border border-slate-800/60 bg-slate-900/40 p-4">
		<input
			type="file"
			bind:this={fileInput}
			class="text-xs text-slate-400 file:mr-2 file:rounded-full file:border-0 file:bg-slate-800 file:px-3 file:py-1 file:text-slate-200"
		/>
		<input
			class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 text-sm text-slate-100 focus:border-cyan-400 focus:outline-none"
			placeholder="Description"
			bind:value={uploadDescription}
		/>
		<button
			class="rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-4 py-1.5 text-sm font-semibold text-white disabled:opacity-40"
			disabled={uploading}
			onclick={upload}
		>
			{uploading ? 'Uploading…' : 'Upload'}
		</button>
	</div>

	{#if files.length === 0}
		<p class="text-sm text-slate-400">No files in this area yet.</p>
	{:else}
		<div class="overflow-hidden rounded-2xl border border-slate-800/60 bg-slate-900/40">
			<table class="w-full text-left text-sm">
				<thead class="text-xs tracking-widest text-slate-500 uppercase">
					<tr class="border-b border-slate-800/60">
						<th class="py-2.5 pl-4">Filename</th>
						<th class="py-2.5">Size</th>
						<th class="py-2.5">Description</th>
						<th class="py-2.5">Uploaded By</th>
						<th class="py-2.5 pr-4"></th>
					</tr>
				</thead>
				<tbody>
					{#each files as f, i (f.id)}
						<tr
							class="{i > 0 ? 'border-t border-slate-800/60' : ''} {f.unread
								? 'border-l-2 border-l-fuchsia-400'
								: 'border-l-2 border-l-transparent'} transition hover:bg-slate-800/60"
						>
							<td class="py-2 pl-4 {f.unread ? 'font-medium text-slate-100' : 'text-slate-300'}">
								{f.filename}
							</td>
							<td class="py-2 text-slate-500">{f.size_human}</td>
							<td class="py-2 text-slate-500">{f.description}</td>
							<td class="py-2 text-slate-500">{f.uploaded_by}</td>
							<td class="py-2 pr-4">
								<button
									class="rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-3 py-1 text-xs font-semibold text-white"
									onclick={() => download(f)}
								>
									Download
								</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
{/if}
