<script lang="ts">
	import { t, i18n } from '$lib/i18n.svelte';
	// The off-site copy: each backup, encrypted with age to the sysop's
	// public key, to an SFTP server or OpenStack Swift.
	import { onMount } from 'svelte';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		getOffsite,
		saveOffsite,
		newOffsiteAgeKey,
		newOffsiteSSHKey,
		testOffsite,
		runOffsite,
		ApiError,
		type OffsiteSettings
	} from '$lib/api';

	let o = $state<OffsiteSettings | null>(null);
	let sftpPassword = $state('');
	let swiftPassword = $state('');
	let s3Secret = $state('');
	let davPassword = $state('');
	let privateKey = $state(''); // shown once, never stored here
	let keySaved = $state(false);
	let busy = $state<string | null>(null);
	let remote = $state<string[] | null>(null);
	let confirmHostKey = $state('');

	const fail = (err: unknown, fallback: string) => toast.push(err instanceof ApiError ? err.message : fallback, 'error');

	onMount(async () => {
		if (!auth.token) return;
		try {
			o = await getOffsite(auth.token);
			if (!o.kind) o.kind = 'swift';
		} catch (err) {
			fail(err, t('admin.offsite.could_not_load_the_off'));
		}
	});

	async function save(extra: Record<string, unknown> = {}) {
		if (!auth.token || !o) return false;
		busy = 'save';
		try {
			o = await saveOffsite(auth.token, {
				...o,
				...(sftpPassword ? { sftp_password: sftpPassword } : {}),
				...(swiftPassword ? { swift_password: swiftPassword } : {}),
				...(s3Secret ? { s3_secret_key: s3Secret } : {}),
				...(davPassword ? { webdav_password: davPassword } : {}),
				...extra
			});
			sftpPassword = swiftPassword = s3Secret = davPassword = '';
			return true;
		} catch (err) {
			fail(err, t('admin.common.could_not_save'));
			return false;
		} finally {
			busy = null;
		}
	}

	async function makeAgeKey() {
		if (!auth.token || !o) return;
		if (o.recipient && !confirm(t('admin.offsite.make_a_new_key_copies'))) return;
		const k = await newOffsiteAgeKey(auth.token);
		privateKey = k.private;
		keySaved = false;
		o.recipient = k.public;
	}

	function downloadKey() {
		const blob = new Blob([`# NullModem BBS backup key -- keep it safe, offline.\n# Decrypt a copy: age -d -i this-file nullmodem-....tar.gz.age > backup.tar.gz\n${privateKey}\n`], { type: 'text/plain' });
		const a = document.createElement('a');
		a.href = URL.createObjectURL(blob);
		a.download = 'nullmodem-backup-key.txt';
		a.click();
		keySaved = true;
	}

	async function makeSSHKey() {
		if (!auth.token || !o) return;
		if (o.sftp_public_key && !confirm(t('admin.offsite.make_a_new_login_key'))) return;
		if (!(await save())) return;
		o = await newOffsiteSSHKey(auth.token);
	}

	async function test() {
		if (!auth.token || !o) return;
		if (!(await save())) return;
		busy = 'test';
		try {
			const r = await testOffsite(auth.token);
			if (r.host_key) {
				confirmHostKey = r.host_key;
				if (r.error) toast.push(r.error, 'error');
				return;
			}
			remote = r.remote ?? [];
			toast.push(t('admin.offsite.it_works_written_read_back'), 'success');
		} catch (err) {
			fail(err, t('admin.common.the_test_failed'));
		} finally {
			busy = null;
		}
	}

	async function acceptHostKey() {
		if (!o) return;
		o.sftp_host_key = confirmHostKey;
		confirmHostKey = '';
		await test();
	}

	async function copyNow() {
		if (!auth.token) return;
		busy = 'run';
		try {
			o = await runOffsite(auth.token);
			toast.push(t('admin.offsite.the_newest_backup_is_there'), 'success');
		} catch (err) {
			fail(err, t('admin.offsite.the_copy_failed'));
			if (auth.token) o = await getOffsite(auth.token);
		} finally {
			busy = null;
		}
	}

	const when = (iso: string) => (iso && !iso.startsWith('0001') ? new Date(iso).toLocaleString(i18n.locale) : '');
</script>

<section class="card">
	<h2 class="card-label mb-1">{t('admin.common.off_site_copy')}</h2>
	<p class="mb-4 text-xs leading-relaxed text-muted">
		{t('admin.offsite.each_backup_also_goes_to')}
	</p>
	{#if o}
		<div class="flex flex-col gap-4">
			<!-- The key -->
			<div class="rounded-lg border border-line p-3">
				<div class="mb-2 flex flex-wrap items-center justify-between gap-2">
					<span class="text-sm font-medium text-ink-strong">{t('admin.offsite.1_the_encryption_key')}</span>
					<button class="btn-secondary btn-xs" onclick={makeAgeKey}>{o.recipient ? t('admin.offsite.new_key') : t('admin.offsite.make_a_key')}</button>
				</div>
				<input class="field field-sm w-full font-mono text-xs" bind:value={o.recipient} placeholder={t('admin.offsite.age1_the_public_key_make')} />
				{#if privateKey}
					<div class="mt-3 rounded-lg border border-amber-500/50 bg-amber-500/5 p-3 text-xs">
						<p class="mb-2 text-amber-300">
							{t('admin.offsite.your_private_key_shown_only')}
						</p>
						<code class="block break-all select-all text-ink">{privateKey}</code>
						<div class="mt-2 flex gap-2">
							<button class="btn-primary btn-xs" onclick={downloadKey}>{t('admin.offsite.download_it')}</button>
							<button class="btn-secondary btn-xs" onclick={() => { navigator.clipboard?.writeText(privateKey); keySaved = true; }}>{t('web.common.copy')}</button>
							<button class="btn-secondary btn-xs" disabled={!keySaved} onclick={() => (privateKey = '')}>{t('admin.offsite.i_ve_stored_it')}</button>
						</div>
					</div>
				{/if}
			</div>

			<!-- Where to -->
			<div class="rounded-lg border border-line p-3">
				<div class="mb-3 flex flex-wrap items-center gap-3">
					<span class="text-sm font-medium text-ink-strong">{t('admin.offsite.2_where_to')}</span>
					{#each [['s3', 'S3'], ['swift', 'OpenStack Swift'], ['sftp', 'SFTP'], ['webdav', 'WebDAV']] as [k, label] (k)}
						<button class="pill {o.kind === k ? 'pill-active' : ''}" onclick={() => o && (o.kind = k as 'sftp' | 'swift' | 's3' | 'webdav')}>{label}</button>
					{/each}
				</div>
				{#if o.kind === 'swift'}
					<div class="grid gap-2.5 sm:grid-cols-2">
						<label class="flex flex-col gap-1 text-xs text-muted sm:col-span-2">{t('admin.offsite.auth_url_keystone_v3')}
							<input class="field field-sm font-mono" bind:value={o.swift_auth_url} placeholder="https://swiss-backup02.infomaniak.com/identity/v3" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.common.user')}
							<input class="field field-sm font-mono" bind:value={o.swift_user} autocomplete="off" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.password_v', { V: o.swift_has_password ? t('admin.common.saved') : '' })}
							<input class="field field-sm" type="password" bind:value={swiftPassword} autocomplete="new-password" placeholder={o.swift_has_password ? '••••••••' : ''} /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.project')}
							<input class="field field-sm font-mono" bind:value={o.swift_project} placeholder="sb_project_…" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.region')}
							<input class="field field-sm font-mono" bind:value={o.swift_region} placeholder={t('admin.offsite.regionone')} /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.user_domain')}
							<input class="field field-sm font-mono" bind:value={o.swift_user_domain} placeholder={t('admin.offsite.default')} /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.project_domain')}
							<input class="field field-sm font-mono" bind:value={o.swift_project_domain} placeholder={t('admin.offsite.default')} /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.container')}
							<input class="field field-sm font-mono" bind:value={o.swift_container} placeholder="nullmodem" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.prefix_optional')}
							<input class="field field-sm font-mono" bind:value={o.swift_prefix} placeholder="bbs/" /></label>
					</div>
					<p class="mt-2 text-[11px] text-faint">{t('admin.offsite.infomaniak_swiss_backup_the_values')}</p>
				{:else if o.kind === 's3'}
					<div class="grid gap-2.5 sm:grid-cols-2">
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.endpoint')}
							<input class="field field-sm font-mono" bind:value={o.s3_endpoint} placeholder="s3.amazonaws.com" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.region')}
							<input class="field field-sm font-mono" bind:value={o.s3_region} placeholder="eu-central-1" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.bucket')}
							<input class="field field-sm font-mono" bind:value={o.s3_bucket} placeholder="nullmodem-backups" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.prefix_optional')}
							<input class="field field-sm font-mono" bind:value={o.s3_prefix} placeholder="bbs/" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.access_key')}
							<input class="field field-sm font-mono" bind:value={o.s3_access_key} autocomplete="off" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.secret_key_v', { V: o.s3_has_secret_key ? t('admin.common.saved') : '' })}
							<input class="field field-sm" type="password" bind:value={s3Secret} autocomplete="new-password" placeholder={o.s3_has_secret_key ? '••••••••' : ''} /></label>
					</div>
					<label class="mt-2 flex items-center gap-2 text-xs text-muted"><input type="checkbox" bind:checked={o.s3_path_style} /> {t('admin.offsite.path_style_addressing_minio_and')}</label>
					<p class="mt-2 text-[11px] leading-relaxed text-faint">
						AWS: <span class="font-mono">s3.amazonaws.com</span> {t('admin.offsite.hetzner')} <span class="font-mono">fsn1.your-objectstorage.com</span> {t('admin.offsite.infomaniak_see_the_s3_details')} <span class="font-mono">s3.eu-central-1.wasabisys.com</span> {t('admin.offsite.backblaze_b2')} <span class="font-mono">s3.eu-central-003.backblazeb2.com</span>{t('admin.offsite.the_bucket_is_made_if')}
					</p>
				{:else if o.kind === 'webdav'}
					<div class="grid gap-2.5 sm:grid-cols-2">
						<label class="flex flex-col gap-1 text-xs text-muted sm:col-span-2">{t('admin.offsite.folder_url')}
							<input class="field field-sm font-mono" bind:value={o.webdav_url} placeholder="https://cloud.example.org/remote.php/dav/files/me/bbs-backups" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.common.user')}
							<input class="field field-sm font-mono" bind:value={o.webdav_user} autocomplete="off" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.password_v', { V: o.webdav_has_password ? t('admin.common.saved') : t('admin.offsite.an_app_password_ideally') })}
							<input class="field field-sm" type="password" bind:value={davPassword} autocomplete="new-password" placeholder={o.webdav_has_password ? '••••••••' : ''} /></label>
					</div>
					<p class="mt-2 text-[11px] text-faint">{t('admin.offsite.nextcloud_owncloud_settings_webdav_shows')}</p>
				{:else}
					<div class="grid gap-2.5 sm:grid-cols-[1fr_6rem]">
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.common.host')}
							<input class="field field-sm font-mono" bind:value={o.sftp_host} placeholder="u123456.your-storagebox.de" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.common.port')}
							<input class="field field-sm font-mono" type="number" bind:value={o.sftp_port} placeholder="23" /></label>
					</div>
					<div class="mt-2.5 grid gap-2.5 sm:grid-cols-3">
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.common.user')}
							<input class="field field-sm font-mono" bind:value={o.sftp_user} autocomplete="off" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.common.directory')}
							<input class="field field-sm font-mono" bind:value={o.sftp_dir} placeholder="nullmodem-backups" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">{t('admin.offsite.password_v', { V: o.sftp_has_password ? t('admin.common.saved') : t('admin.offsite.or_the_key_below') })}
							<input class="field field-sm" type="password" bind:value={sftpPassword} autocomplete="new-password" placeholder={o.sftp_has_password ? '••••••••' : ''} /></label>
					</div>
					<div class="mt-3 text-xs text-muted">
						<div class="mb-1 flex items-center justify-between gap-2">
							<span>{t('admin.offsite.login_key_better_than_a')}</span>
							<button class="btn-secondary btn-xs" onclick={makeSSHKey}>{o.sftp_public_key ? t('admin.offsite.new_key') : t('admin.offsite.make_a_key')}</button>
						</div>
						{#if o.sftp_public_key}
							<code class="block break-all rounded-md border border-line p-2 select-all text-ink">{o.sftp_public_key}</code>
							<span class="text-[11px] text-faint">{t('admin.offsite.put_this_line_into_the')}</span>
						{/if}
						{#if o.sftp_host_key}
							<p class="mt-2 text-[11px] text-faint">{t('admin.offsite.server_s_key')} <span class="font-mono">{o.sftp_host_key}</span></p>
						{/if}
					</div>
				{/if}
				{#if confirmHostKey}
					<div class="mt-3 rounded-lg border border-accent/50 bg-accent/5 p-3 text-xs">
						<p class="mb-1 text-ink">{t('admin.offsite.first_connection_the_server_identifies')}</p>
						<code class="block font-mono text-accent">{confirmHostKey}</code>
						<p class="mt-1 text-muted">{t('admin.offsite.compare_it_with_your_provider')}</p>
						<div class="mt-2 flex gap-2">
							<button class="btn-primary btn-xs" onclick={acceptHostKey}>{t('admin.offsite.yes_it_s_the_right')}</button>
							<button class="btn-secondary btn-xs" onclick={() => (confirmHostKey = '')}>{t('admin.common.no')}</button>
						</div>
					</div>
				{/if}
			</div>

			<!-- On, keep, status -->
			<div class="rounded-lg border border-line p-3">
				<div class="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm">
					<span class="font-medium text-ink-strong">{t('admin.offsite.3_copy')}</span>
					<label class="flex items-center gap-2"><input type="checkbox" bind:checked={o.enabled} disabled={!o.recipient} /> {t('admin.offsite.after_each_backup')}</label>
					<label class="flex items-center gap-1.5 text-xs text-muted">{t('admin.offsite.keep')} <input class="field field-sm w-16" type="number" min="1" bind:value={o.keep_daily} /> {t('admin.offsite.daily')}</label>
					<label class="flex items-center gap-1.5 text-xs text-muted">+ <input class="field field-sm w-16" type="number" min="0" bind:value={o.keep_weekly} /> {t('admin.offsite.weekly')}</label>
				</div>
				{#if o.status.last_ok || o.status.last_error}
					<p class="mt-2 text-xs {o.status.last_error ? 'text-amber-300' : 'text-muted'}">
						{#if o.status.last_error}{t('admin.offsite.last_try_v_last_error', { V: when(o.status.last_try), LAST_ERROR: o.status.last_error })}{:else}{t('admin.offsite.last_copy_v_last_name', { V: when(o.status.last_ok), LAST_NAME: o.status.last_name, REMOTE: o.status.remote })}{/if}
					</p>
				{/if}
				{#if remote}
					<p class="mt-2 text-xs text-muted">{t('admin.offsite.there_now_v', { V: remote.length ? remote.join(', ') : 'nothing yet' })}</p>
				{/if}
			</div>

			<div class="flex flex-wrap justify-end gap-2">
				<button class="btn-secondary btn-sm" disabled={busy !== null} onclick={test}>{busy === 'test' ? t('admin.common.testing') : t('admin.offsite.save_and_test')}</button>
				<button class="btn-secondary btn-sm" disabled={busy !== null || !o.enabled} onclick={copyNow}>{busy === 'run' ? t('admin.offsite.copying') : t('admin.offsite.copy_the_newest_now')}</button>
				<button class="btn-primary btn-sm" disabled={busy !== null} onclick={async () => (await save()) && toast.push(t('common.saved'), 'success')}>{t('web.common.save')}</button>
			</div>
		</div>
	{/if}
</section>
