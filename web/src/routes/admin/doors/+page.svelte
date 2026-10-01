<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listDoors,
		putDoors,
		listDoorTemplates,
		addDoorFromTemplate,
		getMRCConfig,
		putMRCConfig,
		getBBSInfo,
		getConfig,
		ApiError,
		type Door,
		type DoorTemplate,
		type MRCConfig
	} from '$lib/api';
	import Modal from '$lib/Modal.svelte';

	const DROPFILE_LABELS: Record<string, string> = {
		'door.sys': 'DOOR.SYS',
		dorinfo: 'DORINFO1.DEF',
		'doorfile.sr': 'DOORFILE.SR',
		'door32.sys': 'DOOR32.SYS'
	};

	let doors = $state<Door[]>([]);
	let formats = $state<string[]>([]);
	let doorsDir = $state('');
	let templates = $state<DoorTemplate[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	// The door being edited: its index in doors, or -1 for a new one.
	let editing = $state<number | null>(null);
	let draft = $state<Door>(emptyDoor());
	let argsText = $state('');
	let lockText = $state('');
	let programText = $state('');
	let saving = $state(false);
	let installing = $state<string | null>(null);

	function emptyDoor(): Door {
		return {
			name: '',
			kind: 'dosbox',
			min_sl: 0,
			exe: '',
			dir: '',
			args: [],
			dosbox_dir: '',
			dosbox_launch_cmd: '',
			dropfile: '',
			dropfile_in_door_dir: false,
			lock_files: [],
			stdio: false,
			ansi16: false,
			remote: { host: '', port: 513, client_user: '', server_user: '', term_type: '' },
			template: '',
			program: [],
			installed: false
		};
	}

	async function authFailed(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return true;
		}
		return false;
	}

	async function load() {
		if (!auth.token) return;
		try {
			const [res, tmpls] = await Promise.all([listDoors(auth.token), listDoorTemplates(auth.token)]);
			doors = res.doors;
			formats = res.dropfile_formats;
			doorsDir = res.doors_dir;
			templates = tmpls;
			loadError = null;
		} catch (err) {
			if (await authFailed(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load doors.';
		} finally {
			loaded = true;
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		await load();
	});

	function startEdit(i: number) {
		editing = i;
		draft = i >= 0 ? structuredClone($state.snapshot(doors[i])) : emptyDoor();
		if (draft.kind === '') draft.kind = 'native';
		draft.remote ??= { host: '', port: 513, client_user: '', server_user: '', term_type: '' };
		if (!draft.remote.port) draft.remote.port = 513;
		argsText = draft.args.join(' ');
		lockText = draft.lock_files.join('\n');
		programText = draft.program.join(' ');
	}

	async function save(list: Door[], message: string) {
		if (!auth.token) return false;
		saving = true;
		try {
			const res = await putDoors(auth.token, list);
			doors = res.doors;
			templates = await listDoorTemplates(auth.token);
			toast.push(message, 'success');
			return true;
		} catch (err) {
			if (await authFailed(err)) return false;
			toast.push(err instanceof ApiError ? err.message : 'Could not save doors.', 'error');
			return false;
		} finally {
			saving = false;
		}
	}

	async function saveDraft() {
		if (editing === null) return;
		const door: Door = {
			...$state.snapshot(draft),
			args: argsText.split(/\s+/).filter(Boolean),
			lock_files: lockText.split(/[\n,]/).map((s) => s.trim()).filter(Boolean),
			program: draft.kind === 'native' ? programText.split(/\s+/).filter(Boolean) : []
		};
		const list = $state.snapshot(doors) as Door[];
		if (editing >= 0) list[editing] = door;
		else list.push(door);
		if (await save(list, `Saved ${door.name}.`)) editing = null;
	}

	async function remove(i: number) {
		const d = doors[i];
		if (!confirm(`Remove "${d.name}" from the doors menu? Its files stay where they are.`)) return;
		const list = ($state.snapshot(doors) as Door[]).filter((_, j) => j !== i);
		await save(list, `Removed ${d.name}.`);
		if (editing === i) editing = null;
	}

	// MRC Chat's settings dialog: before installing it (mrcTemplate set)
	// or for the door already set up.
	let mrc = $state<MRCConfig | null>(null);
	let mrcTemplate = $state<DoorTemplate | null>(null);
	let mrcSaving = $state(false);

	async function openMRC(t: DoorTemplate | null) {
		if (!auth.token) return;
		try {
			if (t) {
				// Suggest this board's own addresses, as callers reach it.
				const [info, cfg] = await Promise.all([getBBSInfo(), getConfig(auth.token)]);
				const host = location.hostname;
				mrc = {
					host: 'na-multi.relaychat.net',
					port: '5001',
					ssl: true,
					bbs_name: cfg.name,
					software: 'NullModem BBS',
					website: location.origin,
					telnet: info.telnet_port ? `${host}:${info.telnet_port}` : '',
					ssh: info.ssh_port ? `${host}:${info.ssh_port}` : '',
					sysop: cfg.sysop,
					description: ''
				};
			} else {
				mrc = await getMRCConfig(auth.token);
			}
			mrcTemplate = t;
		} catch (err) {
			if (await authFailed(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not load the MRC settings.', 'error');
		}
	}

	async function saveMRC() {
		if (!auth.token || !mrc) return;
		const settings = $state.snapshot(mrc) as MRCConfig;
		if (mrcTemplate) {
			const t = mrcTemplate;
			mrc = null;
			mrcTemplate = null;
			await addTemplate(t, settings);
			return;
		}
		mrcSaving = true;
		try {
			await putMRCConfig(auth.token, settings);
			mrc = null;
			toast.push('Saved. The chat connection restarts with the new settings.', 'success');
		} catch (err) {
			if (await authFailed(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not save the MRC settings.', 'error');
		} finally {
			mrcSaving = false;
		}
	}

	async function addTemplate(t: DoorTemplate, mrcSettings?: MRCConfig) {
		if (!auth.token) return;
		if (t.mrc && !mrcSettings && !t.installed) {
			await openMRC(t);
			return;
		}
		installing = t.id;
		try {
			const res = await addDoorFromTemplate(auth.token, t.id, mrcSettings);
			doors = res.doors;
			templates = await listDoorTemplates(auth.token);
			toast.push(
				t.downloadable && !t.installed ? `${t.name} installed and added.` : `${t.name} added.`,
				'success'
			);
		} catch (err) {
			if (await authFailed(err)) return;
			toast.push(err instanceof ApiError ? err.message : `Could not add ${t.name}.`, 'error');
		} finally {
			installing = null;
		}
	}

	function doorDir(d: Door): string {
		if (d.kind === 'rlogin') return `${d.remote.host}:${d.remote.port || 513}`;
		return d.kind === 'dosbox' ? d.dosbox_dir : d.dir;
	}

	function dropfileLabel(d: Door): string {
		if (d.dropfile) return DROPFILE_LABELS[d.dropfile] ?? d.dropfile;
		return d.kind === 'dosbox' ? 'DOOR.SYS' : 'DOOR32.SYS';
	}
</script>

<div class="mb-6 flex items-start justify-between gap-4">
	<div>
		<h1 class="page-title">Doors</h1>
		<p class="page-subtitle max-w-2xl leading-relaxed">
			Games and programs callers start from the doors menu. Changes apply the next time someone
			opens that menu — no restart needed.
		</p>
	</div>
	{#if editing === null}
		<button class="btn-primary shrink-0" onclick={() => startEdit(-1)}>+ New Door</button>
	{/if}
</div>

{#snippet editor()}
	<form
		class="card mb-4 flex flex-col gap-4"
		onsubmit={(e) => {
			e.preventDefault();
			saveDraft();
		}}
	>
		<h2 class="card-label">{editing === -1 ? 'New door' : `Edit ${doors[editing ?? 0]?.name}`}</h2>
		<div class="grid gap-3 sm:grid-cols-[1fr_10rem_7rem]">
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">Name (as shown in the menu)</span>
				<input class="field field-sm" bind:value={draft.name} required />
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">Kind</span>
				<select class="field field-sm" bind:value={draft.kind}>
					<option value="dosbox">DOS (DOSBox-X)</option>
					<option value="native">Native Linux</option>
					<option value="rlogin">Remote (RLogin)</option>
				</select>
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">Min. SL</span>
				<input class="field field-sm" type="number" min="0" max="255" bind:value={draft.min_sl} />
			</label>
		</div>

		{#if draft.kind === 'dosbox'}
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">Door directory (mounted as C:)</span>
				<input
					class="field field-sm font-mono"
					bind:value={draft.dosbox_dir}
					placeholder="{doorsDir}/mydoor"
					required
				/>
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">Launch command (one DOS command per line)</span>
				<textarea
					class="field field-sm h-20 resize-y font-mono"
					bind:value={draft.dosbox_launch_cmd}
					placeholder="GAME.EXE /P{'{dropfile_dir}'}"
					required
				></textarea>
				<span class="text-xs leading-relaxed text-faint">
					Placeholders: <code class="font-mono text-muted">{'{dropfile_dir}'}</code> (D:\),
					<code class="font-mono text-muted">{'{dropfile}'}</code> (e.g. D:\DOOR.SYS),
					<code class="font-mono text-muted">{'{node}'}</code> (node number). FOSSIL is built in — no BNU/X00
					needed.
				</span>
			</label>
		{:else if draft.kind === 'rlogin'}
			<p class="text-xs leading-relaxed text-faint">
				The door runs on another system -- a door network like DoorParty, or another BBS -- and is
				reached over RLogin. The network tells you its host and what to send as the two user names
				(often a system tag before the caller's handle, and your system's password).
			</p>
			<div class="grid gap-3 sm:grid-cols-[1fr_7rem]">
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">Host</span>
					<input class="field field-sm font-mono" bind:value={draft.remote.host} placeholder="doors.example.net" required />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">Port</span>
					<input class="field field-sm" type="number" min="1" max="65535" bind:value={draft.remote.port} />
				</label>
			</div>
			<div class="grid gap-3 sm:grid-cols-3">
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">Client user name</span>
					<input class="field field-sm font-mono" bind:value={draft.remote.client_user} placeholder={'[TAG]{handle}'} required />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">Server user name</span>
					<input class="field field-sm font-mono" bind:value={draft.remote.server_user} placeholder="system password" />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">Terminal type</span>
					<input class="field field-sm font-mono" bind:value={draft.remote.term_type} placeholder="ansi-bbs/115200" />
				</label>
			</div>
			<span class="text-xs leading-relaxed text-faint">
				Placeholders: <code class="font-mono text-muted">{'{handle}'}</code>,
				<code class="font-mono text-muted">{'{realname}'}</code>,
				<code class="font-mono text-muted">{'{node}'}</code>,
				<code class="font-mono text-muted">{'{userid}'}</code>.
			</span>
		{:else}
			<div class="grid gap-3 sm:grid-cols-2">
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">Executable</span>
					<input class="field field-sm font-mono" bind:value={draft.exe} required />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">Working directory</span>
					<input class="field field-sm font-mono" bind:value={draft.dir} required />
				</label>
			</div>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">Arguments</span>
				<input class="field field-sm font-mono" bind:value={argsText} placeholder="-dropfile {'{dropfile}'}" />
				<span class="text-xs leading-relaxed text-faint">
					Placeholders: <code class="font-mono text-muted">{'{dropfile}'}</code> (path of DOOR32.SYS),
					<code class="font-mono text-muted">{'{dropfile_dir}'}</code>,
					<code class="font-mono text-muted">{'{node}'}</code>,
					<code class="font-mono text-muted">{'{ip}'}</code> (the caller's IP). Without any, Usurper's
					<code class="font-mono text-muted">/P&lt;dir&gt;/</code> is appended.
				</span>
			</label>
			<label class="flex cursor-pointer items-start gap-2.5 text-[13px]">
				<input type="checkbox" class="check mt-0.5" bind:checked={draft.stdio} />
				<span>
					<span class="text-ink">Talk over standard I/O</span>
					<span class="block text-xs text-faint">
						For doors that use stdin/stdout like under Synchronet (Usurper Reborn) instead of the
						DOOR32.SYS socket.
					</span>
				</span>
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">Background program (optional)</span>
				<input class="field field-sm font-mono" bind:value={programText} placeholder="umrc-bridge" />
				<span class="text-xs leading-relaxed text-faint">
					Kept running in the working directory as long as the door is set up, restarted if it
					exits — for doors that need a connection of their own (MRC Chat's umrc-bridge). Shown
					under Services.
				</span>
			</label>
		{/if}

		{#if draft.kind !== 'rlogin'}
		<div class="grid gap-3 sm:grid-cols-2">
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">Drop file</span>
				<select class="field field-sm" bind:value={draft.dropfile}>
					<option value="">Default ({draft.kind === 'dosbox' ? 'DOOR.SYS' : 'DOOR32.SYS'})</option>
					{#each formats as f (f)}
						<option value={f}>{DROPFILE_LABELS[f] ?? f}</option>
					{/each}
				</select>
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">Lock files to clear (one per line, relative)</span>
				<textarea
					class="field field-sm h-[2.35rem] resize-y font-mono"
					bind:value={lockText}
					placeholder="OONODE.DAT"
				></textarea>
			</label>
		</div>
		<label class="flex cursor-pointer items-start gap-2.5 text-[13px]">
			<input type="checkbox" class="check mt-0.5" bind:checked={draft.ansi16} />
			<span>
				<span class="text-ink">Reduce colours to the 16 ANSI colours</span>
				<span class="block text-xs text-faint">
					For doors drawn in 256 or true colours (Immortal Barons): classic BBS terminals like
					SyncTERM or MuffinTerm show those as stripes.
				</span>
			</span>
		</label>
		<label class="flex cursor-pointer items-start gap-2.5 text-[13px]">
			<input type="checkbox" class="check mt-0.5" bind:checked={draft.dropfile_in_door_dir} />
			<span>
				<span class="text-ink">Also write the drop file into the door's directory</span>
				<span class="block text-xs text-faint">
					For doors that look for it there instead of taking a path (LORD, TradeWars).
				</span>
			</span>
		</label>
		{/if}

		<div class="flex justify-end gap-2.5">
			<button type="button" class="btn-secondary btn-sm" onclick={() => (editing = null)}>Cancel</button>
			<button type="submit" class="btn-primary btn-sm" disabled={saving}>
				{saving ? 'Saving…' : 'Save door'}
			</button>
		</div>
	</form>
{/snippet}

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-muted">Loading…</p>
{:else}
	{#if editing === -1}
		{@render editor()}
	{/if}

	<h2 class="card-label mb-2 px-1">Configured · {doors.length}</h2>
	{#if doors.length === 0}
		<p class="mb-8 px-1 text-sm text-muted">No doors yet — add one below from a template.</p>
	{:else}
		<div class="mb-10 flex flex-col">
			{#each doors as d, i (d.name + i)}
				{#if editing === i}
					<div class="py-2">{@render editor()}</div>
				{:else}
					<div class="list-row gap-4 py-3">
						<div class="min-w-0 flex-1">
							<div class="flex flex-wrap items-center gap-2">
								<span class="text-[13.5px] font-medium text-slate-100">{d.name}</span>
								<span
									class="rounded-md border border-line-strong px-1.5 py-0.5 font-mono text-[10px] text-muted"
								>
									{d.kind === 'dosbox' ? 'DOS' : d.kind === 'rlogin' ? 'REMOTE' : 'NATIVE'}
								</span>
								{#if !d.installed && d.kind !== 'rlogin'}
									<span
										class="rounded-md border border-amber-500/40 px-1.5 py-0.5 font-mono text-[10px] text-amber-400"
										title="The door's directory is missing or empty"
									>
										NOT INSTALLED
									</span>
								{/if}
							</div>
							<div class="mt-1 truncate font-mono text-[11px] text-faint">
								{doorDir(d) || '—'}{d.kind === 'rlogin' ? '' : ` · ${dropfileLabel(d)}`}{d.dropfile_in_door_dir ? ' (+door dir)' : ''}{d.stdio ? ' · stdio' : ''}{d.ansi16 ? ' · 16 colours' : ''}{d.program.length ? ` · runs ${d.program[0]}` : ''} · SL {d.min_sl}+
							</div>
						</div>
						<div class="flex shrink-0 gap-1.5">
							{#if d.template === 'umrc'}
								<button class="btn-secondary btn-xs" onclick={() => openMRC(null)}>Chat settings</button>
							{/if}
							<button class="btn-secondary btn-xs" onclick={() => startEdit(i)} disabled={editing !== null}>
								Edit
							</button>
							<button class="btn-danger btn-xs" onclick={() => remove(i)} disabled={saving}>Remove</button>
						</div>
					</div>
				{/if}
			{/each}
		</div>
	{/if}

	<h2 class="card-label mb-1 px-1">Templates</h2>
	<p class="mb-3 px-1 text-[13px] text-muted">
		Ready-made setups for well-known doors. Open-source ones are downloaded, set up and installed into
		<span class="font-mono text-ink-soft">{doorsDir}</span>; the others you unpack there yourself.
	</p>
	<div class="grid gap-3 md:grid-cols-2">
		{#each templates as t (t.id)}
			<div class="card flex flex-col gap-2.5 p-5">
				<div class="flex items-start justify-between gap-3">
					<div class="min-w-0">
						<div class="text-[14px] font-semibold text-ink-strong">{t.name}</div>
						<div class="mt-0.5 font-mono text-[10.5px] text-faint">
							{t.license} · {t.kind === 'native' ? 'Linux' : 'DOS'} · {t.dir}/
						</div>
					</div>
					{#if t.configured}
						<span class="shrink-0 font-mono text-[10.5px] text-accent">✓ SET UP</span>
					{:else}
						<button
							class="{t.downloadable && !t.installed ? 'btn-primary' : 'btn-secondary'} btn-xs shrink-0"
							disabled={installing !== null}
							onclick={() => addTemplate(t)}
						>
							{#if installing === t.id}
								{t.downloadable && !t.installed ? 'Installing…' : 'Adding…'}
							{:else}
								{t.downloadable && !t.installed ? 'Install' : 'Add'}
							{/if}
						</button>
					{/if}
				</div>
				<p class="text-[12.5px] leading-relaxed text-muted">{t.description}</p>
				<div class="font-mono text-[11px] text-faint">
					{#if t.kind === 'native'}
						{[t.exe, ...(t.args ?? [])].join(' ')} · {t.stdio ? 'stdio' : 'DOOR32.SYS'}{t.ansi16 ? ' · 16 colours' : ''}
					{:else}
						{(t.dosbox_launch_cmd ?? '').split('\n').join(' ⏎ ')} ·
						{DROPFILE_LABELS[t.dropfile] ?? t.dropfile}
					{/if}
				</div>
				{#if t.setup}
					<p class="border-l-2 border-line-strong pl-2.5 text-xs leading-relaxed text-faint">{t.setup}</p>
				{/if}
				{#if t.source_url}
					<a href={t.source_url} target="_blank" rel="noopener" class="text-xs text-muted hover:text-accent">
						{t.source_url.replace('https://', '')} ↗
					</a>
				{/if}
			</div>
		{/each}
	</div>
{/if}

{#if mrc}
	<Modal
		title={mrcTemplate ? 'Install MRC Chat' : 'MRC Chat settings'}
		onclose={() => {
			mrc = null;
			mrcTemplate = null;
		}}
	>
		<form
			class="flex flex-col gap-3"
			onsubmit={(e) => {
				e.preventDefault();
				saveMRC();
			}}
		>
			<p class="text-[13px] leading-relaxed text-muted">
				What the Multi-Relay Chat network shows about your board. Pipe colour codes (<code
					class="font-mono">|01</code
				>–<code class="font-mono">|23</code>) are allowed.
			</p>
			<div class="grid gap-3 sm:grid-cols-2">
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">BBS name</span>
					<input class="field field-sm" bind:value={mrc.bbs_name} maxlength="139" required />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">Sysop</span>
					<input class="field field-sm" bind:value={mrc.sysop} maxlength="139" required />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">Website</span>
					<input class="field field-sm font-mono" bind:value={mrc.website} maxlength="139" placeholder="https://" />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">Software</span>
					<input class="field field-sm" bind:value={mrc.software} maxlength="139" />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">Telnet address</span>
					<input class="field field-sm font-mono" bind:value={mrc.telnet} maxlength="139" placeholder="host:port" />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">SSH address</span>
					<input class="field field-sm font-mono" bind:value={mrc.ssh} maxlength="139" placeholder="host:port" />
				</label>
			</div>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">Short description</span>
				<input class="field field-sm" bind:value={mrc.description} maxlength="139" />
			</label>
			<details class="text-[13px]">
				<summary class="cursor-pointer text-xs text-muted">Chat host</summary>
				<div class="mt-3 grid gap-3 sm:grid-cols-[1fr_6rem]">
					<label class="flex flex-col gap-1.5">
						<span class="text-xs text-muted">MRC host</span>
						<input class="field field-sm font-mono" bind:value={mrc.host} maxlength="79" required />
					</label>
					<label class="flex flex-col gap-1.5">
						<span class="text-xs text-muted">Port</span>
						<input class="field field-sm font-mono" bind:value={mrc.port} maxlength="5" required />
					</label>
				</div>
				<label class="mt-3 flex cursor-pointer items-center gap-2.5">
					<input type="checkbox" class="check" bind:checked={mrc.ssl} />
					<span class="text-ink">SSL (port 5001; plain is 5000)</span>
				</label>
			</details>
			{#if mrcTemplate}
				<p class="text-xs leading-relaxed text-faint">
					Installing starts the chat connection (umrc-bridge) right away. Only one connection per
					board is allowed, so don't run MRC for this board anywhere else.
				</p>
			{/if}
			<div class="flex justify-end gap-2.5">
				<button
					type="button"
					class="btn-secondary btn-sm"
					onclick={() => {
						mrc = null;
						mrcTemplate = null;
					}}>Cancel</button
				>
				<button type="submit" class="btn-primary btn-sm" disabled={mrcSaving}>
					{mrcTemplate ? 'Install' : mrcSaving ? 'Saving…' : 'Save'}
				</button>
			</div>
		</form>
	</Modal>
{/if}
