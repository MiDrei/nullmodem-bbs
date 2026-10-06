<script lang="ts">
	import { t, tn } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		getConfig,
		putConfig,
		testBinkpConnection,
		sendNowBinkp,
		listGroups,
		addressDomain,
		ApiError,
		type BBSConfig,
		type BinkpUplink
	} from '$lib/api';

	let config = $state<BBSConfig | null>(null);
	let groups = $state<string[]>([]);
	let loadError = $state<string | null>(null);
	let saveError = $state<string | null>(null);
	let saveNote = $state<string | null>(null);
	let saving = $state(false);
	let activeTab = $state<'hubs' | 'downlinks'>('hubs');

	// The modal edits a detached copy so Cancel doesn't mutate
	// config.binkp_uplinks -- editingIndex is null while adding a new
	// entry (Save appends instead of writing back at an index).
	let editingUplink = $state<BinkpUplink | null>(null);
	let editingIndex = $state<number | null>(null);
	let testing = $state(false);
	let sending = $state(false);

	function emptyUplink(downlink: boolean): BinkpUplink {
		return {
			address: '',
			host: '',
			password: '',
			poll_disabled: false,
			poll_interval_seconds: 0,
			packet_password: '',
			tic_password: '',
			areafix_password: '',
			filefix_password: '',
			network: '',
			hold: false,
			no_cram: false,
			aka_addresses: [],
			downlink,
		};
	}

	function isAKAChecked(uplink: BinkpUplink, addr: string): boolean {
		return uplink.aka_addresses.includes(addr);
	}

	// The network (short name) an own address belongs to, by domain.
	function networkOf(addr: string): string {
		const d = addressDomain(addr);
		return config?.networks.find((n) => n.domain.toLowerCase() === d)?.name ?? '';
	}

	// Our own addresses in the uplink's network.
	function networkAddresses(uplink: BinkpUplink): string[] {
		const n = config?.networks.find((x) => x.name.toLowerCase() === uplink.network.toLowerCase());
		if (!n) return [];
		return (config?.ftn_addresses ?? []).filter((a) => addressDomain(a) === n.domain.toLowerCase());
	}

	// Picking a network for an uplink that isn't restricted to any of
	// our addresses yet restricts it to that network's.
	function networkChanged(uplink: BinkpUplink) {
		if (uplink.aka_addresses.length === 0) uplink.aka_addresses = networkAddresses(uplink);
	}

	function toggleAKA(uplink: BinkpUplink, addr: string) {
		uplink.aka_addresses = isAKAChecked(uplink, addr)
			? uplink.aka_addresses.filter((a) => a !== addr)
			: [...uplink.aka_addresses, addr];
	}

	let hubRows = $derived(
		(config?.binkp_uplinks ?? [])
			.map((u, i) => ({ u, i }))
			.filter(({ u }) => !u.downlink)
	);
	let downlinkRows = $derived(
		(config?.binkp_uplinks ?? [])
			.map((u, i) => ({ u, i }))
			.filter(({ u }) => u.downlink)
	);

	function openEdit(index: number) {
		if (!config) return;
		// $state.snapshot, not structuredClone: cloning a live $state
		// proxy directly throws DataCloneError (a known Svelte 5 gotcha
		// -- the structured-clone algorithm rejects Proxy instances
		// outright). snapshot() already returns an independent, non-
		// reactive deep copy on its own, so nothing further is needed.
		editingUplink = $state.snapshot(config.binkp_uplinks[index]);
		editingIndex = index;
	}

	function openNew() {
		editingUplink = emptyUplink(activeTab === 'downlinks');
		editingIndex = null;
	}

	function closeModal() {
		editingUplink = null;
		editingIndex = null;
		modalError = null;
	}

	let modalError = $state<string | null>(null);
	let modalSaving = $state(false);

	// Saves the uplink list right away -- a staged change that only a
	// second "Save changes" click would persist got lost too easily.
	async function saveUplinks(list: BinkpUplink[]): Promise<boolean> {
		if (!config || !auth.token) return false;
		saveError = null;
		saveNote = null;
		try {
			const res = await putConfig(auth.token, { ...$state.snapshot(config), binkp_uplinks: list });
			config = res.config;
			saveNote = res.note;
			return true;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return false;
			}
			throw err;
		}
	}

	async function saveModal() {
		if (!config || !editingUplink) return;
		const edited = $state.snapshot(editingUplink) as BinkpUplink;
		const current = $state.snapshot(config.binkp_uplinks) as BinkpUplink[];
		const list =
			editingIndex === null ? [...current, edited] : current.map((u, i) => (i === editingIndex ? edited : u));
		modalError = null;
		modalSaving = true;
		try {
			if (await saveUplinks(list)) {
				toast.push(t('admin.binkp_uplinks.saved_v', { V: edited.address || t('admin.binkp_uplinks.the_uplink') }), 'success');
				closeModal();
			}
		} catch (err) {
			modalError = err instanceof ApiError ? err.message : t('admin.common.could_not_save');
		} finally {
			modalSaving = false;
		}
	}

	async function removeUplink(index: number) {
		if (!config) return;
		const u = config.binkp_uplinks[index];
		if (!confirm(t('admin.binkp_uplinks.remove_host', { HOST: u.address || u.host }))) return;
		const list = ($state.snapshot(config.binkp_uplinks) as BinkpUplink[]).filter((_, i) => i !== index);
		try {
			if (await saveUplinks(list)) toast.push(t('admin.binkp_uplinks.removed_host', { HOST: u.address || u.host }), 'success');
		} catch (err) {
			saveError = err instanceof ApiError ? err.message : t('admin.common.could_not_save');
		}
	}

	async function testUplink() {
		if (!config || !auth.token || !editingUplink) return;
		if (!editingUplink.host.trim()) {
			toast.push(t('admin.binkp_uplinks.enter_a_host_port_first'), 'error');
			return;
		}
		if (config.ftn_addresses.length === 0) {
			toast.push(t('admin.binkp_uplinks.set_this_system_s_own'), 'error');
			return;
		}
		testing = true;
		try {
			const res = await testBinkpConnection(auth.token, editingUplink);
			toast.push(t('admin.binkp_uplinks.connected_uplink_claims_v', { V: res.remote_addresses.join(', ') }), 'success');
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			toast.push(err instanceof ApiError ? err.message : t('admin.binkp_uplinks.connection_test_failed'), 'error');
		} finally {
			testing = false;
		}
	}

	async function sendNowUplink() {
		if (!config || !auth.token || !editingUplink) return;
		if (!editingUplink.host.trim()) {
			toast.push(t('admin.binkp_uplinks.enter_a_host_port_first'), 'error');
			return;
		}
		if (config.ftn_addresses.length === 0) {
			toast.push(t('admin.binkp_uplinks.set_this_system_s_own'), 'error');
			return;
		}
		sending = true;
		try {
			const res = await sendNowBinkp(auth.token, editingUplink);
			toast.push(
				t('admin.binkp_uplinks.polled', { SENT: res.sent, SENT_ECHO: res.sent_echo, FWD_ECHO: res.forwarded_echo, FWD_FILES: res.forwarded_files, RECEIVED: res.received, RECEIVED_ECHO: res.received_echo, RECEIVED_FILES: res.received_files }),
				'success'
			);
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			toast.push(err instanceof ApiError ? err.message : t('admin.binkp_uplinks.sending_failed'), 'error');
		} finally {
			sending = false;
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			config = await getConfig(auth.token);
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : t('admin.common.could_not_load_configuration');
		}
		try {
			groups = await listGroups(auth.token);
		} catch {
			// Non-critical: the Group field just falls back to free text.
		}
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (!config || !auth.token) return;
		saveError = null;
		saveNote = null;
		saving = true;
		try {
			const res = await putConfig(auth.token, config);
			config = res.config;
			saveNote = res.note;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			saveError = err instanceof ApiError ? err.message : t('admin.common.could_not_save_configuration');
		} finally {
			saving = false;
		}
	}

	function handleModalKeydown(e: KeyboardEvent) {
		if (editingUplink && e.key === 'Escape') closeModal();
	}
</script>

<svelte:window onkeydown={handleModalKeydown} />

<datalist id="groups-list">
	{#each groups as g (g)}
		<option value={g}></option>
	{/each}
</datalist>

<div class="mb-6 flex items-center justify-between">
	<h1 class="page-title">{t('admin.common.uplinks_nodes_points')}</h1>
	<a href="/admin/binkp" class="btn-secondary btn-sm">
		{t('admin.binkp_uplinks.networks_addresses')}
	</a>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !config}
	<p class="text-sm text-slate-400">{t('web.common.loading')}</p>
{:else}
	<form class="flex flex-col gap-6" onsubmit={handleSubmit}>
		<section class="flex flex-col gap-4 rounded-xl border border-line p-4">
			<p class="text-xs text-slate-500">
				{t('admin.binkp_uplinks.nodes_hubs_points_this_system')}
			</p>
			<label class="flex max-w-xs flex-col gap-1 text-sm">
				<span class="text-slate-400">{t('admin.binkp_uplinks.default_poll_interval_seconds')}</span>
				<input
					type="number"
					min="1"
					class="field font-mono"
					bind:value={config.binkp_default_poll_interval_seconds}
					placeholder="900"
				/>
				<span class="text-xs text-slate-500">
					{t('admin.binkp_uplinks.used_by_any_uplink_below')}
				</span>
			</label>

			<div class="flex gap-1 border-b border-slate-800">
				<button
					type="button"
					class="border-b-2 px-3 py-2 text-sm font-medium transition {activeTab === 'hubs'
						? 'border-cyan-400 text-slate-100'
						: 'border-transparent text-slate-500 hover:text-slate-300'}"
					onclick={() => (activeTab = 'hubs')}
				>
					{t('admin.binkp_uplinks.hubs_length', { LENGTH: hubRows.length })}
				</button>
				<button
					type="button"
					class="border-b-2 px-3 py-2 text-sm font-medium transition {activeTab === 'downlinks'
						? 'border-cyan-400 text-slate-100'
						: 'border-transparent text-slate-500 hover:text-slate-300'}"
					onclick={() => (activeTab = 'downlinks')}
				>
					{t('admin.binkp_uplinks.nodes_points_length', { LENGTH: downlinkRows.length })}
				</button>
			</div>

			{#if activeTab === 'hubs'}
				<p class="text-xs text-slate-500">
					{t('admin.binkp_uplinks.upstream_networks_hubs_this_system')}
				</p>
			{:else}
				<p class="text-xs text-slate-500">
					{t('admin.binkp_uplinks.this_system_s_own_downstream')}
				</p>
			{/if}

			<div class="flex items-center justify-between">
				<span class="text-sm text-slate-400">
					{(activeTab === 'hubs' ? hubRows : downlinkRows).length === 0
						? t('admin.binkp_uplinks.none_configured_yet') : ''}
				</span>
				<button
					type="button"
					class="btn-secondary btn-xs"
					onclick={openNew}
				>
					{t('admin.binkp_uplinks.add_v', { V: activeTab === 'hubs' ? t('admin.binkp_uplinks.hub') : t('admin.binkp_uplinks.node_point') })}
				</button>
			</div>

			<div class="flex flex-col divide-y divide-slate-800 overflow-hidden rounded-xl border border-line">
				{#each (activeTab === 'hubs' ? hubRows : downlinkRows) as { u, i } (i)}
					<div class="flex items-center gap-3 px-3 py-2 hover:bg-slate-800/40">
						<div class="min-w-0 flex-1">
							<div class="flex flex-wrap items-center gap-2">
								<span class="truncate font-mono text-sm text-slate-100">
									{u.address || t('admin.binkp_uplinks.no_address_set')}
								</span>
								{#if u.network}
									<span class="rounded-full bg-slate-800 px-2 py-0.5 text-[10px] text-slate-300">
										{u.network}
									</span>
								{/if}
								{#if u.hold}
									<span class="rounded-full bg-red-950 px-2 py-0.5 text-[10px] text-red-400">
										{t('admin.common.hold')}
									</span>
								{/if}
								{#if u.no_cram}
									<span
										class="rounded-full bg-red-950 px-2 py-0.5 text-[10px] text-red-400"
										title={t('admin.binkp_uplinks.session_password_sent_in_the')}
									>
										{t('admin.binkp_uplinks.no_cram')}
									</span>
								{/if}
								{#if u.poll_disabled}
									<span class="rounded-full bg-amber-950 px-2 py-0.5 text-[10px] text-amber-400">
										{t('admin.common.crash_only')}
									</span>
								{/if}
							</div>
							<div class="truncate font-mono text-xs text-slate-500">
								{u.host || t('admin.binkp_uplinks.no_host_set')}
							</div>
						</div>
						<div class="flex shrink-0 gap-2">
							<button
								type="button"
								class="btn-secondary btn-xs"
								onclick={() => openEdit(i)}
							>
								{t('web.common.edit')}
							</button>
							<button
								type="button"
								class="btn-danger btn-xs"
								onclick={() => removeUplink(i)}
							>
								{t('web.common.remove')}
							</button>
						</div>
					</div>
				{/each}
			</div>
		</section>

		{#if saveError}
			<p class="text-sm text-red-400">{saveError}</p>
		{/if}
		{#if saveNote}
			<p class="text-sm text-amber-400">{saveNote}</p>
		{/if}

		<button
			type="submit"
			disabled={saving}
			class="btn-primary"
		>
			{saving ? t('web.common.saving') : t('admin.binkp_uplinks.save_poll_interval')}
		</button>
	</form>

	{#if editingUplink}
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4"
			onclick={closeModal}
		>
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div
				class="max-h-[90vh] w-full max-w-2xl overflow-auto rounded-2xl border border-line-strong bg-surface p-6 shadow-2xl shadow-black/50"
				onclick={(e) => e.stopPropagation()}
				role="dialog"
				aria-modal="true"
				tabindex="-1"
			>
				<div class="mb-4 flex items-center justify-between gap-4">
					<h2 class="card-label">
						{editingIndex === null ? t('admin.common.add') : t('web.common.edit')}
						{editingUplink.downlink ? t('admin.binkp_uplinks.node_point') : t('admin.binkp_uplinks.hub')}
					</h2>
					<button
						type="button"
						class="shrink-0 rounded-full border border-slate-700 px-2.5 py-1 text-xs text-slate-300 hover:bg-slate-800"
						onclick={closeModal}
					>
						{t('web.common.close')}
					</button>
				</div>

				<div class="grid grid-cols-2 gap-3">
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">{t('admin.binkp_uplinks.their_ftn_address')}</span>
						<input
							class="field field-sm font-mono"
							bind:value={editingUplink.address}
							placeholder="21:3/194"
						/>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">{t('admin.binkp_uplinks.host_port')}</span>
						<input
							class="field field-sm font-mono"
							bind:value={editingUplink.host}
							placeholder="bbs.example.com:24554"
						/>
					</label>
					<label class="col-span-2 flex flex-col gap-1 text-sm">
						<span class="text-slate-400">{t('admin.binkp_uplinks.session_password')}</span>
						<input
							type="password"
							class="field field-sm font-mono"
							bind:value={editingUplink.password}
							placeholder={t('admin.binkp_uplinks.optional_blank_for_an_open')}
						/>
						<span class="text-xs text-slate-500">{t('admin.binkp_uplinks.authenticates_the_binkp_session_itself')}</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">{t('admin.binkp_uplinks.packet_password')}</span>
						<input
							type="password"
							maxlength="8"
							class="field field-sm font-mono"
							bind:value={editingUplink.packet_password}
							placeholder={t('admin.binkp_uplinks.optional_max_8_chars')}
						/>
						<span class="text-xs text-slate-500">
							{t('admin.binkp_uplinks.authenticates_the_fts_0001_pkt')}
						</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">{t('admin.binkp_uplinks.tic_password')}</span>
						<input
							type="password"
							class="field field-sm font-mono"
							bind:value={editingUplink.tic_password}
							placeholder={t('admin.binkp_uplinks.optional')}
						/>
						<span class="text-xs text-slate-500">
							{t('admin.binkp_uplinks.authenticates_inbound_tic_file_echo')}
						</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">{t('admin.binkp_uplinks.areafix_password')}</span>
						<input
							type="password"
							class="field field-sm font-mono"
							bind:value={editingUplink.areafix_password}
							placeholder={t('admin.binkp_uplinks.optional')}
						/>
						<span class="text-xs text-slate-500">
							{t('admin.binkp_uplinks.sent_as_the_first_line')}
						</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">{t('admin.binkp_uplinks.filefix_password')}</span>
						<input
							type="password"
							class="field field-sm font-mono"
							bind:value={editingUplink.filefix_password}
							placeholder={t('admin.binkp_uplinks.optional')}
						/>
						<span class="text-xs text-slate-500">
							{t('admin.binkp_uplinks.sent_as_the_first_line_2')}
						</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">{t('admin.common.network')}</span>
						<select
							class="field field-sm"
							bind:value={editingUplink.network}
							onchange={() => networkChanged(editingUplink!)}
						>
							<option value="">{t('admin.binkp_uplinks.none_never_carries_outgoing_echomail')}</option>
							{#each config.networks as n (n.name)}
								<option value={n.name}>{n.name} (@{n.domain})</option>
							{/each}
							{#if editingUplink.network && !config.networks.some((n) => n.name.toLowerCase() === editingUplink!.network.toLowerCase())}
								<option value={editingUplink.network}>{t('admin.binkp_uplinks.network_not_defined', { NETWORK: editingUplink.network })}</option>
							{/if}
						</select>
						<span class="text-xs text-slate-500">
							{t('admin.binkp_uplinks.locally_posted_echomail_in_this')}
						</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">{t('admin.binkp_uplinks.poll_interval_override_seconds')}</span>
						<input
							type="number"
							min="0"
							class="field field-sm font-mono"
							bind:value={editingUplink.poll_interval_seconds}
							placeholder={t('admin.binkp_uplinks.0_use_default_v_s', { V: config.binkp_default_poll_interval_seconds || 900 })}
						/>
						<span class="text-xs text-slate-500">
							{t('admin.binkp_uplinks.still_meaningful_when_crash_only')}
						</span>
					</label>
					<label class="col-span-2 flex items-center gap-2 text-sm">
						<input type="checkbox" class="check" bind:checked={editingUplink.downlink} />
						<span class="text-slate-400">
							{t('admin.binkp_uplinks.this_is_one_of_our')}
						</span>
					</label>
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" class="check" bind:checked={editingUplink.poll_disabled} />
						<span class="text-slate-400">
							{t('admin.binkp_uplinks.crash_only_exclude_from_the')}
						</span>
					</label>
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" class="check" bind:checked={editingUplink.hold} />
						<span class="text-slate-400">
							{t('admin.binkp_uplinks.hold_never_dialed_automatically_for')}
						</span>
					</label>
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" class="check" bind:checked={editingUplink.no_cram} />
						<span class="text-slate-400">
							{t('admin.binkp_uplinks.no_cram_md5_send_the')}
						</span>
					</label>
					<div class="col-span-2 flex flex-col gap-2 rounded-xl border border-line p-3">
						<span class="text-sm text-slate-400">{t('admin.binkp_uplinks.restrict_to_these_of_your')}</span>
						<span class="text-xs text-slate-500">
							{t('admin.binkp_uplinks.only_checked_addresses_are_presented')}
						</span>
						{#if config.ftn_addresses.filter((a) => a.trim()).length === 0}
							<p class="text-sm text-slate-500">
								{t('admin.binkp_uplinks.no_addresses_configured_yet_add')}
							</p>
						{/if}
						{#each config.ftn_addresses.filter((a) => a.trim()) as addr (addr)}
							<label class="flex items-center gap-2 text-sm">
								<input
									type="checkbox" class="check"
									checked={isAKAChecked(editingUplink, addr)}
									onchange={() => toggleAKA(editingUplink!, addr)}
								/>
								<span class="font-mono text-slate-300">{addr}</span>
								{#if networkOf(addr)}
									<span class="rounded-md border border-line-strong px-1.5 py-0.5 font-mono text-[10.5px] text-muted"
										>{networkOf(addr)}</span
									>
								{/if}
							</label>
						{/each}
						{#if networkAddresses(editingUplink).length > 0}
							<button
								type="button"
								class="btn-secondary btn-xs self-start"
								onclick={() => (editingUplink!.aka_addresses = networkAddresses(editingUplink!))}
							>
								{tn('admin.binkp_uplinks.only_network', networkAddresses(editingUplink).length, { NETWORK: editingUplink.network })}
							</button>
						{/if}
					</div>
				</div>

				<div class="mt-4 flex flex-wrap gap-2 border-t border-slate-800 pt-4">
					<button
						type="button"
						class="btn-secondary btn-sm"
						disabled={testing}
						onclick={testUplink}
					>
						{testing ? t('admin.common.testing') : t('admin.binkp_uplinks.test_connection')}
					</button>
					<button
						type="button"
						class="btn-secondary btn-sm"
						disabled={sending}
						onclick={sendNowUplink}
					>
						{sending ? t('admin.common.sending') : t('admin.binkp_uplinks.send_now')}
					</button>
					<div class="flex-1"></div>
					<button
						type="button"
						class="btn-secondary btn-sm"
						onclick={closeModal}
					>
						{t('web.common.cancel')}
					</button>
					<button
						type="button"
						class="btn-primary btn-sm"
						disabled={modalSaving}
						onclick={saveModal}
					>
						{modalSaving ? t('web.common.saving') : editingIndex === null ? t('admin.common.add') : t('web.common.save')}
					</button>
				</div>
				{#if modalError}
					<p class="mt-3 text-sm text-red-400">{modalError}</p>
				{/if}
			</div>
		</div>
	{/if}
{/if}
