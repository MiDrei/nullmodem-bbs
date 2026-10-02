<script lang="ts">
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
			loadError = err instanceof ApiError ? err.message : 'Could not load the settings.';
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
			toast.push('Saved.', 'success');
		} catch (err) {
			await failed(err, 'Could not save.');
		} finally {
			saving = false;
		}
	}

	async function run(dry: boolean) {
		if (!auth.token) return;
		if (!dry && !confirm('Clean up now by the saved limits? What it deletes is gone.')) return;
		running = dry ? 'preview' : 'run';
		try {
			const r = await runMaintenance(auth.token, dry);
			if (dry) preview = r;
			else {
				last = r;
				preview = null;
				toast.push('Cleanup done.', 'success');
			}
		} catch (err) {
			await failed(err, 'The cleanup failed.');
		} finally {
			running = null;
		}
	}

	const mb = (n: number) => `${(n / 1048576).toFixed(1)} MB`;
	const when = (iso: string) => new Date(iso).toLocaleString();

	const limits: { key: keyof MaintenanceSettings; label: string; hint: string }[] = [
		{ key: 'message_keep_days', label: 'Echomail: keep days', hint: 'Per area; an area can set its own.' },
		{ key: 'message_keep_max', label: 'Echomail: at most per area', hint: '0 = no count limit.' },
		{ key: 'data_area_keep_days', label: 'Data areas: keep days', hint: 'FSX_DAT and other data areas.' },
		{ key: 'file_keep_days', label: 'Files: keep days', hint: 'Per file area; 0 = keep.' },
		{ key: 'netmail_keep_days', label: 'Read netmail: keep days', hint: '0 = keep. Unread never goes.' },
		{ key: 'log_keep_rows', label: 'Log: keep entries', hint: 'The newest ones.' },
		{ key: 'transcript_keep_days', label: 'BinkP transcripts: keep days', hint: 'Session logs.' },
		{ key: 'archive_keep_days', label: 'Inbound archive: keep days', hint: 'Copies of received files.' },
		{ key: 'pending_user_days', label: 'Unapproved accounts: days', hint: 'Never approved (bots); 0 = keep.' }
	];
</script>

{#snippet report(r: MaintenanceReport, title: string)}
	<section class="card">
		<h2 class="card-label mb-3">{title}</h2>
		<p class="mb-3 text-xs text-faint">
			{when(r.started_at)} · {r.seconds.toFixed(1)} s{r.dry_run ? ' · preview, nothing deleted' : ''}
		</p>
		<dl class="grid grid-cols-2 gap-x-6 gap-y-1.5 text-[13px] sm:grid-cols-4">
			<dt class="text-muted">Echomail</dt>
			<dd class="text-ink">{r.messages}</dd>
			<dt class="text-muted">Files</dt>
			<dd class="text-ink">{r.files} ({mb(r.file_bytes)})</dd>
			<dt class="text-muted">Netmail</dt>
			<dd class="text-ink">{r.netmail}</dd>
			<dt class="text-muted">Log entries</dt>
			<dd class="text-ink">{r.logs}</dd>
			<dt class="text-muted">Transcripts</dt>
			<dd class="text-ink">{r.transcripts}</dd>
			<dt class="text-muted">Archived files</dt>
			<dd class="text-ink">{r.archive}</dd>
			<dt class="text-muted">Unapproved accounts</dt>
			<dd class="text-ink">{r.pending_users ?? 0}</dd>
			{#if !r.dry_run && r.db_bytes_before}
				<dt class="text-muted">Database</dt>
				<dd class="text-ink">
					{mb(r.db_bytes_before)} → {mb(r.db_bytes_after)}{r.vacuumed ? ' (compacted)' : ''}
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
	<h1 class="page-title">Maintenance</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">
		Cleans up old echomail, files and read netmail, the log, BinkP transcripts and the inbound
		archive, then compacts the database. Never deleted: your posts not yet sent to the hub, unread or
		unsent netmail, and messages younger than 14 days.
	</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !settings}
	<p class="text-sm text-muted">Loading…</p>
{:else}
	<div class="flex flex-col gap-4">
		<form class="card flex flex-col gap-4" onsubmit={save}>
			<div class="flex flex-wrap items-center gap-4">
				<label class="flex items-center gap-2 text-sm">
					<input type="checkbox" class="check" bind:checked={settings.enabled} />
					<span class="text-ink">Clean up every night at</span>
				</label>
				<select class="field field-sm w-24" bind:value={settings.hour} disabled={!settings.enabled}>
					{#each Array.from({ length: 24 }, (_, h) => h) as h (h)}
						<option value={h}>{String(h).padStart(2, '0')}:00</option>
					{/each}
				</select>
				<span class="text-xs text-faint">server time; off, it only runs from here</span>
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
				<span class="text-muted">Compact the database afterwards <span class="text-xs text-faint">(only when 10 % or more of it is unused)</span></span>
			</label>
			<div class="flex flex-wrap justify-end gap-2.5">
				<button type="button" class="btn-secondary btn-sm" disabled={running !== null} onclick={() => run(true)}>
					{running === 'preview' ? 'Counting…' : 'Preview'}
				</button>
				<button type="button" class="btn-danger btn-sm" disabled={running !== null} onclick={() => run(false)}>
					{running === 'run' ? 'Cleaning up…' : 'Run now'}
				</button>
				<button type="submit" class="btn-primary btn-sm" disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
			</div>
			<p class="text-xs text-faint">Preview and Run now use the saved limits — save changes first.</p>
		</form>

		{#if preview}
			{@render report(preview, 'Preview')}
		{/if}
		{#if last}
			{@render report(last, 'Last run')}
		{/if}
	</div>
{/if}
