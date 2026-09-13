<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		getConfig,
		putConfig,
		testBinkpConnection,
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

	function emptyUplink(): BinkpUplink {
		return { address: '', host: '', password: '' };
	}

	function addUplink() {
		if (!config) return;
		config.binkp_uplinks = [...config.binkp_uplinks, emptyUplink()];
	}

	function removeUplink(index: number) {
		if (!config) return;
		config.binkp_uplinks = config.binkp_uplinks.filter((_, i) => i !== index);
	}

	async function testUplink(index: number) {
		if (!config || !auth.token) return;
		const uplink = config.binkp_uplinks[index];
		if (!uplink.host.trim()) {
			toast.push('Enter a host:port first.', 'error');
			return;
		}
		if (!config.ftn_address.trim()) {
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

<h1 class="mb-6 text-xl font-semibold text-slate-100">BBS Settings</h1>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !config}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	<form class="flex flex-col gap-6" onsubmit={handleSubmit}>
		<section class="flex flex-col gap-4 rounded border border-slate-800 p-4">
			<h2 class="text-sm font-semibold tracking-wide text-cyan-400 uppercase">General</h2>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">BBS Name</span>
				<input
					class="rounded border border-slate-700 bg-slate-900 px-3 py-2 text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={config.name}
					required
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Sysop Name</span>
				<input
					class="rounded border border-slate-700 bg-slate-900 px-3 py-2 text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={config.sysop}
					required
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">New User Security Level (0-255)</span>
				<input
					type="number"
					min="0"
					max="255"
					class="rounded border border-slate-700 bg-slate-900 px-3 py-2 text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={config.new_user_sl}
					required
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">FTN Address (zone:net/node.point)</span>
				<input
					class="rounded border border-slate-700 bg-slate-900 px-3 py-2 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={config.ftn_address}
					placeholder="e.g. 1:234/56.0 -- leave blank if you don't have one"
				/>
				<span class="text-xs text-slate-500">
					Your node address on whichever FTN-compatible network you're a member of (FidoNet,
					fsxNet, etc.), if any. Stamped on outgoing netmail; has no other effect until a BinkP
					mailer is set up.
				</span>
			</label>
		</section>

		<section class="flex flex-col gap-4 rounded border border-slate-800 p-4">
			<h2 class="text-sm font-semibold tracking-wide text-cyan-400 uppercase">Telnet</h2>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" bind:checked={config.telnet_enabled} />
				<span class="text-slate-400">Enabled</span>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Listen address</span>
				<input
					class="rounded border border-slate-700 bg-slate-900 px-3 py-2 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={config.telnet_addr}
					placeholder=":2323"
				/>
			</label>
		</section>

		<section class="flex flex-col gap-4 rounded border border-slate-800 p-4">
			<h2 class="text-sm font-semibold tracking-wide text-cyan-400 uppercase">SSH</h2>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" bind:checked={config.ssh_enabled} />
				<span class="text-slate-400">Enabled</span>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Listen address</span>
				<input
					class="rounded border border-slate-700 bg-slate-900 px-3 py-2 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
					bind:value={config.ssh_addr}
					placeholder=":2222"
				/>
			</label>
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
				Nodes/hubs this system polls to exchange netmail and echomail once BinkP is fully wired
				up. "Test" connects and authenticates now, without sending or requesting any mail.
			</p>
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
						<span class="text-slate-400">Password</span>
						<input
							type="password"
							class="rounded border border-slate-700 bg-slate-900 px-2 py-1 font-mono text-slate-100 focus:border-cyan-500 focus:outline-none"
							bind:value={uplink.password}
							placeholder="(optional -- blank for an open/no-auth node)"
						/>
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
