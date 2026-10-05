<script lang="ts">
	import { t } from '$lib/i18n.svelte';
	// The rooms callers may enter (/rooms, /join in the teleconference)
	// and the Discord bot that bridges them to channels.
	import { onMount, onDestroy } from 'svelte';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listChatRoomSettings,
		saveChatRoom,
		deleteChatRoom,
		getDiscord,
		saveDiscord,
		getMatrix,
		saveMatrix,
		getChatSettings,
		saveChatSettings,
		ApiError,
		type ChatRoomSettings,
		type DiscordState,
		type MatrixState
	} from '$lib/api';

	let { onchange }: { onchange?: (rooms: ChatRoomSettings[]) => void } = $props();

	let rooms = $state<ChatRoomSettings[]>([]);
	let editing = $state<ChatRoomSettings | null>(null);
	let isNew = $state(false);
	let dc = $state<DiscordState | null>(null);
	let mx = $state<MatrixState | null>(null);
	let mxLogin = $state({ homeserver: 'https://matrix.org', user: '', password: '' });
	let tokenInput = $state('');
	let saving = $state(false);
	let announceSysops = $state(false);
	let timer: ReturnType<typeof setInterval> | undefined;

	const fail = (err: unknown, fallback: string) => toast.push(err instanceof ApiError ? err.message : fallback, 'error');

	async function load() {
		if (!auth.token) return;
		try {
			[rooms, dc, mx] = await Promise.all([listChatRoomSettings(auth.token), getDiscord(auth.token), getMatrix(auth.token)]);
			getChatSettings(auth.token)
				.then((s) => (announceSysops = s.announce_sysops))
				.catch(() => {});
			if (mx.homeserver) mxLogin.homeserver = mx.homeserver;
			onchange?.(rooms);
		} catch (err) {
			fail(err, t('web.common.could_not_load_the_rooms'));
		}
	}

	onMount(() => {
		load();
		// While connecting, the status and channels change by themselves.
		timer = setInterval(async () => {
			if (auth.token && dc?.enabled) dc = await getDiscord(auth.token).catch(() => dc);
			if (auth.token && mx?.enabled) mx = await getMatrix(auth.token).catch(() => mx);
		}, 5000);
	});
	onDestroy(() => clearInterval(timer));

	async function saveAnnounce() {
		if (!auth.token) return;
		try {
			await saveChatSettings(auth.token, { announce_sysops: announceSysops });
			toast.push(announceSysops ? t('admin.chatsettings.sysops_announced') : t('admin.chatsettings.sysops_quiet'), 'success');
		} catch (err) {
			announceSysops = !announceSysops;
			fail(err, t('admin.chatsettings.could_not_save_that'));
		}
	}

	function edit(r?: ChatRoomSettings) {
		isNew = !r;
		editing = r ? { ...r } : { name: '', title: '', topic: '', min_sl: 0, sort_order: (rooms.length + 1) * 10, discord_channel: '', matrix_room: '' };
	}

	async function saveRoom(e: SubmitEvent) {
		e.preventDefault();
		if (!auth.token || !editing) return;
		try {
			await saveChatRoom(auth.token, { ...editing, name: editing.name.trim().toLowerCase() });
			editing = null;
			await load();
			toast.push(t('admin.chatsettings.room_saved'), 'success');
		} catch (err) {
			fail(err, t('admin.chatsettings.could_not_save_the_room'));
		}
	}

	async function removeRoom(r: ChatRoomSettings) {
		if (!auth.token || !confirm(t('admin.chatsettings.remove_the_room_title_callers', { TITLE: r.title }))) return;
		try {
			await deleteChatRoom(auth.token, r.name);
			await load();
		} catch (err) {
			fail(err, t('admin.chatsettings.could_not_remove_the_room'));
		}
	}

	async function saveBot(enabled: boolean, extra: { token?: string; clear_token?: boolean } = {}) {
		if (!auth.token || !dc) return;
		saving = true;
		try {
			dc = await saveDiscord(auth.token, { enabled, quiet: dc.quiet, ...extra });
			tokenInput = '';
			toast.push(enabled ? t('admin.chatsettings.saved_the_bot_connects_in') : t('common.saved'), 'success');
			setTimeout(load, 3000);
		} catch (err) {
			fail(err, t('admin.common.could_not_save'));
		} finally {
			saving = false;
		}
	}

	async function saveMx(enabled: boolean, extra: { forget?: boolean; login?: boolean } = {}) {
		if (!auth.token || !mx) return;
		saving = true;
		try {
			mx = await saveMatrix(auth.token, {
				enabled,
				quiet: mx.quiet,
				homeserver: mxLogin.homeserver,
				...(extra.login ? { user: mxLogin.user, password: mxLogin.password } : {}),
				...(extra.forget ? { forget: true } : {})
			});
			mxLogin.password = '';
			toast.push(extra.login ? t('admin.chatsettings.logged_in_the_bot_connects') : t('common.saved'), 'success');
			setTimeout(load, 3000);
		} catch (err) {
			fail(err, t('admin.common.could_not_save'));
		} finally {
			saving = false;
		}
	}

	const matrixName = (id: string) => mx?.rooms.find((r) => r.id === id)?.name ?? id;

	const channelName = (id: string) => {
		const c = dc?.channels.find((x) => x.id === id);
		return c ? `#${c.name} (${c.guild})` : id ? t('admin.chatsettings.channel_id', { ID: id }) : '';
	};
</script>

<div class="grid gap-4 lg:grid-cols-2">
	<section class="card">
		<div class="mb-3 flex items-center justify-between">
			<h2 class="card-label">{t('web.common.rooms')}</h2>
			{#if !editing}<button class="btn-primary btn-xs" onclick={() => edit()}>{t('admin.chatsettings.add_a_room')}</button>{/if}
		</div>
		<p class="mb-3 text-xs leading-relaxed text-muted">
			{t('admin.chatsettings.in_the_teleconference_callers_see')} <span class="font-mono">/rooms</span> {t('admin.chatsettings.and_change_with')}
			<span class="font-mono">{t('admin.chatsettings.join_name')}</span>.
		</p>
		<label class="mb-3 flex items-center gap-2 text-xs text-muted">
			<input type="checkbox" class="check" bind:checked={announceSysops} onchange={saveAnnounce} />
			{t('admin.chatsettings.announce_sysops')}
		</label>
		{#if editing}
			<form class="mb-4 grid gap-2.5 sm:grid-cols-2" onsubmit={saveRoom}>
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('admin.chatsettings.name_for_join')}
					<input class="field font-mono" bind:value={editing.name} disabled={!isNew} required maxlength="64" placeholder="tech" />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('admin.chatsettings.title')}
					<input class="field" bind:value={editing.title} placeholder={t('admin.chatsettings.technik')} />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted sm:col-span-2">
					{t('admin.chatsettings.topic_shown_on_entering')}
					<input class="field" bind:value={editing.topic} maxlength="200" />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('admin.common.lowest_security_level')}
					<input class="field" type="number" min="0" max="255" bind:value={editing.min_sl} />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('admin.chatsettings.order')}
					<input class="field" type="number" bind:value={editing.sort_order} />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted sm:col-span-2">
					{t('admin.chatsettings.discord_channel')}
					{#if dc?.channels.length}
						<select class="field" bind:value={editing.discord_channel}>
							<option value="">{t('admin.chatsettings.not_bridged')}</option>
							{#each dc.channels as c (c.id)}
								<option value={c.id}>#{c.name} ({c.guild})</option>
							{/each}
						</select>
					{:else}
						<input class="field font-mono" bind:value={editing.discord_channel} placeholder={t('admin.chatsettings.channel_id_or_connect_the')} />
					{/if}
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted sm:col-span-2">
					{t('admin.chatsettings.matrix_room')}
					{#if mx?.rooms.length}
						<select class="field" bind:value={editing.matrix_room}>
							<option value="">{t('admin.chatsettings.not_bridged')}</option>
							{#each mx.rooms as r (r.id)}
								<option value={r.id}>{r.name}</option>
							{/each}
							{#if editing.matrix_room && !mx.rooms.some((r) => r.id === editing?.matrix_room)}
								<option value={editing.matrix_room}>{editing.matrix_room}</option>
							{/if}
						</select>
						<span class="text-[11px] text-faint">{t('admin.chatsettings.rooms_the_bot_is_in')}</span>
					{/if}
					<input class="field font-mono" bind:value={editing.matrix_room} placeholder={t('admin.chatsettings.room_matrix_org_or_id')} />
				</label>
				<div class="flex justify-end gap-2 sm:col-span-2">
					<button type="button" class="btn-secondary btn-sm" onclick={() => (editing = null)}>{t('web.common.cancel')}</button>
					<button type="submit" class="btn-primary btn-sm">{t('web.common.save')}</button>
				</div>
			</form>
		{/if}
		<div class="flex flex-col divide-y divide-line">
			{#each rooms as r (r.name)}
				<div class="flex flex-wrap items-center gap-x-3 gap-y-1 py-2.5 text-sm">
					<div class="min-w-0 flex-1">
						<div class="text-ink-strong">
							{r.title} <span class="font-mono text-xs text-faint">{r.name}</span>
							{#if r.min_sl > 0}<span class="text-xs text-faint"> {t('admin.chatsettings.sl_min_sl', { MIN_SL: r.min_sl })}</span>{/if}
						</div>
						{#if r.discord_channel}
							<div class="text-xs text-indigo-400">{t('admin.chatsettings.discord_v', { V: channelName(r.discord_channel) })}</div>
						{/if}
						{#if r.matrix_room}
							<div class="text-xs text-emerald-500">{t('admin.chatsettings.matrix_v', { V: matrixName(r.matrix_room) })}</div>
						{/if}
					</div>
					<button class="btn-secondary btn-xs" onclick={() => edit(r)}>{t('web.common.edit')}</button>
					{#if r.name !== 'main'}<button class="btn-secondary btn-xs" onclick={() => removeRoom(r)}>{t('web.common.remove')}</button>{/if}
				</div>
			{/each}
		</div>
	</section>

	<section class="card">
		<h2 class="card-label mb-3">{t('admin.chatsettings.discord_bridge')}</h2>
		{#if dc}
			<div class="mb-3 flex items-center gap-2 text-sm">
				<span class="inline-block h-2 w-2 rounded-full {dc.status.connected ? 'bg-emerald-400' : dc.enabled ? 'bg-amber-400' : 'bg-line-strong'}"></span>
				{#if dc.status.connected}
					<span>{t('admin.chatsettings.connected_as')} <span class="text-ink-strong">{dc.status.bot}</span>{#if dc.status.guilds?.length}{t('admin.chatsettings.on_v', { V: dc.status.guilds.join(', ') })}{/if}</span>
				{:else if dc.enabled}
					<span class="text-amber-300">{dc.status.error || t('web.common.connecting')}</span>
				{:else}
					<span class="text-muted">{t('admin.chatsettings.off')}</span>
				{/if}
			</div>

			{#if dc.status.connected && !dc.status.guilds?.length}
				<p class="mb-3 rounded-lg border border-line p-3 text-xs leading-relaxed text-ink-soft">
					{t('admin.chatsettings.the_bot_isn_t_on')}
					<a href={dc.invite_url} target="_blank" rel="noopener" class="text-accent hover:underline">{t('admin.chatsettings.add_it_to_your_server')}</a>
					{t('admin.chatsettings.then_pick_a_channel_for')}
				</p>
			{:else if dc.invite_url}
				<p class="mb-3 text-xs text-muted">
					<a href={dc.invite_url} target="_blank" rel="noopener" class="text-accent hover:underline">{t('admin.chatsettings.add_the_bot_to_another')}</a>
				</p>
			{/if}

			<div class="flex flex-col gap-3">
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('admin.chatsettings.bot_token_v', { V: dc.has_token ? t('admin.chatsettings.saved_enter_a_new_one') : '' })}
					<input class="field font-mono" type="password" autocomplete="off" bind:value={tokenInput} placeholder={dc.has_token ? '••••••••' : t('admin.chatsettings.from_the_developer_portal_bot')} />
				</label>
				<label class="flex items-center gap-2 text-sm">
					<input type="checkbox" bind:checked={dc.quiet} />
					{t('admin.chatsettings.don_t_tell_discord_when')}
				</label>
				<div class="flex flex-wrap justify-end gap-2">
					{#if dc.has_token && !dc.enabled}
						<button class="btn-secondary btn-sm" disabled={saving} onclick={() => saveBot(false, { clear_token: true })}>{t('admin.chatsettings.forget_the_token')}</button>
					{/if}
					{#if dc.enabled}
						<button class="btn-secondary btn-sm" disabled={saving} onclick={() => saveBot(false, tokenInput ? { token: tokenInput } : {})}>{t('admin.common.turn_off')}</button>
					{/if}
					<button class="btn-primary btn-sm" disabled={saving || (!dc.has_token && !tokenInput.trim())} onclick={() => saveBot(true, tokenInput ? { token: tokenInput } : {})}>
						{dc.enabled ? t('web.common.save') : t('admin.common.turn_on')}
					</button>
				</div>
			</div>

			<details class="mt-4 text-xs leading-relaxed text-muted">
				<summary class="cursor-pointer text-ink-soft">{t('admin.chatsettings.how_to_set_it_up')}</summary>
				<ol class="mt-2 ml-4 list-decimal space-y-1.5">
					<li>{t('admin.chatsettings.your_own_discord_server_if')} <b>+</b> {t('admin.chatsettings.at_the_bottom_of_the')}</li>
					<li>
						<a href="https://discord.com/developers/applications" target="_blank" rel="noopener" class="text-accent hover:underline">{t('admin.chatsettings.developer_portal')}</a>
						→ <b>{t('admin.chatsettings.new_application')}</b> {t('admin.chatsettings.the_name_is_the_bot')}
					</li>
					<li><b>{t('admin.chatsettings.bot')}</b> {t('admin.chatsettings.turn_on')} <b>{t('admin.chatsettings.message_content_intent')}</b> {t('admin.chatsettings.save')}</li>
					<li><b>{t('admin.chatsettings.bot')}</b> → <b>{t('admin.chatsettings.reset_token')}</b> {t('admin.chatsettings.copy_it_into_the_field')} <b>{t('admin.common.turn_on')}</b>.</li>
					<li>{t('admin.chatsettings.once_it_says_connected')} <b>{t('admin.chatsettings.add_it_to_your_server')}</b> {t('admin.chatsettings.the_link_above_it_asks')}</li>
					<li>{t('admin.chatsettings.give_each_room_its_channel')}</li>
				</ol>
			</details>
		{/if}
	</section>

	<section class="card lg:col-span-2">
		<h2 class="card-label mb-3">{t('admin.chatsettings.matrix_bridge')}</h2>
		{#if mx}
			<div class="mb-3 flex items-center gap-2 text-sm">
				<span class="inline-block h-2 w-2 rounded-full {mx.status.connected ? 'bg-emerald-400' : mx.enabled ? 'bg-amber-400' : 'bg-line-strong'}"></span>
				{#if mx.status.connected}
					<span>{t('admin.chatsettings.connected_as')} <span class="text-ink-strong">{mx.status.user_id}</span>{#if mx.rooms.length}{t('admin.chatsettings.in_length_room_s', { LENGTH: mx.rooms.length })}{/if}</span>
				{:else if mx.enabled}
					<span class="text-amber-300">{mx.status.error || t('web.common.connecting')}</span>
				{:else if mx.has_token}
					<span class="text-muted">{t('admin.chatsettings.off_logged_in_as_user', { USER_ID: mx.user_id })}</span>
				{:else}
					<span class="text-muted">{t('admin.chatsettings.off')}</span>
				{/if}
			</div>
			{#each mx.status.warnings ?? [] as w (w)}
				<p class="mb-2 rounded-lg border border-amber-500/40 bg-amber-500/5 p-2 text-xs text-amber-300">{w}</p>
			{/each}
			<div class="grid gap-3 sm:grid-cols-3">
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('admin.chatsettings.homeserver')}
					<input class="field font-mono" bind:value={mxLogin.homeserver} placeholder="https://matrix.org" />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('admin.chatsettings.bot_user_v', { V: mx.has_token ? t('admin.chatsettings.to_log_in_again') : '' })}
					<input class="field font-mono" bind:value={mxLogin.user} placeholder={mx.user_id || 'maiksplace-bot'} autocomplete="off" />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('admin.chatsettings.its_password_only_used_to')}
					<input class="field" type="password" bind:value={mxLogin.password} autocomplete="new-password" />
				</label>
			</div>
			<label class="mt-3 flex items-center gap-2 text-sm">
				<input type="checkbox" bind:checked={mx.quiet} />
				{t('admin.chatsettings.don_t_tell_matrix_when')}
			</label>
			<div class="mt-3 flex flex-wrap justify-end gap-2">
				{#if mx.has_token && !mx.enabled}
					<button class="btn-secondary btn-sm" disabled={saving} onclick={() => saveMx(false, { forget: true })}>{t('web.common.log_out')}</button>
				{/if}
				{#if mx.enabled}
					<button class="btn-secondary btn-sm" disabled={saving} onclick={() => saveMx(false)}>{t('admin.common.turn_off')}</button>
				{/if}
				{#if mxLogin.password}
					<button class="btn-primary btn-sm" disabled={saving || !mxLogin.user.trim()} onclick={() => saveMx(true, { login: true })}>{t('admin.chatsettings.log_in_and_turn_on')}</button>
				{:else}
					<button class="btn-primary btn-sm" disabled={saving || !mx.has_token} onclick={() => saveMx(true)}>{mx.enabled ? t('web.common.save') : t('admin.common.turn_on')}</button>
				{/if}
			</div>
			<details class="mt-4 text-xs leading-relaxed text-muted">
				<summary class="cursor-pointer text-ink-soft">{t('admin.chatsettings.how_to_set_it_up')}</summary>
				<ol class="mt-2 ml-4 list-decimal space-y-1.5">
					<li>{t('admin.chatsettings.an_account_for_the_bot')} <a href="https://app.element.io/#/register" target="_blank" rel="noopener" class="text-accent hover:underline">{t('admin.chatsettings.matrix_org_via_element')}</a> {t('admin.chatsettings.any_homeserver_works')}</li>
					<li>{t('admin.chatsettings.enter_the_homeserver_the_bot')} <b>{t('admin.chatsettings.log_in_and_turn_on')}</b>{t('admin.chatsettings.only_the_access_token_is')}</li>
					<li>{t('admin.chatsettings.with_your_own_account_create')} <b>{t('admin.chatsettings.without_encryption')}</b> {t('admin.chatsettings.the_bot_can_t_read')}</li>
					<li>{t('admin.chatsettings.give_each_bbs_room_its')}</li>
				</ol>
			</details>
		{/if}
	</section>
</div>
