<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listQWKAreas,
		setQWKAreas,
		downloadQWKPacket,
		uploadQWKReply,
		ApiError,
		type QWKArea
	} from '$lib/api';

	let areas = $state<QWKArea[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);
	let saving = $state(false);
	let downloading = $state(false);
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
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		try {
			areas = await listQWKAreas(bbsAuth.token);
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load message areas.';
		} finally {
			loaded = true;
		}
	}

	onMount(load);

	function toggle(area: QWKArea) {
		area.selected = !area.selected;
	}

	async function save() {
		if (!bbsAuth.token) return;
		saving = true;
		try {
			await setQWKAreas(
				bbsAuth.token,
				areas.filter((a) => a.selected).map((a) => a.id)
			);
			toast.push('QWK area selection saved.', 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not save selection.', 'error');
		} finally {
			saving = false;
		}
	}

	async function download() {
		if (!bbsAuth.token) return;
		downloading = true;
		try {
			const got = await downloadQWKPacket(bbsAuth.token);
			toast.push(got ? 'QWK packet downloaded.' : 'No new mail to download.', 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not download packet.', 'error');
		} finally {
			downloading = false;
		}
	}

	async function handleUpload(e: Event) {
		if (!bbsAuth.token) return;
		const input = e.target as HTMLInputElement;
		const file = input.files?.[0];
		if (!file) return;
		uploading = true;
		try {
			const result = await uploadQWKReply(bbsAuth.token, file);
			toast.push(
				`Replies processed: ${result.posted} posted, ${result.sent} netmail sent` +
					(result.skipped > 0 ? `, ${result.skipped} skipped` : '') +
					'.',
				'success'
			);
			for (const r of result.rejected ?? []) {
				toast.push(`Not delivered: "${r.subject}" to ${r.to} -- ${r.reason}`, 'error');
			}
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not process reply packet.', 'error');
		} finally {
			uploading = false;
			input.value = '';
		}
	}
</script>

<div class="mb-5">
	<h1 class="page-title">QWK Offline Mail</h1>
	<p class="page-subtitle max-w-xl leading-relaxed">
		Pick which areas your QWK packets include, download your current mail, and upload a reply
		packet from your offline reader.
	</p>
</div>

<div class="mb-6 flex flex-wrap gap-2.5">
	<button class="btn-primary" disabled={downloading} onclick={download}>
		{downloading ? 'Building…' : 'Download .QWK now'}
	</button>
	<button class="btn-secondary" disabled={uploading} onclick={() => fileInput?.click()}>
		{uploading ? 'Uploading…' : 'Upload .REP reply'}
	</button>
	<input bind:this={fileInput} type="file" accept=".rep" class="hidden" onchange={handleUpload} />
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-muted">Loading…</p>
{:else if areas.length === 0}
	<p class="text-sm text-muted">No message areas available to you yet.</p>
{:else}
	<div class="flex flex-col">
		{#each areas as area (area.id)}
			<label class="list-row cursor-pointer gap-3.5 py-2.5 hover:bg-white/[0.025]">
				<input type="checkbox" class="check" checked={area.selected} onchange={() => toggle(area)} />
				<div class="min-w-0 flex-1">
					<div class="truncate text-[13.5px] font-medium text-slate-100">{area.name}</div>
					{#if area.description}
						<div class="mt-0.5 truncate text-xs text-faint">{area.description}</div>
					{/if}
				</div>
			</label>
		{/each}
	</div>
	<div class="mt-5 flex justify-end">
		<button class="btn-primary" disabled={saving} onclick={save}>
			{saving ? 'Saving…' : 'Save selection'}
		</button>
	</div>
{/if}
