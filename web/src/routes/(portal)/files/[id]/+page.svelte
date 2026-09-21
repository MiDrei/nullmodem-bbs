<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { getBBSFile, downloadBBSFile, ApiError, type BBSFile } from '$lib/api';

	const fileId = Number(page.params.id);

	let file = $state<BBSFile | null>(null);
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
			// Loading this page marks the file read, same as opening a
			// message does -- the only other way is downloading it.
			file = await getBBSFile(bbsAuth.token, fileId);
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load file.';
		} finally {
			loaded = true;
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
</script>

<a
	href={file ? `/file-areas/${file.area_id}` : '/file-areas'}
	class="text-sm text-cyan-400 hover:text-cyan-300"
>
	&larr; Files
</a>

{#if loadError}
	<p class="mt-4 text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="mt-4 text-sm text-slate-400">Loading…</p>
{:else if file}
	<div class="mt-3 mb-5">
		<h1 class="text-xl font-bold tracking-tight break-all text-slate-100">{file.filename}</h1>
		<div class="mt-0.5 text-sm text-slate-500">
			{file.size_human} &middot; uploaded by
			<strong class="text-slate-300">{file.uploaded_by}</strong>
			&middot; {new Date(file.uploaded_at).toLocaleString()} &middot;
			{file.download_count}
			{file.download_count === 1 ? 'download' : 'downloads'}
		</div>
	</div>

	<div class="mb-6 max-w-2xl rounded-2xl border border-slate-800/60 bg-slate-900/40 p-6">
		{#if file.description}
			<div class="text-sm leading-relaxed whitespace-pre-wrap text-slate-200">
				{file.description}
			</div>
		{:else}
			<p class="text-sm text-slate-500">No description.</p>
		{/if}
	</div>

	<button
		class="rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-4 py-1.5 text-sm font-semibold text-white shadow-lg shadow-fuchsia-500/20 transition hover:shadow-fuchsia-500/40"
		onclick={download}
	>
		Download
	</button>
{/if}
