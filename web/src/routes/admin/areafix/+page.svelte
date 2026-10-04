<script lang="ts">
	import { t, i18n } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		getConfig,
		requestAreafixChanges,
		requestAreafixList,
		getAreafixListReply,
		listAreafixSubscriptions,
		listAreafixGrants,
		setAreafixGrants,
		ApiError,
		type BBSConfig,
		type AreafixKind,
		type AreafixListReply,
		type AreaGrant
	} from '$lib/api';

	type Entry = {
		tag: string;
		description: string;
		checked: boolean;
		/** Whether this entry came from the hub's own %LIST reply, vs. one we already have recorded or typed in by hand -- purely informational, shown as a hint. */
		fromReply: boolean;
	};

	/** "outbound": areas we request from an uplink's own Areafix/Filefix robot. "downlink": which of our own local areas a downlink is permitted to request from ours (see internal/tosser's handleAreafixRequest -- default-deny until granted here). */
	type Mode = 'outbound' | 'downlink';
	let mode = $state<Mode>('outbound');

	let config = $state<BBSConfig | null>(null);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let uplinkIndex = $state(0);
	let kind = $state<AreafixKind>('echo');

	let existingSubscriptions = $state<string[]>([]);
	let listReply = $state<AreafixListReply | null>(null);
	let manualTags = $state<string[]>([]);
	let checkedOverrides = $state<Record<string, boolean>>({});
	let manualTagInput = $state('');
	let showRawReply = $state(false);

	let requestingList = $state(false);
	let refreshingReply = $state(false);
	let applying = $state(false);

	let grants = $state<AreaGrant[]>([]);
	let grantOverrides = $state<Record<string, boolean>>({});
	let loadingGrants = $state(false);
	let savingGrants = $state(false);

	let grantEntries = $derived.by((): AreaGrant[] => {
		return grants
			.map((g) => ({ ...g, granted: g.tag.toUpperCase() in grantOverrides ? grantOverrides[g.tag.toUpperCase()] : g.granted }))
			.sort((a, b) => a.tag.localeCompare(b.tag));
	});

	function toggleGrant(tag: string) {
		const key = tag.toUpperCase();
		const current = grantEntries.find((g) => g.tag.toUpperCase() === key)?.granted ?? false;
		grantOverrides = { ...grantOverrides, [key]: !current };
	}

	let uplink = $derived(config?.binkp_uplinks[uplinkIndex] ?? null);

	// Merges everything we know about into one checkbox list: areas
	// already on our own record (existingSubscriptions -- start
	// checked), areas the hub's own %LIST reply mentioned (checked if
	// the reply itself marked them subscribed), and any tag typed in
	// by hand -- deduplicated case-insensitively, in that order.
	let entries = $derived.by((): Entry[] => {
		const byTag = new Map<string, Entry>();
		for (const tag of existingSubscriptions) {
			byTag.set(tag.toUpperCase(), { tag, description: '', checked: true, fromReply: false });
		}
		for (const area of listReply?.areas ?? []) {
			const key = area.Tag.toUpperCase();
			const existing = byTag.get(key);
			byTag.set(key, {
				tag: area.Tag,
				description: area.Description,
				checked: existing ? existing.checked : area.Subscribed,
				fromReply: true
			});
		}
		for (const tag of manualTags) {
			const key = tag.toUpperCase();
			if (!byTag.has(key)) {
				byTag.set(key, { tag, description: '', checked: true, fromReply: false });
			}
		}
		for (const [key, entry] of byTag) {
			if (key in checkedOverrides) entry.checked = checkedOverrides[key];
		}
		return [...byTag.values()].sort((a, b) => a.tag.localeCompare(b.tag));
	});

	function toggleEntry(tag: string) {
		const key = tag.toUpperCase();
		const current = entries.find((e) => e.tag.toUpperCase() === key)?.checked ?? false;
		checkedOverrides = { ...checkedOverrides, [key]: !current };
	}

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return true;
		}
		return false;
	}

	async function loadState() {
		if (!auth.token || !uplink || !uplink.host.trim()) return;
		checkedOverrides = {};
		manualTags = [];
		try {
			const [subs, reply] = await Promise.all([
				listAreafixSubscriptions(auth.token, uplink.host, kind),
				getAreafixListReply(auth.token, uplink.address)
			]);
			existingSubscriptions = subs.area_tags;
			listReply = reply;
		} catch (err) {
			if (await handleAuthError(err)) return;
			// Non-fatal: the checkbox list just starts empty/manual-only.
		}
	}

	async function loadGrants() {
		if (!auth.token || !uplink || !uplink.host.trim()) return;
		grantOverrides = {};
		loadingGrants = true;
		try {
			const res = await listAreafixGrants(auth.token, uplink.host, kind);
			grants = res.areas;
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.areafix.could_not_load_areas'), 'error');
		} finally {
			loadingGrants = false;
		}
	}

	function loadForMode() {
		return mode === 'downlink' ? loadGrants() : loadState();
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			config = await getConfig(auth.token);
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : t('admin.areafix.could_not_load_configuration');
		} finally {
			loaded = true;
		}
		await loadForMode();
	});

	async function saveGrants() {
		if (!auth.token || !uplink) return;
		if (!uplink.host.trim()) {
			toast.push(t('admin.areafix.this_uplink_needs_a_host'), 'error');
			return;
		}
		const grantedTags = grantEntries.filter((g) => g.granted).map((g) => g.tag);
		savingGrants = true;
		try {
			await setAreafixGrants(auth.token, uplink.host, kind, grantedTags);
			toast.push(t('admin.areafix.length_area_s_granted_to', { LENGTH: grantedTags.length }), 'success');
			await loadGrants();
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.areafix.saving_grants_failed'), 'error');
		} finally {
			savingGrants = false;
		}
	}

	function addManualTag() {
		const tag = manualTagInput.trim().toUpperCase();
		if (!tag) return;
		if (!manualTags.some((x) => x.toUpperCase() === tag)) {
			manualTags = [...manualTags, tag];
		}
		manualTagInput = '';
	}

	async function requestList() {
		if (!auth.token || !uplink) return;
		if (!uplink.host.trim() || !uplink.address.trim()) {
			toast.push(t('admin.areafix.this_uplink_needs_a_host_2'), 'error');
			return;
		}
		requestingList = true;
		try {
			await requestAreafixList(auth.token, uplink, kind);
			toast.push(
				t('admin.areafix.list_requested'),
				'success'
			);
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.areafix.requesting_the_list_failed'), 'error');
		} finally {
			requestingList = false;
		}
	}

	async function refreshReply() {
		if (!auth.token || !uplink) return;
		refreshingReply = true;
		try {
			listReply = await getAreafixListReply(auth.token, uplink.address);
			if (!listReply.found) {
				toast.push(t('admin.areafix.no_reply_from_this_uplink'), 'success');
			}
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.areafix.could_not_fetch_the_reply'), 'error');
		} finally {
			refreshingReply = false;
		}
	}

	async function applyChanges() {
		if (!auth.token || !uplink) return;
		if (!uplink.host.trim() || !uplink.address.trim()) {
			toast.push(t('admin.areafix.this_uplink_needs_a_host_2'), 'error');
			return;
		}
		const existing = new Set(existingSubscriptions.map((x) => x.toUpperCase()));
		const changes = entries
			.filter((e) => e.checked !== existing.has(e.tag.toUpperCase()))
			.map((e) => ({ area_tag: e.tag, subscribe: e.checked }));
		if (changes.length === 0) {
			toast.push(t('admin.areafix.nothing_changed'), 'success');
			return;
		}
		applying = true;
		try {
			await requestAreafixChanges(auth.token, uplink, changes, kind);
			toast.push(
				t('admin.areafix.changes_queued', { COUNT: changes.length }),
				'success'
			);
			await loadState();
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.areafix.applying_changes_failed'), 'error');
		} finally {
			applying = false;
		}
	}
</script>

<h1 class="mb-2 page-title">{t('admin.areafix.areafix_filefix')}</h1>
<p class="mb-6 text-sm text-slate-400">
	{t('admin.areafix.request_echomail_areafix_or_file')}
</p>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">{t('admin.common.loading')}</p>
{:else if !config || config.binkp_uplinks.length === 0}
	<p class="text-sm text-slate-500">
		{t('admin.areafix.no_binkp_uplinks_configured_yet')} <a
			href="/admin/binkp/uplinks"
			class="text-cyan-400 underline">{t('admin.areafix.uplinks_page')}</a
		>
		{t('admin.areafix.first')}
	</p>
{:else}
	<div class="mb-4 flex gap-1 border-b border-slate-800">
		<button
			type="button"
			class="rounded-t px-3 py-1.5 text-sm {mode === 'outbound'
				? 'border-b-2 border-cyan-500 text-cyan-400'
				: 'text-slate-400 hover:text-slate-200'}"
			onclick={() => {
				mode = 'outbound';
				loadForMode();
			}}
		>
			{t('admin.areafix.outbound_our_requests')}
		</button>
		<button
			type="button"
			class="rounded-t px-3 py-1.5 text-sm {mode === 'downlink'
				? 'border-b-2 border-cyan-500 text-cyan-400'
				: 'text-slate-400 hover:text-slate-200'}"
			onclick={() => {
				mode = 'downlink';
				loadForMode();
			}}
		>
			{t('admin.areafix.downlink_access')}
		</button>
	</div>

	{#if mode === 'outbound'}
		<p class="mb-4 text-sm text-slate-400">
			{t('admin.areafix.request_echomail_areafix_or_file_2')}
		</p>
	{:else}
		<p class="mb-4 text-sm text-slate-400">
			{t('admin.areafix.a_downlink_authenticated_with_the')}
		</p>
	{/if}

	<div class="mb-6 flex flex-wrap items-end gap-3">
		<label class="flex flex-col gap-1 text-sm">
			<span class="text-slate-400">{mode === 'outbound' ? t('admin.areafix.uplink') : t('admin.areafix.downlink')}</span>
			<select
				class="field field-sm"
				bind:value={uplinkIndex}
				onchange={loadForMode}
			>
				{#each config.binkp_uplinks as u, i (i)}
					<option value={i}>{u.address || u.host || t('admin.areafix.uplink_v', { V: i + 1 })}</option>
				{/each}
			</select>
		</label>
		<label class="flex flex-col gap-1 text-sm">
			<span class="text-slate-400">{t('admin.areafix.kind')}</span>
			<select
				class="field field-sm"
				bind:value={kind}
				onchange={loadForMode}
			>
				<option value="echo">{t('admin.areafix.echo_areas_areafix')}</option>
				<option value="file">{t('admin.areafix.file_areas_filefix')}</option>
			</select>
		</label>
		{#if mode === 'outbound'}
			<button
				type="button"
				class="btn-secondary btn-sm"
				disabled={requestingList}
				onclick={requestList}
			>
				{requestingList ? t('admin.areafix.requesting') : t('admin.areafix.request_area_list')}
			</button>
			<button
				type="button"
				class="btn-secondary btn-sm"
				disabled={refreshingReply}
				onclick={refreshReply}
			>
				{refreshingReply ? t('admin.areafix.refreshing') : t('admin.areafix.refresh_reply')}
			</button>
		{/if}
	</div>

	{#if mode === 'downlink'}
		{#if loadingGrants}
			<p class="mb-4 text-sm text-slate-400">{t('admin.common.loading')}</p>
		{:else if grantEntries.length === 0}
			<p class="mb-4 text-sm text-slate-500">
				{t('admin.areafix.no_local_v_areas_exist', { V: kind === 'file' ? t('admin.areafix.kind_file') : t('admin.areafix.kind_echo') })}
			</p>
		{:else}
			<div class="mb-4 max-h-96 overflow-auto rounded-xl border border-line">
				<table class="w-full text-left text-sm">
					<tbody>
						{#each grantEntries as entry (entry.tag)}
							<tr class="border-b border-slate-800 last:border-0 hover:bg-slate-900">
								<td class="w-8 px-3 py-1.5">
									<input
										type="checkbox" class="check"
										checked={entry.granted}
										onchange={() => toggleGrant(entry.tag)}
									/>
								</td>
								<td class="px-1 py-1.5 font-mono text-slate-200">{entry.tag}</td>
								<td class="px-3 py-1.5 text-slate-400">{entry.name}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
		<button
			type="button"
			class="btn-primary"
			disabled={savingGrants}
			onclick={saveGrants}
		>
			{savingGrants ? t('admin.common.saving') : t('admin.areafix.save_grants')}
		</button>
	{:else if listReply?.found}
		<p class="mb-3 text-xs text-slate-500">
			{t('admin.areafix.showing_the_reply_received_posted', { POSTED_AT: new Date(listReply.posted_at ?? '').toLocaleString(i18n.locale), SUBJECT: listReply.subject })}
			<button type="button" class="text-cyan-400 underline" onclick={() => (showRawReply = !showRawReply)}>
				{t('admin.areafix.v_raw_text', { V: showRawReply ? t('admin.areafix.hide') : t('admin.areafix.show') })}
			</button>
		</p>
		{#if showRawReply}
			<pre
				class="mb-4 max-h-64 overflow-auto rounded-xl border border-line bg-slate-950 p-3 font-mono text-xs whitespace-pre-wrap text-slate-300">{listReply.raw_body}</pre>
		{/if}
	{:else}
		<p class="mb-3 text-xs text-slate-500">
			{t('admin.areafix.no_area_list_reply_on')}
		</p>
	{/if}

	{#if mode === 'outbound'}
		<div class="mb-4 flex items-end gap-2">
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">{t('admin.areafix.add_a_tag_by_hand')}</span>
				<input
					type="text"
					class="field field-sm font-mono"
					placeholder="AREA_TAG"
					bind:value={manualTagInput}
					onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), addManualTag())}
				/>
			</label>
			<button
				type="button"
				class="btn-secondary btn-sm"
				onclick={addManualTag}
			>
				{t('admin.common.add')}
			</button>
		</div>

		{#if entries.length === 0}
			<p class="mb-4 text-sm text-slate-500">{t('admin.areafix.no_areas_to_show_yet')}</p>
		{:else}
			<div class="mb-4 max-h-96 overflow-auto rounded-xl border border-line">
				<table class="w-full text-left text-sm">
					<tbody>
						{#each entries as entry (entry.tag)}
							<tr class="border-b border-slate-800 last:border-0 hover:bg-slate-900">
								<td class="w-8 px-3 py-1.5">
									<input
										type="checkbox" class="check"
										checked={entry.checked}
										onchange={() => toggleEntry(entry.tag)}
									/>
								</td>
								<td class="px-1 py-1.5 font-mono text-slate-200">{entry.tag}</td>
								<td class="px-3 py-1.5 text-slate-400">{entry.description}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}

		<button
			type="button"
			class="btn-primary"
			disabled={applying}
			onclick={applyChanges}
		>
			{applying ? t('admin.areafix.applying') : t('admin.areafix.apply_changes')}
		</button>
	{/if}
{/if}
