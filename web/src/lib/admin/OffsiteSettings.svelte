<script lang="ts">
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
			fail(err, 'Could not load the off-site settings.');
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
			fail(err, 'Could not save.');
			return false;
		} finally {
			busy = null;
		}
	}

	async function makeAgeKey() {
		if (!auth.token || !o) return;
		if (o.recipient && !confirm('Make a new key? Copies made with the old one still need the old private key.')) return;
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
		if (o.sftp_public_key && !confirm('Make a new login key? The old one stops working once you replace it on the server.')) return;
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
			toast.push('It works: written, read back and deleted.', 'success');
		} catch (err) {
			fail(err, 'The test failed.');
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
			toast.push('The newest backup is there.', 'success');
		} catch (err) {
			fail(err, 'The copy failed.');
			if (auth.token) o = await getOffsite(auth.token);
		} finally {
			busy = null;
		}
	}

	const when = (iso: string) => (iso && !iso.startsWith('0001') ? new Date(iso).toLocaleString() : '');
</script>

<section class="card">
	<h2 class="card-label mb-1">Off-site copy</h2>
	<p class="mb-4 text-xs leading-relaxed text-muted">
		Each backup also goes to a storage service (S3, OpenStack Swift, SFTP or WebDAV) -- encrypted first with your key, so neither the service nor anyone who gets
		hold of this server can read the copies there. Only the private key you keep opens them.
	</p>
	{#if o}
		<div class="flex flex-col gap-4">
			<!-- The key -->
			<div class="rounded-lg border border-line p-3">
				<div class="mb-2 flex flex-wrap items-center justify-between gap-2">
					<span class="text-sm font-medium text-ink-strong">1. The encryption key</span>
					<button class="btn-secondary btn-xs" onclick={makeAgeKey}>{o.recipient ? 'New key…' : 'Make a key'}</button>
				</div>
				<input class="field field-sm w-full font-mono text-xs" bind:value={o.recipient} placeholder="age1… (the public key -- make one, or paste your own)" />
				{#if privateKey}
					<div class="mt-3 rounded-lg border border-amber-500/50 bg-amber-500/5 p-3 text-xs">
						<p class="mb-2 text-amber-300">
							Your private key -- shown only now. Without it the copies can't be opened, by anyone. Keep it in your password
							manager or on paper, not on this server.
						</p>
						<code class="block break-all select-all text-ink">{privateKey}</code>
						<div class="mt-2 flex gap-2">
							<button class="btn-primary btn-xs" onclick={downloadKey}>Download it</button>
							<button class="btn-secondary btn-xs" onclick={() => { navigator.clipboard?.writeText(privateKey); keySaved = true; }}>Copy</button>
							<button class="btn-secondary btn-xs" disabled={!keySaved} onclick={() => (privateKey = '')}>I've stored it</button>
						</div>
					</div>
				{/if}
			</div>

			<!-- Where to -->
			<div class="rounded-lg border border-line p-3">
				<div class="mb-3 flex flex-wrap items-center gap-3">
					<span class="text-sm font-medium text-ink-strong">2. Where to</span>
					{#each [['s3', 'S3'], ['swift', 'OpenStack Swift'], ['sftp', 'SFTP'], ['webdav', 'WebDAV']] as [k, label] (k)}
						<button class="pill {o.kind === k ? 'pill-active' : ''}" onclick={() => o && (o.kind = k as 'sftp' | 'swift' | 's3' | 'webdav')}>{label}</button>
					{/each}
				</div>
				{#if o.kind === 'swift'}
					<div class="grid gap-2.5 sm:grid-cols-2">
						<label class="flex flex-col gap-1 text-xs text-muted sm:col-span-2">Auth URL (Keystone v3)
							<input class="field field-sm font-mono" bind:value={o.swift_auth_url} placeholder="https://swiss-backup02.infomaniak.com/identity/v3" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">User
							<input class="field field-sm font-mono" bind:value={o.swift_user} autocomplete="off" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Password {o.swift_has_password ? '(saved)' : ''}
							<input class="field field-sm" type="password" bind:value={swiftPassword} autocomplete="new-password" placeholder={o.swift_has_password ? '••••••••' : ''} /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Project
							<input class="field field-sm font-mono" bind:value={o.swift_project} placeholder="sb_project_…" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Region
							<input class="field field-sm font-mono" bind:value={o.swift_region} placeholder="RegionOne" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">User domain
							<input class="field field-sm font-mono" bind:value={o.swift_user_domain} placeholder="Default" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Project domain
							<input class="field field-sm font-mono" bind:value={o.swift_project_domain} placeholder="Default" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Container
							<input class="field field-sm font-mono" bind:value={o.swift_container} placeholder="nullmodem" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Prefix (optional)
							<input class="field field-sm font-mono" bind:value={o.swift_prefix} placeholder="bbs/" /></label>
					</div>
					<p class="mt-2 text-[11px] text-faint">Infomaniak Swiss Backup: the values are in the device's "OpenStack" / rclone settings (OpenRC file).</p>
				{:else if o.kind === 's3'}
					<div class="grid gap-2.5 sm:grid-cols-2">
						<label class="flex flex-col gap-1 text-xs text-muted">Endpoint
							<input class="field field-sm font-mono" bind:value={o.s3_endpoint} placeholder="s3.amazonaws.com" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Region
							<input class="field field-sm font-mono" bind:value={o.s3_region} placeholder="eu-central-1" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Bucket
							<input class="field field-sm font-mono" bind:value={o.s3_bucket} placeholder="nullmodem-backups" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Prefix (optional)
							<input class="field field-sm font-mono" bind:value={o.s3_prefix} placeholder="bbs/" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Access key
							<input class="field field-sm font-mono" bind:value={o.s3_access_key} autocomplete="off" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Secret key {o.s3_has_secret_key ? '(saved)' : ''}
							<input class="field field-sm" type="password" bind:value={s3Secret} autocomplete="new-password" placeholder={o.s3_has_secret_key ? '••••••••' : ''} /></label>
					</div>
					<label class="mt-2 flex items-center gap-2 text-xs text-muted"><input type="checkbox" bind:checked={o.s3_path_style} /> path-style addressing (MinIO and some older services)</label>
					<p class="mt-2 text-[11px] leading-relaxed text-faint">
						AWS: <span class="font-mono">s3.amazonaws.com</span> · Hetzner: <span class="font-mono">fsn1.your-objectstorage.com</span> ·
						Infomaniak: see the S3 details of your Swiss Backup device · Wasabi: <span class="font-mono">s3.eu-central-1.wasabisys.com</span> ·
						Backblaze B2: <span class="font-mono">s3.eu-central-003.backblazeb2.com</span>. The bucket is made if it isn't there.
					</p>
				{:else if o.kind === 'webdav'}
					<div class="grid gap-2.5 sm:grid-cols-2">
						<label class="flex flex-col gap-1 text-xs text-muted sm:col-span-2">Folder URL
							<input class="field field-sm font-mono" bind:value={o.webdav_url} placeholder="https://cloud.example.org/remote.php/dav/files/me/bbs-backups" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">User
							<input class="field field-sm font-mono" bind:value={o.webdav_user} autocomplete="off" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Password {o.webdav_has_password ? '(saved)' : '(an app password, ideally)'}
							<input class="field field-sm" type="password" bind:value={davPassword} autocomplete="new-password" placeholder={o.webdav_has_password ? '••••••••' : ''} /></label>
					</div>
					<p class="mt-2 text-[11px] text-faint">Nextcloud/ownCloud: Settings → WebDAV shows the address; kDrive and Storage Boxes speak WebDAV too. The folder is made if missing.</p>
				{:else}
					<div class="grid gap-2.5 sm:grid-cols-[1fr_6rem]">
						<label class="flex flex-col gap-1 text-xs text-muted">Host
							<input class="field field-sm font-mono" bind:value={o.sftp_host} placeholder="u123456.your-storagebox.de" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Port
							<input class="field field-sm font-mono" type="number" bind:value={o.sftp_port} placeholder="23" /></label>
					</div>
					<div class="mt-2.5 grid gap-2.5 sm:grid-cols-3">
						<label class="flex flex-col gap-1 text-xs text-muted">User
							<input class="field field-sm font-mono" bind:value={o.sftp_user} autocomplete="off" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Directory
							<input class="field field-sm font-mono" bind:value={o.sftp_dir} placeholder="nullmodem-backups" /></label>
						<label class="flex flex-col gap-1 text-xs text-muted">Password {o.sftp_has_password ? '(saved)' : '(or the key below)'}
							<input class="field field-sm" type="password" bind:value={sftpPassword} autocomplete="new-password" placeholder={o.sftp_has_password ? '••••••••' : ''} /></label>
					</div>
					<div class="mt-3 text-xs text-muted">
						<div class="mb-1 flex items-center justify-between gap-2">
							<span>Login key (better than a password)</span>
							<button class="btn-secondary btn-xs" onclick={makeSSHKey}>{o.sftp_public_key ? 'New key…' : 'Make a key'}</button>
						</div>
						{#if o.sftp_public_key}
							<code class="block break-all rounded-md border border-line p-2 select-all text-ink">{o.sftp_public_key}</code>
							<span class="text-[11px] text-faint">Put this line into the server's .ssh/authorized_keys (Hetzner Storage Box: see its docs, "SSH keys").</span>
						{/if}
						{#if o.sftp_host_key}
							<p class="mt-2 text-[11px] text-faint">Server's key: <span class="font-mono">{o.sftp_host_key}</span></p>
						{/if}
					</div>
				{/if}
				{#if confirmHostKey}
					<div class="mt-3 rounded-lg border border-accent/50 bg-accent/5 p-3 text-xs">
						<p class="mb-1 text-ink">First connection: the server identifies itself with</p>
						<code class="block font-mono text-accent">{confirmHostKey}</code>
						<p class="mt-1 text-muted">Compare it with your provider's (Hetzner lists the Storage Box keys). Right?</p>
						<div class="mt-2 flex gap-2">
							<button class="btn-primary btn-xs" onclick={acceptHostKey}>Yes, it's the right server</button>
							<button class="btn-secondary btn-xs" onclick={() => (confirmHostKey = '')}>No</button>
						</div>
					</div>
				{/if}
			</div>

			<!-- On, keep, status -->
			<div class="rounded-lg border border-line p-3">
				<div class="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm">
					<span class="font-medium text-ink-strong">3. Copy</span>
					<label class="flex items-center gap-2"><input type="checkbox" bind:checked={o.enabled} disabled={!o.recipient} /> after each backup</label>
					<label class="flex items-center gap-1.5 text-xs text-muted">keep <input class="field field-sm w-16" type="number" min="1" bind:value={o.keep_daily} /> daily</label>
					<label class="flex items-center gap-1.5 text-xs text-muted">+ <input class="field field-sm w-16" type="number" min="0" bind:value={o.keep_weekly} /> weekly</label>
				</div>
				{#if o.status.last_ok || o.status.last_error}
					<p class="mt-2 text-xs {o.status.last_error ? 'text-amber-300' : 'text-muted'}">
						{#if o.status.last_error}Last try {when(o.status.last_try)}: {o.status.last_error}{:else}Last copy {when(o.status.last_ok)}: {o.status.last_name}.age · {o.status.remote} there{/if}
					</p>
				{/if}
				{#if remote}
					<p class="mt-2 text-xs text-muted">There now: {remote.length ? remote.join(', ') : 'nothing yet'}</p>
				{/if}
			</div>

			<div class="flex flex-wrap justify-end gap-2">
				<button class="btn-secondary btn-sm" disabled={busy !== null} onclick={test}>{busy === 'test' ? 'Testing…' : 'Save and test'}</button>
				<button class="btn-secondary btn-sm" disabled={busy !== null || !o.enabled} onclick={copyNow}>{busy === 'run' ? 'Copying…' : 'Copy the newest now'}</button>
				<button class="btn-primary btn-sm" disabled={busy !== null} onclick={async () => (await save()) && toast.push('Saved.', 'success')}>Save</button>
			</div>
		</div>
	{/if}
</section>
