<script lang="ts">
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
			post_as: ''
		};
	}

	// A point of this system: a downlink with a point address.
	function isPoint(u: BinkpUplink): boolean {
		return u.downlink && /^\s*\d+:\d+\/\d+\.[1-9]\d*/.test(u.address);
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
	}

	function saveModal() {
		if (!config || !editingUplink) return;
		if (editingIndex === null) {
			config.binkp_uplinks = [...config.binkp_uplinks, editingUplink];
		} else {
			config.binkp_uplinks = config.binkp_uplinks.map((u, i) =>
				i === editingIndex ? editingUplink! : u
			);
		}
		closeModal();
	}

	function removeUplink(index: number) {
		if (!config) return;
		config.binkp_uplinks = config.binkp_uplinks.filter((_, i) => i !== index);
	}

	async function testUplink() {
		if (!config || !auth.token || !editingUplink) return;
		if (!editingUplink.host.trim()) {
			toast.push('Enter a host:port first.', 'error');
			return;
		}
		if (config.ftn_addresses.length === 0) {
			toast.push('Set this system’s own FTN address on the BinkP page first.', 'error');
			return;
		}
		testing = true;
		try {
			const res = await testBinkpConnection(auth.token, editingUplink);
			toast.push(`Connected. Uplink claims: ${res.remote_addresses.join(', ')}`, 'success');
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			toast.push(err instanceof ApiError ? err.message : 'Connection test failed.', 'error');
		} finally {
			testing = false;
		}
	}

	async function sendNowUplink() {
		if (!config || !auth.token || !editingUplink) return;
		if (!editingUplink.host.trim()) {
			toast.push('Enter a host:port first.', 'error');
			return;
		}
		if (config.ftn_addresses.length === 0) {
			toast.push('Set this system’s own FTN address on the BinkP page first.', 'error');
			return;
		}
		sending = true;
		try {
			const res = await sendNowBinkp(auth.token, editingUplink);
			toast.push(
				`Polled uplink: sent ${res.sent} netmail, ${res.sent_echo} echomail, forwarded ${res.forwarded_echo} echomail, ${res.forwarded_files} file(s), received ${res.received} netmail, ${res.received_echo} echomail, ${res.received_files} file(s).`,
				'success'
			);
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			toast.push(err instanceof ApiError ? err.message : 'Sending failed.', 'error');
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
			loadError = err instanceof ApiError ? err.message : 'Could not load configuration.';
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
			saveError = err instanceof ApiError ? err.message : 'Could not save configuration.';
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
	<h1 class="page-title">BinkP Uplinks</h1>
	<a href="/admin/binkp" class="btn-secondary btn-sm">
		&larr; BinkP
	</a>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !config}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	<form class="flex flex-col gap-6" onsubmit={handleSubmit}>
		<section class="flex flex-col gap-4 rounded-xl border border-line p-4">
			<p class="text-xs text-slate-500">
				Nodes/hubs/points this system exchanges netmail, echomail, and files with. The mailer
				daemon polls each uplink automatically on its own schedule; "Test" connects and
				authenticates now without sending or requesting any mail, and "Send Now" polls
				immediately -- sending anything queued and picking up anything waiting for us.
			</p>
			<label class="flex max-w-xs flex-col gap-1 text-sm">
				<span class="text-slate-400">Default poll interval (seconds)</span>
				<input
					type="number"
					min="1"
					class="field font-mono"
					bind:value={config.binkp_default_poll_interval_seconds}
					placeholder="900"
				/>
				<span class="text-xs text-slate-500">
					Used by any uplink below that doesn't set its own interval.
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
					Hubs ({hubRows.length})
				</button>
				<button
					type="button"
					class="border-b-2 px-3 py-2 text-sm font-medium transition {activeTab === 'downlinks'
						? 'border-cyan-400 text-slate-100'
						: 'border-transparent text-slate-500 hover:text-slate-300'}"
					onclick={() => (activeTab = 'downlinks')}
				>
					Nodes / Points ({downlinkRows.length})
				</button>
			</div>

			{#if activeTab === 'hubs'}
				<p class="text-xs text-slate-500">
					Upstream networks/hubs this system itself is fed by (fsxNet, HobbyNet, ...).
				</p>
			{:else}
				<p class="text-xs text-slate-500">
					This system's own downstream points/nodes -- entries where we are the hub.
				</p>
			{/if}

			<div class="flex items-center justify-between">
				<span class="text-sm text-slate-400">
					{(activeTab === 'hubs' ? hubRows : downlinkRows).length === 0
						? 'None configured yet.'
						: ''}
				</span>
				<button
					type="button"
					class="btn-secondary btn-xs"
					onclick={openNew}
				>
					+ Add {activeTab === 'hubs' ? 'Hub' : 'Node / Point'}
				</button>
			</div>

			<div class="flex flex-col divide-y divide-slate-800 overflow-hidden rounded-xl border border-line">
				{#each (activeTab === 'hubs' ? hubRows : downlinkRows) as { u, i } (i)}
					<div class="flex items-center gap-3 px-3 py-2 hover:bg-slate-800/40">
						<div class="min-w-0 flex-1">
							<div class="flex flex-wrap items-center gap-2">
								<span class="truncate font-mono text-sm text-slate-100">
									{u.address || '(no address set)'}
								</span>
								{#if u.network}
									<span class="rounded-full bg-slate-800 px-2 py-0.5 text-[10px] text-slate-300">
										{u.network}
									</span>
								{/if}
								{#if u.hold}
									<span class="rounded-full bg-red-950 px-2 py-0.5 text-[10px] text-red-400">
										Hold
									</span>
								{/if}
								{#if u.post_as}
									<span
										class="rounded-full bg-sky-950 px-2 py-0.5 text-[10px] text-sky-300"
										title="Its mail goes out as if {u.post_as} wrote it on the BBS"
									>
										Posts as {u.post_as}
									</span>
								{/if}
								{#if u.no_cram}
									<span
										class="rounded-full bg-red-950 px-2 py-0.5 text-[10px] text-red-400"
										title="Session password sent in the clear"
									>
										No CRAM
									</span>
								{/if}
								{#if u.poll_disabled}
									<span class="rounded-full bg-amber-950 px-2 py-0.5 text-[10px] text-amber-400">
										Crash-only
									</span>
								{/if}
							</div>
							<div class="truncate font-mono text-xs text-slate-500">
								{u.host || '(no host set)'}
							</div>
						</div>
						<div class="flex shrink-0 gap-2">
							<button
								type="button"
								class="btn-secondary btn-xs"
								onclick={() => openEdit(i)}
							>
								Edit
							</button>
							<button
								type="button"
								class="btn-danger btn-xs"
								onclick={() => removeUplink(i)}
							>
								Remove
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
			{saving ? 'Saving…' : 'Save changes'}
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
						{editingIndex === null ? 'Add' : 'Edit'}
						{editingUplink.downlink ? 'Node / Point' : 'Hub'}
					</h2>
					<button
						type="button"
						class="shrink-0 rounded-full border border-slate-700 px-2.5 py-1 text-xs text-slate-300 hover:bg-slate-800"
						onclick={closeModal}
					>
						Close
					</button>
				</div>

				<div class="grid grid-cols-2 gap-3">
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Their FTN address</span>
						<input
							class="field field-sm font-mono"
							bind:value={editingUplink.address}
							placeholder="21:3/194"
						/>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Host:Port</span>
						<input
							class="field field-sm font-mono"
							bind:value={editingUplink.host}
							placeholder="bbs.example.com:24554"
						/>
					</label>
					<label class="col-span-2 flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Session Password</span>
						<input
							type="password"
							class="field field-sm font-mono"
							bind:value={editingUplink.password}
							placeholder="(optional -- blank for an open/no-auth node)"
						/>
						<span class="text-xs text-slate-500">Authenticates the BinkP session itself.</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Packet Password</span>
						<input
							type="password"
							maxlength="8"
							class="field field-sm font-mono"
							bind:value={editingUplink.packet_password}
							placeholder="(optional, max 8 chars)"
						/>
						<span class="text-xs text-slate-500">
							Authenticates the FTS-0001 .pkt file itself (FTS-0001's 8-character packet header
							field) -- distinct from the session password above.
						</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">TIC Password</span>
						<input
							type="password"
							class="field field-sm font-mono"
							bind:value={editingUplink.tic_password}
							placeholder="(optional)"
						/>
						<span class="text-xs text-slate-500">
							Authenticates inbound TIC file-echo announcements from this uplink -- leave blank if
							it doesn't set one.
						</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Areafix Password</span>
						<input
							type="password"
							class="field field-sm font-mono"
							bind:value={editingUplink.areafix_password}
							placeholder="(optional)"
						/>
						<span class="text-xs text-slate-500">
							Sent as the first line of every echomail area (un)subscribe request to this uplink's
							"Areafix" robot -- see the Areafix / Filefix page under System to manage
							subscriptions.
						</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Filefix Password</span>
						<input
							type="password"
							class="field field-sm font-mono"
							bind:value={editingUplink.filefix_password}
							placeholder="(optional)"
						/>
						<span class="text-xs text-slate-500">
							Sent as the first line of every file-echo area (un)subscribe request to this uplink's
							"Filefix" robot -- commonly a different password from Areafix's.
						</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Network</span>
						<select
							class="field field-sm"
							bind:value={editingUplink.network}
							onchange={() => networkChanged(editingUplink!)}
						>
							<option value="">— none (never carries outgoing echomail) —</option>
							{#each config.networks as n (n.name)}
								<option value={n.name}>{n.name} (@{n.domain})</option>
							{/each}
							{#if editingUplink.network && !config.networks.some((n) => n.name.toLowerCase() === editingUplink!.network.toLowerCase())}
								<option value={editingUplink.network}>{editingUplink.network} (not defined)</option>
							{/if}
						</select>
						<span class="text-xs text-slate-500">
							Locally posted echomail in this network's areas goes out through this uplink. Networks
							are defined on the BinkP page.
						</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Poll interval override (seconds)</span>
						<input
							type="number"
							min="0"
							class="field field-sm font-mono"
							bind:value={editingUplink.poll_interval_seconds}
							placeholder={`0 = use default (${config.binkp_default_poll_interval_seconds || 900}s)`}
						/>
						<span class="text-xs text-slate-500">
							Still meaningful when Crash-only is checked below: a slow fallback poll, since a
							crash-only uplink otherwise only gets dialed when there's actually mail to send.
						</span>
					</label>
					<label class="col-span-2 flex items-center gap-2 text-sm">
						<input type="checkbox" class="check" bind:checked={editingUplink.downlink} />
						<span class="text-slate-400">
							This is one of our own nodes/points (we're their hub) -- lists it under "Nodes /
							Points" instead of "Hubs". With a point address (21:3/194.1) it is treated as a point:
							it only gets netmail addressed to it and the areas it subscribed to.
						</span>
					</label>
					{#if isPoint(editingUplink)}
						<label class="col-span-2 flex flex-col gap-1 text-sm">
							<span class="text-slate-400">Post as BBS user (for your own reader app)</span>
							<input
								class="field field-sm"
								bind:value={editingUplink.post_as}
								placeholder="(empty: an ordinary point)"
								autocomplete="off"
							/>
							<span class="text-xs text-slate-500">
								For a reader like FidoMail that you use as this point: its echomail and netmail go out
								as if this user wrote them on the BBS -- this system's address, MSGID and origin line --
								and netmail to this user is copied to the point too (the last two weeks when first
								set up). Give each network's point address its own entry, all with the same host
								label, password and user.
							</span>
						</label>
					{/if}
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" class="check" bind:checked={editingUplink.poll_disabled} />
						<span class="text-slate-400">
							Crash-only: exclude from the mailer's regular, interval-based scheduled poll --
							still dialed immediately whenever there's netmail or echomail actually pending for
							it (see the mailer's crash-style triggering), plus the poll interval above as a slow
							fallback if set, or manually via "Send Now"
						</span>
					</label>
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" class="check" bind:checked={editingUplink.hold} />
						<span class="text-slate-400">
							Hold: never dialed automatically for any reason at all, not even pending/Crash mail
							-- only via "Send Now", or by this uplink polling us itself. Use this for a peer
							that can't be reached back either way (e.g. a point behind NAT with no port
							forwarding), where Crash-only above still isn't enough to stop a doomed dial attempt
							every time there's mail pending for it.
						</span>
					</label>
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" class="check" bind:checked={editingUplink.no_cram} />
						<span class="text-slate-400">
							No CRAM-MD5: send the session password in the clear even when this uplink offers
							CRAM-MD5. Only for diagnosing a hub -- anyone on the path can read the password.
							Leave off otherwise.
						</span>
					</label>
					<div class="col-span-2 flex flex-col gap-2 rounded-xl border border-line p-3">
						<span class="text-sm text-slate-400">Restrict to these of your own addresses</span>
						<span class="text-xs text-slate-500">
							Only checked addresses are presented to this uplink (M_ADR) and count as its own for
							Crash-mail routing -- keeps an AKA that belongs to a different network from leaking
							into a hub that has nothing to do with it (a hub's own software can auto-register a
							new node entry for every address it sees in M_ADR). Leave all unchecked to keep the
							old behavior: every address applies to every uplink.
						</span>
						{#if config.ftn_addresses.filter((a) => a.trim()).length === 0}
							<p class="text-sm text-slate-500">
								No addresses configured yet -- add one on the BinkP page.
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
								Only {editingUplink.network}'s address{networkAddresses(editingUplink).length > 1 ? 'es' : ''}
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
						{testing ? 'Testing…' : 'Test Connection'}
					</button>
					<button
						type="button"
						class="btn-secondary btn-sm"
						disabled={sending}
						onclick={sendNowUplink}
					>
						{sending ? 'Sending…' : 'Send Now'}
					</button>
					<div class="flex-1"></div>
					<button
						type="button"
						class="btn-secondary btn-sm"
						onclick={closeModal}
					>
						Cancel
					</button>
					<button
						type="button"
						class="btn-primary btn-sm"
						onclick={saveModal}
					>
						{editingIndex === null ? 'Add' : 'Save'}
					</button>
				</div>
				<p class="mt-3 text-xs text-slate-500">
					Changes here apply to this page's own working copy -- use "Save changes" on the main
					page to actually persist them.
				</p>
			</div>
		</div>
	{/if}
{/if}
