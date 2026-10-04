<script lang="ts">
	import { t } from '$lib/i18n.svelte';
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
			loadError = err instanceof ApiError ? err.message : t('web.areas.load_failed');
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
			toast.push(t('web.qwk.saved'), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('web.qwk.save_failed'), 'error');
		} finally {
			saving = false;
		}
	}

	async function download() {
		if (!bbsAuth.token) return;
		downloading = true;
		try {
			const got = await downloadQWKPacket(bbsAuth.token);
			toast.push(got ? t('web.qwk.downloaded') : t('web.qwk.no_mail'), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('web.qwk.download_failed'), 'error');
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
				t('web.qwk.processed', { POSTED: result.posted, SENT: result.sent }) +
					(result.skipped > 0 ? t('web.qwk.skipped', { COUNT: result.skipped }) : '') +
					'.',
				'success'
			);
			for (const r of result.rejected ?? []) {
				toast.push(t('web.qwk.not_delivered', { SUBJECT: r.subject, TO: r.to, REASON: r.reason }), 'error');
			}
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('web.qwk.process_failed'), 'error');
		} finally {
			uploading = false;
			input.value = '';
		}
	}
</script>

<div class="mb-5">
	<h1 class="page-title">{t('web.qwk.title')}</h1>
	<p class="page-subtitle max-w-xl leading-relaxed">{t('web.qwk.subtitle')}</p>
</div>

<div class="mb-6 flex flex-wrap gap-2.5">
	<button class="btn-primary" disabled={downloading} onclick={download}>
		{downloading ? t('web.qwk.building') : t('web.qwk.download')}
	</button>
	<button class="btn-secondary" disabled={uploading} onclick={() => fileInput?.click()}>
		{uploading ? t('web.files.uploading') : t('web.qwk.upload')}
	</button>
	<input bind:this={fileInput} type="file" accept=".rep" class="hidden" onchange={handleUpload} />
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-muted">{t('web.common.loading')}</p>
{:else if areas.length === 0}
	<p class="text-sm text-muted">{t('web.areas.none')}</p>
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
			{saving ? t('web.common.saving') : t('web.qwk.save')}
		</button>
	</div>
{/if}
