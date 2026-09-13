<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { getConfig, putConfig, ApiError, type BBSConfig } from '$lib/api';

	let config = $state<BBSConfig | null>(null);
	let loadError = $state<string | null>(null);
	let saveError = $state<string | null>(null);
	let saveNote = $state<string | null>(null);
	let saving = $state(false);

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
