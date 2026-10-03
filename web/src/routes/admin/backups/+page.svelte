<script lang="ts">
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
			loadError = err instanceof ApiError ? err.message : 'Could not load the backups.';
		}
	});

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!auth.token || !settings) return;
		saving = true;
		try {
			apply(await putBackupSettings(auth.token, $state.snapshot(settings) as BackupSettings));
			toast.push('Saved.', 'success');
		} catch (err) {
			await failed(err, 'Could not save.');
		} finally {
			saving = false;
		}
	}

	async function runNow() {
		if (!auth.token) return;
		running = true;
		try {
			apply(await runBackup(auth.token));
			toast.push('Backup written.', 'success');
		} catch (err) {
			await failed(err, 'The backup failed.');
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
			await failed(err, 'Could not download the backup.');
		} finally {
			downloading = null;
		}
	}

	async function remove(b: BackupInfo) {
		if (!auth.token || !confirm(`Delete the backup of ${when(b.time)}?`)) return;
		try {
			await deleteBackup(auth.token, b.name);
			backups = backups.filter((x) => x.name !== b.name);
		} catch (err) {
			await failed(err, 'Could not delete the backup.');
		}
	}

	const mb = (n: number) => (n >= 1073741824 ? `${(n / 1073741824).toFixed(1)} GB` : `${(n / 1048576).toFixed(1)} MB`);
	const when = (iso: string) => new Date(iso).toLocaleString();
	let total = $derived(backups.reduce((n, b) => n + b.size, 0));
	let newestAge = $derived(backups.length ? (Date.now() - new Date(backups[0].time).getTime()) / 3600000 : Infinity);
</script>

<div class="mb-6">
	<h1 class="page-title">Backups</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">
		Every night a copy of the database, the configuration (bbs.yaml, web.yaml, menus and screens) and
		the keys (login secret, SSH host key, push key) -- taken while the BBS keeps running. Restoring is
		unpacking one into the BBS's directory; see docs/backup.md.
	</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !settings}
	<p class="text-sm text-muted">Loading…</p>
{:else}
	<div class="flex flex-col gap-4">
		{#if settings.enabled && newestAge > 26 && backups.length}
			<p class="rounded-xl border border-amber-800 bg-amber-950/40 px-4 py-3 text-sm text-amber-300">
				The newest backup is {Math.round(newestAge / 24)} day(s) old -- see the log (Web tab) for why.
			</p>
		{/if}
		<p class="rounded-xl border border-line bg-sunken px-4 py-3 text-sm text-muted">
			The backups are on the same disk as the BBS: they help against mistakes and a broken database,
			not a broken disk or a lost server. For that, turn on the off-site copy below -- encrypted, to
			an SFTP server or OpenStack Swift.
		</p>

		<form class="card flex flex-col gap-4" onsubmit={save}>
			<div class="flex flex-wrap items-center gap-4">
				<label class="flex items-center gap-2 text-sm">
					<input type="checkbox" class="check" bind:checked={settings.enabled} />
					<span class="text-ink">Back up every night at</span>
				</label>
				<select class="field field-sm w-24" bind:value={settings.hour} disabled={!settings.enabled}>
					{#each Array.from({ length: 24 }, (_, h) => h) as h (h)}
						<option value={h}>{String(h).padStart(2, '0')}:00</option>
					{/each}
				</select>
				<span class="text-xs text-faint">server time</span>
			</div>
			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">Keep daily</span>
					<input type="number" min="1" class="field field-sm" bind:value={settings.keep_daily} />
					<span class="text-[11px] text-faint">The newest ones.</span>
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">Keep weekly</span>
					<input type="number" min="0" class="field field-sm" bind:value={settings.keep_weekly} />
					<span class="text-[11px] text-faint">Plus the newest of each of the last weeks.</span>
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">Directory</span>
					<input class="field field-sm font-mono" bind:value={settings.dir} />
					<span class="text-[11px] text-faint">Inside the container; data/backups is in data/ on the host.</span>
				</label>
			</div>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={settings.include_files} />
				<span class="text-muted">Include the file areas and doors <span class="text-xs text-faint">(much bigger)</span></span>
			</label>
			<div class="flex flex-wrap justify-end gap-2.5">
				<button type="button" class="btn-secondary btn-sm" disabled={running} onclick={runNow}>
					{running ? 'Backing up…' : 'Back up now'}
				</button>
				<button type="submit" class="btn-primary btn-sm" disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
			</div>
		</form>

		<section class="card">
			<div class="mb-3 flex flex-wrap items-baseline justify-between gap-2">
				<h2 class="card-label">Backups</h2>
				<span class="text-xs text-faint">
					{backups.length} · {mb(total)}{freeBytes ? ` · ${mb(freeBytes)} free` : ''}
				</span>
			</div>
			{#if backups.length === 0}
				<p class="text-sm text-muted">None yet{settings.enabled ? ` -- the first one is written at ${String(settings.hour).padStart(2, '0')}:00` : ''}.</p>
			{:else}
				<table class="w-full text-left text-sm">
					<tbody class="divide-y divide-slate-800">
						{#each backups as b (b.name)}
							<tr>
								<td class="py-2 text-ink">{when(b.time)}</td>
								<td class="py-2 text-right font-mono text-xs text-muted">{mb(b.size)}</td>
								<td class="py-2 text-right whitespace-nowrap">
									<button type="button" class="btn-secondary btn-xs" disabled={downloading !== null} onclick={() => download(b)}>
										{downloading === b.name ? 'Loading…' : 'Download'}
									</button>
									<button type="button" class="btn-secondary btn-xs" onclick={() => remove(b)}>Delete</button>
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
