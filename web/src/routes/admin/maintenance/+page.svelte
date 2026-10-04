<script lang="ts">
	import { t, i18n } from '$lib/i18n.svelte';
	// The nightly cleanup: its limits, a preview (counts only, nothing
	// deleted), running it now, and what the last run did.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		getMaintenance,
		putMaintenance,
		runMaintenance,
		ApiError,
		type MaintenanceSettings,
		type MaintenanceReport
	} from '$lib/api';

	let settings = $state<MaintenanceSettings | null>(null);
	let last = $state<MaintenanceReport | null>(null);
	let preview = $state<MaintenanceReport | null>(null);
	let loadError = $state<string | null>(null);
	let saving = $state(false);
	let running = $state<'preview' | 'run' | null>(null);

	async function failed(err: unknown, fallback: string) {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return;
		}
		toast.push(err instanceof ApiError ? err.message : fallback, 'error');
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			const st = await getMaintenance(auth.token);
			settings = st.settings;
			last = st.last;
		} catch (err) {
			loadError = err instanceof ApiError ? err.message : t('admin.maintenance.could_not_load_the_settings');
		}
	});

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!auth.token || !settings) return;
		saving = true;
		try {
			const st = await putMaintenance(auth.token, $state.snapshot(settings) as MaintenanceSettings);
			settings = st.settings;
			preview = null;
			toast.push(t('admin.maintenance.saved'), 'success');
		} catch (err) {
			await failed(err, t('admin.maintenance.could_not_save'));
		} finally {
			saving = false;
		}
	}

	async function run(dry: boolean) {
		if (!auth.token) return;
		if (!dry && !confirm(t('admin.maintenance.clean_up_now_by_the'))) return;
		running = dry ? 'preview' : 'run';
		try {
			const r = await runMaintenance(auth.token, dry);
			if (dry) preview = r;
			else {
				last = r;
				preview = null;
				toast.push(t('admin.maintenance.cleanup_done'), 'success');
			}
		} catch (err) {
			await failed(err, t('admin.maintenance.the_cleanup_failed'));
		} finally {
			running = null;
		}
	}

	const mb = (n: number) => `${(n / 1048576).toFixed(1)} MB`;
	const when = (iso: string) => new Date(iso).toLocaleString(i18n.locale);

	const limits: { key: keyof MaintenanceSettings; label: string; hint: string }[] = [
		{ key: 'message_keep_days', label: t('admin.maintenance.echomail_keep_days'), hint: t('admin.maintenance.per_area_an_area_can') },
		{ key: 'message_keep_max', label: t('admin.maintenance.echomail_at_most_per_area'), hint: t('admin.maintenance.0_no_count_limit') },
		{ key: 'data_area_keep_days', label: t('admin.maintenance.data_areas_keep_days'), hint: t('admin.maintenance.fsx_dat_and_other_data') },
		{ key: 'file_keep_days', label: t('admin.maintenance.files_keep_days'), hint: t('admin.maintenance.per_file_area_0_keep') },
		{ key: 'netmail_keep_days', label: t('admin.maintenance.read_netmail_keep_days'), hint: t('admin.maintenance.0_keep_unread_never_goes') },
		{ key: 'log_keep_rows', label: t('admin.maintenance.log_keep_entries'), hint: t('admin.maintenance.the_newest_ones') },
		{ key: 'transcript_keep_days', label: t('admin.maintenance.binkp_transcripts_keep_days'), hint: t('admin.maintenance.session_logs') },
		{ key: 'archive_keep_days', label: t('admin.maintenance.inbound_archive_keep_days'), hint: t('admin.maintenance.copies_of_received_files') },
		{ key: 'pending_user_days', label: t('admin.maintenance.unapproved_accounts_days'), hint: t('admin.maintenance.never_approved_bots_0_keep') }
	];
</script>

{#snippet report(r: MaintenanceReport, title: string)}
	<section class="card">
		<h2 class="card-label mb-3">{title}</h2>
		<p class="mb-3 text-xs text-faint">
			{when(r.started_at)} · {r.seconds.toFixed(1)} s{r.dry_run ? t('admin.maintenance.preview_nothing_deleted') : ''}
		</p>
		<dl class="grid grid-cols-2 gap-x-6 gap-y-1.5 text-[13px] sm:grid-cols-4">
			<dt class="text-muted">{t('admin.maintenance.echomail')}</dt>
			<dd class="text-ink">{r.messages}</dd>
			<dt class="text-muted">{t('admin.maintenance.files')}</dt>
			<dd class="text-ink">{r.files} ({mb(r.file_bytes)})</dd>
			<dt class="text-muted">{t('admin.maintenance.netmail')}</dt>
			<dd class="text-ink">{r.netmail}</dd>
			<dt class="text-muted">{t('admin.maintenance.log_entries')}</dt>
			<dd class="text-ink">{r.logs}</dd>
			<dt class="text-muted">{t('admin.maintenance.transcripts')}</dt>
			<dd class="text-ink">{r.transcripts}</dd>
			<dt class="text-muted">{t('admin.maintenance.archived_files')}</dt>
			<dd class="text-ink">{r.archive}</dd>
			<dt class="text-muted">{t('admin.maintenance.unapproved_accounts')}</dt>
			<dd class="text-ink">{r.pending_users ?? 0}</dd>
			{#if !r.dry_run && r.db_bytes_before}
				<dt class="text-muted">{t('admin.maintenance.database')}</dt>
				<dd class="text-ink">
					{mb(r.db_bytes_before)} → {mb(r.db_bytes_after)}{r.vacuumed ? t('admin.maintenance.compacted') : ''}
				</dd>
				<dt class="text-muted">WAL</dt>
				<dd class="text-ink">{mb(r.wal_bytes ?? 0)}</dd>
			{/if}
		</dl>
		{#if r.message_areas.length}
			<p class="mt-3 text-xs leading-relaxed text-faint">
				{r.message_areas.map((a) => `${a.tag} ${a.count}`).join(' · ')}
			</p>
		{/if}
		{#each r.errors as e (e)}
			<p class="mt-2 text-sm text-red-400">{e}</p>
		{/each}
	</section>
{/snippet}

<div class="mb-6">
	<h1 class="page-title">{t('admin.maintenance.maintenance')}</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">
		{t('admin.maintenance.cleans_up_old_echomail_files')}
	</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !settings}
	<p class="text-sm text-muted">{t('admin.common.loading')}</p>
{:else}
	<div class="flex flex-col gap-4">
		<form class="card flex flex-col gap-4" onsubmit={save}>
			<div class="flex flex-wrap items-center gap-4">
				<label class="flex items-center gap-2 text-sm">
					<input type="checkbox" class="check" bind:checked={settings.enabled} />
					<span class="text-ink">{t('admin.maintenance.clean_up_every_night_at')}</span>
				</label>
				<select class="field field-sm w-24" bind:value={settings.hour} disabled={!settings.enabled}>
					{#each Array.from({ length: 24 }, (_, h) => h) as h (h)}
						<option value={h}>{String(h).padStart(2, '0')}:00</option>
					{/each}
				</select>
				<span class="text-xs text-faint">{t('admin.maintenance.server_time_off_it_only')}</span>
			</div>
			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
				{#each limits as l (l.key)}
					<label class="flex flex-col gap-1">
						<span class="text-xs text-muted">{l.label}</span>
						<input type="number" min="0" class="field field-sm" bind:value={settings[l.key]} />
						<span class="text-[11px] text-faint">{l.hint}</span>
					</label>
				{/each}
			</div>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={settings.vacuum} />
				<span class="text-muted">{t('admin.maintenance.compact_the_database_afterwards')} <span class="text-xs text-faint">{t('admin.maintenance.only_when_10_or_more')}</span></span>
			</label>
			<div class="flex flex-wrap justify-end gap-2.5">
				<button type="button" class="btn-secondary btn-sm" disabled={running !== null} onclick={() => run(true)}>
					{running === 'preview' ? t('admin.maintenance.counting') : t('admin.maintenance.preview')}
				</button>
				<button type="button" class="btn-danger btn-sm" disabled={running !== null} onclick={() => run(false)}>
					{running === 'run' ? t('admin.maintenance.cleaning_up') : t('admin.maintenance.run_now')}
				</button>
				<button type="submit" class="btn-primary btn-sm" disabled={saving}>{saving ? t('admin.common.saving') : t('admin.common.save')}</button>
			</div>
			<p class="text-xs text-faint">{t('admin.maintenance.preview_and_run_now_use')}</p>
		</form>

		{#if preview}
			{@render report(preview, t('admin.maintenance.preview'))}
		{/if}
		{#if last}
			{@render report(last, t('admin.maintenance.last_run'))}
		{/if}
	</div>
{/if}
