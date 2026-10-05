<script lang="ts">
	import { t, i18n } from '$lib/i18n.svelte';
	// The nightly backup: when and how many are kept, the backups on
	// disk, writing one now, downloading and deleting.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import OffsiteSettings from '$lib/admin/OffsiteSettings.svelte';
	import {
		getBackups,
		putBackupSettings,
		runBackup,
		deleteBackup,
		downloadBackup,
		ApiError,
		type BackupSettings,
		type BackupInfo
	} from '$lib/api';

	let settings = $state<BackupSettings | null>(null);
	let backups = $state<BackupInfo[]>([]);
	let freeBytes = $state(0);
	let loadError = $state<string | null>(null);
	let saving = $state(false);
	let running = $state(false);
	let downloading = $state<string | null>(null);

	async function failed(err: unknown, fallback: string) {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return;
		}
		toast.push(err instanceof ApiError ? err.message : fallback, 'error');
	}

	function apply(st: { settings: BackupSettings; backups: BackupInfo[]; free_bytes: number }) {
		settings = st.settings;
		backups = st.backups;
		freeBytes = st.free_bytes;
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			apply(await getBackups(auth.token));
		} catch (err) {
			loadError = err instanceof ApiError ? err.message : t('admin.backups.could_not_load_the_backups');
		}
	});

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!auth.token || !settings) return;
		saving = true;
		try {
			apply(await putBackupSettings(auth.token, $state.snapshot(settings) as BackupSettings));
			toast.push(t('common.saved'), 'success');
		} catch (err) {
			await failed(err, t('admin.common.could_not_save'));
		} finally {
			saving = false;
		}
	}

	async function runNow() {
		if (!auth.token) return;
		running = true;
		try {
			apply(await runBackup(auth.token));
			toast.push(t('admin.backups.backup_written'), 'success');
		} catch (err) {
			await failed(err, t('admin.backups.the_backup_failed'));
		} finally {
			running = false;
		}
	}

	async function download(b: BackupInfo) {
		if (!auth.token) return;
		downloading = b.name;
		try {
			const blob = await downloadBackup(auth.token, b.name);
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = b.name;
			a.click();
			setTimeout(() => URL.revokeObjectURL(url), 10000);
		} catch (err) {
			await failed(err, t('admin.backups.could_not_download_the_backup'));
		} finally {
			downloading = null;
		}
	}

	async function remove(b: BackupInfo) {
		if (!auth.token || !confirm(t('admin.backups.delete_the_backup_of_v', { V: when(b.time) }))) return;
		try {
			await deleteBackup(auth.token, b.name);
			backups = backups.filter((x) => x.name !== b.name);
		} catch (err) {
			await failed(err, t('admin.backups.could_not_delete_the_backup'));
		}
	}

	const mb = (n: number) => (n >= 1073741824 ? `${(n / 1073741824).toFixed(1)} GB` : `${(n / 1048576).toFixed(1)} MB`);
	const when = (iso: string) => new Date(iso).toLocaleString(i18n.locale);
	let total = $derived(backups.reduce((n, b) => n + b.size, 0));
	let newestAge = $derived(backups.length ? (Date.now() - new Date(backups[0].time).getTime()) / 3600000 : Infinity);
</script>

<div class="mb-6">
	<h1 class="page-title">{t('admin.common.backups')}</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">
		{t('admin.backups.every_night_a_copy_of')}
	</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !settings}
	<p class="text-sm text-muted">{t('web.common.loading')}</p>
{:else}
	<div class="flex flex-col gap-4">
		{#if settings.enabled && newestAge > 26 && backups.length}
			<p class="rounded-xl border border-amber-800 bg-amber-950/40 px-4 py-3 text-sm text-amber-300">
				{t('admin.backups.the_newest_backup_is_v', { V: Math.round(newestAge / 24) })}
			</p>
		{/if}
		<p class="rounded-xl border border-line bg-sunken px-4 py-3 text-sm text-muted">
			{t('admin.backups.the_backups_are_on_the')}
		</p>

		<form class="card flex flex-col gap-4" onsubmit={save}>
			<div class="flex flex-wrap items-center gap-4">
				<label class="flex items-center gap-2 text-sm">
					<input type="checkbox" class="check" bind:checked={settings.enabled} />
					<span class="text-ink">{t('admin.backups.back_up_every_night_at')}</span>
				</label>
				<select class="field field-sm w-24" bind:value={settings.hour} disabled={!settings.enabled}>
					{#each Array.from({ length: 24 }, (_, h) => h) as h (h)}
						<option value={h}>{String(h).padStart(2, '0')}:00</option>
					{/each}
				</select>
				<span class="text-xs text-faint">{t('admin.backups.server_time')}</span>
			</div>
			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.backups.keep_daily')}</span>
					<input type="number" min="1" class="field field-sm" bind:value={settings.keep_daily} />
					<span class="text-[11px] text-faint">{t('admin.common.the_newest_ones')}</span>
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.backups.keep_weekly')}</span>
					<input type="number" min="0" class="field field-sm" bind:value={settings.keep_weekly} />
					<span class="text-[11px] text-faint">{t('admin.backups.plus_the_newest_of_each')}</span>
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.common.directory')}</span>
					<input class="field field-sm font-mono" bind:value={settings.dir} />
					<span class="text-[11px] text-faint">{t('admin.backups.inside_the_container_data_backups')}</span>
				</label>
			</div>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={settings.include_files} />
				<span class="text-muted">{t('admin.backups.include_the_file_areas_and')} <span class="text-xs text-faint">{t('admin.backups.much_bigger')}</span></span>
			</label>
			<div class="flex flex-wrap justify-end gap-2.5">
				<button type="button" class="btn-secondary btn-sm" disabled={running} onclick={runNow}>
					{running ? t('admin.backups.backing_up') : t('admin.backups.back_up_now')}
				</button>
				<button type="submit" class="btn-primary btn-sm" disabled={saving}>{saving ? t('web.common.saving') : t('web.common.save')}</button>
			</div>
		</form>

		<section class="card">
			<div class="mb-3 flex flex-wrap items-baseline justify-between gap-2">
				<h2 class="card-label">{t('admin.common.backups')}</h2>
				<span class="text-xs text-faint">
					{backups.length} · {mb(total)}{freeBytes ? t('admin.backups.v_free', { V: mb(freeBytes) }) : ''}
				</span>
			</div>
			{#if backups.length === 0}
				<p class="text-sm text-muted">{t('admin.backups.none_yet_v', { V: settings.enabled ? t('admin.backups.the_first_one_is_written', { V: String(settings.hour).padStart(2, '0') }) : '' })}</p>
			{:else}
				<table class="w-full text-left text-sm">
					<tbody class="divide-y divide-slate-800">
						{#each backups as b (b.name)}
							<tr>
								<td class="py-2 text-ink">{when(b.time)}</td>
								<td class="py-2 text-right font-mono text-xs text-muted">{mb(b.size)}</td>
								<td class="py-2 text-right whitespace-nowrap">
									<button type="button" class="btn-secondary btn-xs" disabled={downloading !== null} onclick={() => download(b)}>
										{downloading === b.name ? t('web.common.loading') : t('web.common.download')}
									</button>
									<button type="button" class="btn-secondary btn-xs" onclick={() => remove(b)}>{t('web.common.delete')}</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		</section>
		<OffsiteSettings />
	</div>
{/if}
