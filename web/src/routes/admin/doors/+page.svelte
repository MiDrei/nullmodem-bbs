<script lang="ts">
	import { t, i18n } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listDoors,
		putDoors,
		runDoorDaily,
		applyDoorTemplateBulletins,
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
			daily: '',
			daily_at: '',
			bulletins: [],
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
			loadError = err instanceof ApiError ? err.message : t('admin.doors.could_not_load_doors');
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
			toast.push(err instanceof ApiError ? err.message : t('admin.doors.could_not_save_doors'), 'error');
			return false;
		} finally {
			saving = false;
		}
	}

	// The template's bulletins for an installed door: saved at once
	// (the game is told to write them, too).
	async function applyTemplateBulletins() {
		if (!auth.token || editing === null || editing < 0) return;
		const name = doors[editing].name;
		try {
			const res = await applyDoorTemplateBulletins(auth.token, name);
			doors = res.doors;
			const d = res.doors.find((x) => x.name === name);
			if (d) {
				draft.bulletins = d.bulletins ?? [];
				if (!draft.daily) draft.daily = d.daily;
			}
			toast.push(t('admin.doors.name_writes_its_bulletins_from', { NAME: name }), 'success');
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : t('admin.doors.could_not_do_that'), 'error');
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
		if (await save(list, t('admin.doors.saved_name', { NAME: door.name }))) editing = null;
	}

	async function remove(i: number) {
		const d = doors[i];
		if (!confirm(t('admin.doors.remove_name_from_the_doors', { NAME: d.name }))) return;
		const list = ($state.snapshot(doors) as Door[]).filter((_, j) => j !== i);
		await save(list, t('admin.doors.removed_name', { NAME: d.name }));
		if (editing === i) editing = null;
	}

	// MRC Chat's settings dialog: before installing it (mrcTemplate set)
	// or for the door already set up.
	let mrc = $state<MRCConfig | null>(null);
	let mrcTemplate = $state<DoorTemplate | null>(null);
	let mrcSaving = $state(false);

	async function openMRC(tpl: DoorTemplate | null) {
		if (!auth.token) return;
		try {
			if (tpl) {
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
			mrcTemplate = tpl;
		} catch (err) {
			if (await authFailed(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.doors.could_not_load_the_mrc'), 'error');
		}
	}

	async function saveMRC() {
		if (!auth.token || !mrc) return;
		const settings = $state.snapshot(mrc) as MRCConfig;
		if (mrcTemplate) {
			const tpl = mrcTemplate;
			mrc = null;
			mrcTemplate = null;
			await addTemplate(tpl, settings);
			return;
		}
		mrcSaving = true;
		try {
			await putMRCConfig(auth.token, settings);
			mrc = null;
			toast.push(t('admin.doors.saved_the_chat_connection_restarts'), 'success');
		} catch (err) {
			if (await authFailed(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.doors.could_not_save_the_mrc'), 'error');
		} finally {
			mrcSaving = false;
		}
	}

	async function addTemplate(tpl: DoorTemplate, mrcSettings?: MRCConfig) {
		if (!auth.token) return;
		if (tpl.mrc && !mrcSettings && !tpl.installed) {
			await openMRC(tpl);
			return;
		}
		installing = tpl.id;
		try {
			const res = await addDoorFromTemplate(auth.token, tpl.id, mrcSettings);
			doors = res.doors;
			templates = await listDoorTemplates(auth.token);
			toast.push(
				tpl.downloadable && !tpl.installed ? t('admin.doors.name_installed_and_added', { NAME: tpl.name }) : t('admin.doors.name_added', { NAME: tpl.name }),
				'success'
			);
		} catch (err) {
			if (await authFailed(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.doors.could_not_add_name', { NAME: tpl.name }), 'error');
		} finally {
			installing = null;
		}
	}

	async function runDaily(d: Door) {
		if (!auth.token) return;
		try {
			await runDoorDaily(auth.token, d.name);
			toast.push(t('admin.doors.name_s_daily_maintenance_starts', { NAME: d.name }), 'success');
		} catch (err) {
			if (await authFailed(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.doors.could_not_start_it'), 'error');
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
		<h1 class="page-title">{t('common.doors')}</h1>
		<p class="page-subtitle max-w-2xl leading-relaxed">
			{t('admin.doors.games_and_programs_callers_start')}
		</p>
	</div>
	{#if editing === null}
		<button class="btn-primary shrink-0" onclick={() => startEdit(-1)}>{t('admin.doors.new_door')}</button>
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
		<h2 class="card-label">{editing === -1 ? t('admin.doors.new_door_2') : t('admin.doors.edit_editing', { EDITING: doors[editing ?? 0]?.name })}</h2>
		<div class="grid gap-3 sm:grid-cols-[1fr_10rem_7rem]">
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.doors.name_as_shown_in_the')}</span>
				<input class="field field-sm" bind:value={draft.name} required />
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.common.kind')}</span>
				<select class="field field-sm" bind:value={draft.kind}>
					<option value="dosbox">{t('admin.doors.dos_dosbox_x')}</option>
					<option value="native">{t('admin.doors.native_linux')}</option>
					<option value="rlogin">{t('admin.doors.remote_rlogin')}</option>
				</select>
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.doors.min_sl')}</span>
				<input class="field field-sm" type="number" min="0" max="255" bind:value={draft.min_sl} />
			</label>
		</div>

		{#if draft.kind === 'dosbox'}
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.doors.door_directory_mounted_as_c')}</span>
				<input
					class="field field-sm font-mono"
					bind:value={draft.dosbox_dir}
					placeholder="{doorsDir}/mydoor"
					required
				/>
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.doors.launch_command_one_dos_command')}</span>
				<textarea
					class="field field-sm h-20 resize-y font-mono"
					bind:value={draft.dosbox_launch_cmd}
					placeholder="GAME.EXE /P{'{dropfile_dir}'}"
					required
				></textarea>
				<span class="text-xs leading-relaxed text-faint">
					{t('admin.doors.placeholders')} <code class="font-mono text-muted">{'{dropfile_dir}'}</code> (D:\),
					<code class="font-mono text-muted">{'{dropfile}'}</code> {t('admin.doors.e_g_d_door_sys')}
					<code class="font-mono text-muted">{'{node}'}</code> {t('admin.doors.node_number_fossil_is_built')}
				</span>
			</label>
		{:else if draft.kind === 'rlogin'}
			<p class="text-xs leading-relaxed text-faint">
				{t('admin.doors.the_door_runs_on_another')}
			</p>
			<div class="grid gap-3 sm:grid-cols-[1fr_7rem]">
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('admin.common.host')}</span>
					<input class="field field-sm font-mono" bind:value={draft.remote.host} placeholder="doors.example.net" required />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('admin.common.port')}</span>
					<input class="field field-sm" type="number" min="1" max="65535" bind:value={draft.remote.port} />
				</label>
			</div>
			<div class="grid gap-3 sm:grid-cols-3">
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('admin.doors.client_user_name')}</span>
					<input class="field field-sm font-mono" bind:value={draft.remote.client_user} placeholder={'[TAG]{handle}'} required />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('admin.doors.server_user_name')}</span>
					<input class="field field-sm font-mono" bind:value={draft.remote.server_user} placeholder={t('admin.doors.system_password')} />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('admin.doors.terminal_type')}</span>
					<input class="field field-sm font-mono" bind:value={draft.remote.term_type} placeholder="ansi-bbs/115200" />
				</label>
			</div>
			<span class="text-xs leading-relaxed text-faint">
				{t('admin.doors.placeholders')} <code class="font-mono text-muted">{'{handle}'}</code>,
				<code class="font-mono text-muted">{'{realname}'}</code>,
				<code class="font-mono text-muted">{'{node}'}</code>,
				<code class="font-mono text-muted">{'{userid}'}</code>.
			</span>
		{:else}
			<div class="grid gap-3 sm:grid-cols-2">
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('admin.doors.executable')}</span>
					<input class="field field-sm font-mono" bind:value={draft.exe} required />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('admin.doors.working_directory')}</span>
					<input class="field field-sm font-mono" bind:value={draft.dir} required />
				</label>
			</div>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.doors.arguments')}</span>
				<input class="field field-sm font-mono" bind:value={argsText} placeholder="-dropfile {'{dropfile}'}" />
				<span class="text-xs leading-relaxed text-faint">
					{t('admin.doors.placeholders')} <code class="font-mono text-muted">{'{dropfile}'}</code> {t('admin.doors.path_of_door32_sys')}
					<code class="font-mono text-muted">{'{dropfile_dir}'}</code>,
					<code class="font-mono text-muted">{'{node}'}</code>,
					<code class="font-mono text-muted">{'{ip}'}</code> {t('admin.doors.the_caller_s_ip_without')}
					<code class="font-mono text-muted">/P&lt;dir&gt;/</code> {t('admin.doors.is_appended')}
				</span>
			</label>
			<label class="flex cursor-pointer items-start gap-2.5 text-[13px]">
				<input type="checkbox" class="check mt-0.5" bind:checked={draft.stdio} />
				<span>
					<span class="text-ink">{t('admin.doors.talk_over_standard_i_o')}</span>
					<span class="block text-xs text-faint">
						{t('admin.doors.for_doors_that_use_stdin')}
					</span>
				</span>
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.doors.background_program_optional')}</span>
				<input class="field field-sm font-mono" bind:value={programText} placeholder="umrc-bridge" />
				<span class="text-xs leading-relaxed text-faint">
					{t('admin.doors.kept_running_in_the_working')}
				</span>
			</label>
		{/if}

		{#if draft.kind !== 'rlogin'}
		<div class="grid gap-3 sm:grid-cols-[1fr_7rem]">
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.doors.daily_maintenance_optional')}</span>
				{#if draft.kind === 'dosbox'}
					<textarea class="field field-sm h-[2.35rem] resize-y font-mono" bind:value={draft.daily} placeholder="TWMAINT.EXE"></textarea>
				{:else}
					<input class="field field-sm font-mono" bind:value={draft.daily} placeholder={t('admin.doors.maint_daily')} />
				{/if}
				<span class="text-xs leading-relaxed text-faint">
					{t('admin.doors.run_once_a_day_without', { V: draft.kind === 'dosbox' ? t('admin.doors.dos_commands_from_c_one') : t('admin.doors.a_command_line_in_the') })}
				</span>
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.doors.at')}</span>
				<input class="field field-sm font-mono" bind:value={draft.daily_at} placeholder="00:05" />
			</label>
		</div>
		<div class="flex flex-col gap-1.5">
			<span class="text-xs text-muted">{t('admin.doors.bulletins_optional')}</span>
			{#each draft.bulletins ?? [] as b, i (i)}
				<div class="grid grid-cols-[1fr_1fr_auto_auto] items-center gap-2">
					<input class="field field-sm" bind:value={b.title} placeholder={t('admin.doors.title_scoreboard')} />
					<input class="field field-sm font-mono" bind:value={b.file} placeholder="data/bull/scores.ans" />
					<label class="flex items-center gap-1 text-xs text-muted" title={t('admin.doors.also_on_the_public_front')}><input type="checkbox" bind:checked={b.public} /> {t('admin.doors.public')}</label>
					<button type="button" class="btn-secondary btn-xs" onclick={() => draft && (draft.bulletins = (draft.bulletins ?? []).filter((_, k) => k !== i))}>✕</button>
				</div>
			{/each}
			<div class="flex flex-wrap items-center gap-2">
				<button type="button" class="btn-secondary btn-xs" onclick={() => draft && (draft.bulletins = [...(draft.bulletins ?? []), { title: '', file: '', public: false }])}>{t('admin.doors.bulletin')}</button>
				{#if !(draft.bulletins ?? []).length && draft.template_bulletins?.length && editing !== null && editing >= 0}
					<button type="button" class="btn-primary btn-xs" onclick={applyTemplateBulletins}>{t('admin.doors.use_the_template_s_v', { V: draft.template_bulletins.map((b) => b.title).join(', ') })}</button>
				{/if}
			</div>
			<span class="text-xs leading-relaxed text-faint">
				{t('admin.doors.files_the_door_writes_for')}
			</span>
		</div>
		<div class="grid gap-3 sm:grid-cols-2">
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.doors.drop_file')}</span>
				<select class="field field-sm" bind:value={draft.dropfile}>
					<option value="">{t('admin.doors.default_v', { V: draft.kind === 'dosbox' ? 'DOOR.SYS' : 'DOOR32.SYS' })}</option>
					{#each formats as f (f)}
						<option value={f}>{DROPFILE_LABELS[f] ?? f}</option>
					{/each}
				</select>
			</label>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.doors.lock_files_to_clear_one')}</span>
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
				<span class="text-ink">{t('admin.doors.reduce_colours_to_the_16')}</span>
				<span class="block text-xs text-faint">
					{t('admin.doors.for_doors_drawn_in_256')}
				</span>
			</span>
		</label>
		<label class="flex cursor-pointer items-start gap-2.5 text-[13px]">
			<input type="checkbox" class="check mt-0.5" bind:checked={draft.dropfile_in_door_dir} />
			<span>
				<span class="text-ink">{t('admin.doors.also_write_the_drop_file')}</span>
				<span class="block text-xs text-faint">
					{t('admin.doors.for_doors_that_look_for')}
				</span>
			</span>
		</label>
		{/if}

		<div class="flex justify-end gap-2.5">
			<button type="button" class="btn-secondary btn-sm" onclick={() => (editing = null)}>{t('web.common.cancel')}</button>
			<button type="submit" class="btn-primary btn-sm" disabled={saving}>
				{saving ? t('web.common.saving') : t('admin.doors.save_door')}
			</button>
		</div>
	</form>
{/snippet}

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-muted">{t('web.common.loading')}</p>
{:else}
	{#if editing === -1}
		{@render editor()}
	{/if}

	<h2 class="card-label mb-2 px-1">{t('admin.doors.configured_length', { LENGTH: doors.length })}</h2>
	{#if doors.length === 0}
		<p class="mb-8 px-1 text-sm text-muted">{t('admin.doors.no_doors_yet_add_one')}</p>
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
									{d.kind === 'dosbox' ? 'DOS' : d.kind === 'rlogin' ? t('admin.doors.remote') : t('admin.doors.native')}
								</span>
								{#if !d.installed && d.kind !== 'rlogin'}
									<span
										class="rounded-md border border-amber-500/40 px-1.5 py-0.5 font-mono text-[10px] text-amber-400"
										title={t('admin.doors.the_door_s_directory_is')}
									>
										{t('admin.doors.not_installed')}
									</span>
								{/if}
							</div>
							<div class="mt-1 truncate font-mono text-[11px] text-faint">
								{t('admin.doors.v_v2_v3_v4_v5', { V: doorDir(d) || '—', V2: d.kind === 'rlogin' ? '' : ` · ${dropfileLabel(d)}`, V3: d.daily ? t('admin.doors.daily_v', { V: d.daily_at || '00:05' }) : '', V4: d.dropfile_in_door_dir ? t('admin.doors.door_dir') : '', V5: d.stdio ? t('admin.doors.stdio') : '', V6: d.ansi16 ? t('admin.doors.16_colours') : '', V7: d.program.length ? t('admin.doors.runs_v', { V: d.program[0] }) : '', MIN_SL: d.min_sl })}
							</div>
							{#if d.daily_state}
								<div class="mt-0.5 truncate text-[11px] {d.daily_state.ok ? 'text-emerald-500' : 'text-red-400'}" title={d.daily_state.detail}>
									{t('admin.doors.daily_maintenance_v_tolocalestring_v2', { V: d.daily_state.ok ? t('admin.doors.daily_ran') : t('admin.doors.daily_failed'), TOLOCALESTRING: new Date(d.daily_state.last_at).toLocaleString(i18n.locale), V2: d.daily_state.ok ? '' : ` -- ${d.daily_state.detail.split('\n')[0]}` })}
								</div>
							{/if}
						</div>
						<div class="flex shrink-0 gap-1.5">
							{#if d.template === 'umrc'}
								<button class="btn-secondary btn-xs" onclick={() => openMRC(null)}>{t('admin.doors.chat_settings')}</button>
							{/if}
							{#if d.daily && d.kind !== 'rlogin'}
								<button class="btn-secondary btn-xs" onclick={() => runDaily(d)}>{t('admin.doors.run_maintenance')}</button>
							{/if}
							<button class="btn-secondary btn-xs" onclick={() => startEdit(i)} disabled={editing !== null}>
								{t('web.common.edit')}
							</button>
							<button class="btn-danger btn-xs" onclick={() => remove(i)} disabled={saving}>{t('web.common.remove')}</button>
						</div>
					</div>
				{/if}
			{/each}
		</div>
	{/if}

	<h2 class="card-label mb-1 px-1">{t('admin.doors.templates')}</h2>
	<p class="mb-3 px-1 text-[13px] text-muted">
		{t('admin.doors.ready_made_setups_for_well')}
		<span class="font-mono text-ink-soft">{doorsDir}</span>{t('admin.doors.the_others_you_unpack_there')}
	</p>
	<div class="grid gap-3 md:grid-cols-2">
		{#each templates as tpl (tpl.id)}
			<div class="card flex flex-col gap-2.5 p-5">
				<div class="flex items-start justify-between gap-3">
					<div class="min-w-0">
						<div class="text-[14px] font-semibold text-ink-strong">{tpl.name}</div>
						<div class="mt-0.5 font-mono text-[10.5px] text-faint">
							{tpl.license} · {tpl.kind === 'native' ? t('admin.common.linux') : 'DOS'} · {tpl.dir}/
						</div>
					</div>
					{#if tpl.configured}
						<span class="shrink-0 font-mono text-[10.5px] text-accent">{t('admin.doors.set_up')}</span>
					{:else}
						<button
							class="{tpl.downloadable && !tpl.installed ? 'btn-primary' : 'btn-secondary'} btn-xs shrink-0"
							disabled={installing !== null}
							onclick={() => addTemplate(tpl)}
						>
							{#if installing === tpl.id}
								{tpl.downloadable && !tpl.installed ? t('admin.doors.installing') : t('admin.doors.adding')}
							{:else}
								{tpl.downloadable && !tpl.installed ? t('admin.doors.install') : t('admin.common.add')}
							{/if}
						</button>
					{/if}
				</div>
				<p class="text-[12.5px] leading-relaxed text-muted">{tpl.description}</p>
				<div class="font-mono text-[11px] text-faint">
					{#if tpl.kind === 'native'}
						{[tpl.exe, ...(tpl.args ?? [])].join(' ')} · {tpl.stdio ? 'stdio' : 'DOOR32.SYS'}{tpl.ansi16 ? t('admin.doors.16_colours') : ''}
					{:else}
						{(tpl.dosbox_launch_cmd ?? '').split('\n').join(' ⏎ ')} ·
						{DROPFILE_LABELS[tpl.dropfile] ?? tpl.dropfile}
					{/if}
				</div>
				{#if tpl.setup}
					<p class="border-l-2 border-line-strong pl-2.5 text-xs leading-relaxed text-faint">{tpl.setup}</p>
				{/if}
				{#if tpl.source_url}
					<a href={tpl.source_url} target="_blank" rel="noopener" class="text-xs text-muted hover:text-accent">
						{tpl.source_url.replace('https://', '')} ↗
					</a>
				{/if}
			</div>
		{/each}
	</div>
{/if}

{#if mrc}
	<Modal
		title={mrcTemplate ? t('admin.doors.install_mrc_chat') : t('admin.doors.mrc_chat_settings')}
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
				{t('admin.doors.what_the_multi_relay_chat')}<code
					class="font-mono">|01</code
				>–<code class="font-mono">|23</code>{t('admin.doors.are_allowed')}
			</p>
			<div class="grid gap-3 sm:grid-cols-2">
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('admin.common.bbs_name')}</span>
					<input class="field field-sm" bind:value={mrc.bbs_name} maxlength="139" required />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('common.sysop')}</span>
					<input class="field field-sm" bind:value={mrc.sysop} maxlength="139" required />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('admin.doors.website')}</span>
					<input class="field field-sm font-mono" bind:value={mrc.website} maxlength="139" placeholder="https://" />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('common.software')}</span>
					<input class="field field-sm" bind:value={mrc.software} maxlength="139" />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('admin.doors.telnet_address')}</span>
					<input class="field field-sm font-mono" bind:value={mrc.telnet} maxlength="139" placeholder="host:port" />
				</label>
				<label class="flex flex-col gap-1.5">
					<span class="text-xs text-muted">{t('admin.doors.ssh_address')}</span>
					<input class="field field-sm font-mono" bind:value={mrc.ssh} maxlength="139" placeholder="host:port" />
				</label>
			</div>
			<label class="flex flex-col gap-1.5">
				<span class="text-xs text-muted">{t('admin.doors.short_description')}</span>
				<input class="field field-sm" bind:value={mrc.description} maxlength="139" />
			</label>
			<details class="text-[13px]">
				<summary class="cursor-pointer text-xs text-muted">{t('admin.doors.chat_host')}</summary>
				<div class="mt-3 grid gap-3 sm:grid-cols-[1fr_6rem]">
					<label class="flex flex-col gap-1.5">
						<span class="text-xs text-muted">{t('admin.doors.mrc_host')}</span>
						<input class="field field-sm font-mono" bind:value={mrc.host} maxlength="79" required />
					</label>
					<label class="flex flex-col gap-1.5">
						<span class="text-xs text-muted">{t('admin.common.port')}</span>
						<input class="field field-sm font-mono" bind:value={mrc.port} maxlength="5" required />
					</label>
				</div>
				<label class="mt-3 flex cursor-pointer items-center gap-2.5">
					<input type="checkbox" class="check" bind:checked={mrc.ssl} />
					<span class="text-ink">{t('admin.doors.ssl_port_5001_plain_is')}</span>
				</label>
			</details>
			{#if mrcTemplate}
				<p class="text-xs leading-relaxed text-faint">
					{t('admin.doors.installing_starts_the_chat_connection')}
				</p>
			{/if}
			<div class="flex justify-end gap-2.5">
				<button
					type="button"
					class="btn-secondary btn-sm"
					onclick={() => {
						mrc = null;
						mrcTemplate = null;
					}}>{t('web.common.cancel')}</button
				>
				<button type="submit" class="btn-primary btn-sm" disabled={mrcSaving}>
					{mrcTemplate ? t('admin.doors.install') : mrcSaving ? t('web.common.saving') : t('web.common.save')}
				</button>
			</div>
		</form>
	</Modal>
{/if}
