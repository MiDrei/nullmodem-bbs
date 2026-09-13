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
		ApiError,
		type BBSConfig,
		type BinkpUplink
	} from '$lib/api';

	let config = $state<BBSConfig | null>(null);
	let loadError = $state<string | null>(null);
	let saveError = $state<string | null>(null);
	let saveNote = $state<string | null>(null);
	let saving = $state(false);
	let testingIndex = $state<number | null>(null);
	let sendingIndex = $state<number | null>(null);

	function emptyUplink(): BinkpUplink {
		return {
			address: '',
			host: '',
			password: '',
			poll_disabled: false,
			poll_interval_seconds: 0,
			packet_password: '',
			tic_password: '',
			areafix_password: ''
		};
	}

	function addUplink() {
		if (!config) return;
		config.binkp_uplinks = [...config.binkp_uplinks, emptyUplink()];
	}

	function removeUplink(index: number) {
		if (!config) return;
		config.binkp_uplinks = config.binkp_uplinks.filter((_, i) => i !== index);
	}

	function addFTNAddress() {
		if (!config) return;
		config.ftn_addresses = [...config.ftn_addresses, ''];
	}

	function removeFTNAddress(index: number) {
		if (!config) return;
		config.ftn_addresses = config.ftn_addresses.filter((_, i) => i !== index);
	}

	async function testUplink(index: number) {
		if (!config || !auth.token) return;
		const uplink = config.binkp_uplinks[index];
		if (!uplink.host.trim()) {
			toast.push('Enter a host:port first.', 'error');
			return;
		}
		if (config.ftn_addresses.length === 0) {
			toast.push('Set this system’s own FTN address above first.', 'error');
			return;
		}
		testingIndex = index;
		try {
			const res = await testBinkpConnection(auth.token, uplink);
			toast.push(`Connected. Uplink claims: ${res.remote_addresses.join(', ')}`, 'success');
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/login');
				return;
			}
			toast.push(err instanceof ApiError ? err.message : 'Connection test failed.', 'error');
		} finally {
			testingIndex = null;
		}
	}

	async function sendNowUplink(index: number) {
		if (!config || !auth.token) return;
		const uplink = config.binkp_uplinks[index];
		if (!uplink.host.trim()) {
			toast.push('Enter a host:port first.', 'error');
			return;
		}
		if (config.ftn_addresses.length === 0) {
			toast.push('Set this system’s own FTN address above first.', 'error');
			return;
		}
		sendingIndex = index;
		try {
			const res = await sendNowBinkp(auth.token, uplink);
			toast.push(`Polled uplink: sent ${res.sent}, received ${res.received}.`, 'success');
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/login');
				return;
			}
			toast.push(err instanceof ApiError ? err.message : 'Sending failed.', 'error');
		} finally {
			sendingIndex = null;
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/login');
			return;
		}
		try {
			config = await getConfig(auth.token);
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : 'Could not load configuration.';
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
				await goto('/login');
				return;
			}
			saveError = err instanceof ApiError ? err.message : 'Could not save configuration.';
		} finally {
			saving = false;
		}
	}
</script>

<h1 class="mb-6 text-xl font-semibold text-slate-100">BinkP</h1>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !config}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	<form class="flex flex-col gap-6" onsubmit={handleSubmit}>
		<section class="flex flex-col gap-4 rounded border border-slate-800 p-4">
			<div class="flex items-center justify-between">
				<h2 class="text-sm font-semibold tracking-wide text-cyan-400 uppercase">
					FTN Address(es)
				</h2>
				<button
					type="button"
					class="rounded border border-slate-700 px-2 py-0.5 text-xs hover:bg-slate-800"
					onclick={addFTNAddress}
				>
					+ Add Address
				</button>
			</div>
			<p class="text-xs text-slate-500">
				Your node address(es) on whichever FTN-compatible network(s) you're a member of
				(FidoNet, fsxNet, etc.), if any. The first is "primary": stamped on outgoing netmail.
				More than one is only needed if a single uplink presents you with more than one network
				in the same BinkP session (see internal/tosser).
			</p>
			{#if config.ftn_addresses.length === 0}
				<p class="text-sm text-slate-500">None configured.</p>
			{/if}
			{#each config.ftn_addresses as _, i (i)}
				<div class="flex items-center gap-2">
					<input
						class="flex-1 rounded border border-slate-700 bg-slate-900 px-3 py-2 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
						bind:value={config.ftn_addresses[i]}
						placeholder="e.g. 1:234/56.0"
					/>
					<button
						type="button"
						class="rounded border border-red-800 px-3 py-2 text-sm text-red-400 hover:bg-red-950"
						onclick={() => removeFTNAddress(i)}
					>
						Remove
					</button>
				</div>
			{/each}
		</section>

		<section class="flex flex-col gap-4 rounded border border-slate-800 p-4">
			<div class="flex items-center justify-between">
				<h2 class="text-sm font-semibold tracking-wide text-cyan-400 uppercase">BinkP Uplinks</h2>
				<button
					type="button"
					class="rounded border border-slate-700 px-2 py-1 text-xs hover:bg-slate-800"
					onclick={addUplink}
				>
					+ Add Uplink
				</button>
			</div>
			<p class="text-xs text-slate-500">
				Nodes/hubs this system exchanges netmail with (echomail routing isn't wired up yet). The
				mailer daemon polls each uplink automatically on its own schedule; "Test" connects and
				authenticates now without sending or requesting any mail, and "Send Now" polls
				immediately -- sending anything queued and picking up anything waiting for us.
			</p>
			<label class="flex max-w-xs flex-col gap-1 text-sm">
				<span class="text-slate-400">Default poll interval (seconds)</span>
				<input
					type="number"
					min="1"
					class="rounded border border-slate-700 bg-slate-900 px-3 py-2 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={config.binkp_default_poll_interval_seconds}
					placeholder="900"
				/>
				<span class="text-xs text-slate-500">
					Used by any uplink below that doesn't set its own interval.
				</span>
			</label>
			{#if config.binkp_uplinks.length === 0}
				<p class="text-sm text-slate-500">No uplinks configured.</p>
			{/if}
			{#each config.binkp_uplinks as uplink, i (i)}
				<div class="grid grid-cols-2 gap-3 rounded border border-slate-800 p-3">
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Their FTN address</span>
						<input
							class="rounded border border-slate-700 bg-slate-900 px-2 py-1 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
							bind:value={uplink.address}
							placeholder="21:3/194"
						/>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Host:Port</span>
						<input
							class="rounded border border-slate-700 bg-slate-900 px-2 py-1 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
							bind:value={uplink.host}
							placeholder="bbs.example.com:24554"
						/>
					</label>
					<label class="col-span-2 flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Session Password</span>
						<input
							type="password"
							class="rounded border border-slate-700 bg-slate-900 px-2 py-1 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
							bind:value={uplink.password}
							placeholder="(optional -- blank for an open/no-auth node)"
						/>
						<span class="text-xs text-slate-500">Authenticates the BinkP session itself.</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Packet Password</span>
						<input
							type="password"
							maxlength="8"
							class="rounded border border-slate-700 bg-slate-900 px-2 py-1 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
							bind:value={uplink.packet_password}
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
							class="rounded border border-slate-700 bg-slate-900 px-2 py-1 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
							bind:value={uplink.tic_password}
							placeholder="(optional)"
						/>
						<span class="text-xs text-slate-500">
							For file-echo (TIC) distribution -- stored for when that's implemented, not used
							yet.
						</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Areafix Password</span>
						<input
							type="password"
							class="rounded border border-slate-700 bg-slate-900 px-2 py-1 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
							bind:value={uplink.areafix_password}
							placeholder="(optional)"
						/>
						<span class="text-xs text-slate-500">
							For automated echomail area subscription requests -- stored for when Areafix
							support is implemented, not used yet.
						</span>
					</label>
					<label class="flex flex-col gap-1 text-sm">
						<span class="text-slate-400">Poll interval override (seconds)</span>
						<input
							type="number"
							min="0"
							class="rounded border border-slate-700 bg-slate-900 px-2 py-1 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none disabled:opacity-50"
							bind:value={uplink.poll_interval_seconds}
							disabled={uplink.poll_disabled}
							placeholder={`0 = use default (${config.binkp_default_poll_interval_seconds || 900}s)`}
						/>
					</label>
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" bind:checked={uplink.poll_disabled} />
						<span class="text-slate-400">
							Crash-only: exclude from the mailer's regular scheduled poll (still reachable via
							"Send Now", and automatically dialed for Crash-flagged netmail addressed to this
							uplink's own network)
						</span>
					</label>
					<div class="col-span-2 flex gap-2">
						<button
							type="button"
							class="rounded border border-slate-700 px-3 py-1 text-sm hover:bg-slate-800 disabled:opacity-50"
							disabled={testingIndex === i}
							onclick={() => testUplink(i)}
						>
							{testingIndex === i ? 'Testing…' : 'Test Connection'}
						</button>
						<button
							type="button"
							class="rounded border border-cyan-700 px-3 py-1 text-sm text-cyan-400 hover:bg-cyan-950 disabled:opacity-50"
							disabled={sendingIndex === i}
							onclick={() => sendNowUplink(i)}
						>
							{sendingIndex === i ? 'Sending…' : 'Send Now'}
						</button>
						<button
							type="button"
							class="rounded border border-red-800 px-3 py-1 text-sm text-red-400 hover:bg-red-950"
							onclick={() => removeUplink(i)}
						>
							Remove
						</button>
					</div>
				</div>
			{/each}
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
			class="rounded bg-cyan-600 px-4 py-2 font-medium text-white hover:bg-cyan-500 disabled:opacity-50"
		>
			{saving ? 'Saving…' : 'Save changes'}
		</button>
	</form>
{/if}
