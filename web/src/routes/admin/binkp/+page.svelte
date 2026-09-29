<script lang="ts">
	// The FTN networks this system belongs to, each with its own
	// address(es) in it. Stored as before: config.networks (name and
	// domain) and config.ftn_addresses (full 5D addresses, primary
	// first) -- this page only edits them together, appending the
	// network's domain to each address when saving.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { getConfig, putConfig, addressDomain, ApiError, type BBSConfig } from '$lib/api';

	interface NetworkRow {
		name: string;
		domain: string;
		original_name?: string;
		// Addresses without their "@domain".
		addresses: string[];
	}

	let config = $state<BBSConfig | null>(null);
	let rows = $state<NetworkRow[]>([]);
	// Addresses whose domain matches no network, kept as typed.
	let orphans = $state<string[]>([]);
	// The primary address as "n:<row>:<index>" or "o:<index>".
	let primary = $state('');
	let loadError = $state<string | null>(null);
	let saveError = $state<string | null>(null);
	let saving = $state(false);
	let saved = $state(false);

	function stripDomain(addr: string): string {
		const i = addr.lastIndexOf('@');
		return (i >= 0 ? addr.slice(0, i) : addr).trim();
	}

	function fromConfig(c: BBSConfig) {
		const byDomain = new Map<string, number>();
		const newRows: NetworkRow[] = c.networks.map((n, i) => {
			byDomain.set(n.domain.toLowerCase(), i);
			return { name: n.name, domain: n.domain, original_name: n.original_name, addresses: [] };
		});
		const newOrphans: string[] = [];
		let newPrimary = '';
		c.ftn_addresses.forEach((addr, k) => {
			const row = byDomain.get(addressDomain(addr));
			let key: string;
			if (row === undefined) {
				newOrphans.push(addr);
				key = `o:${newOrphans.length - 1}`;
			} else {
				newRows[row].addresses.push(stripDomain(addr));
				key = `n:${row}:${newRows[row].addresses.length - 1}`;
			}
			if (k === 0) newPrimary = key;
		});
		// A network without an address still gets an (empty) field.
		for (const r of newRows) if (r.addresses.length === 0) r.addresses.push('');
		rows = newRows;
		orphans = newOrphans;
		primary = newPrimary;
	}

	// The ftn_addresses list to save: the primary first, then the rest
	// in table order; empty fields are left out.
	function toAddresses(): string[] {
		const all: { key: string; addr: string }[] = [];
		rows.forEach((r, i) =>
			r.addresses.forEach((a, j) => {
				if (a.trim()) all.push({ key: `n:${i}:${j}`, addr: `${stripDomain(a)}@${r.domain.trim().toLowerCase()}` });
			})
		);
		orphans.forEach((a, k) => {
			if (a.trim()) all.push({ key: `o:${k}`, addr: a.trim() });
		});
		const p = all.findIndex((x) => x.key === primary);
		if (p > 0) all.unshift(all.splice(p, 1)[0]);
		return all.map((x) => x.addr);
	}

	function addNetwork() {
		rows = [...rows, { name: '', domain: '', addresses: [''] }];
	}

	function removeNetwork(i: number) {
		const r = rows[i];
		const used = r.addresses.some((a) => a.trim());
		if (used && !confirm(`Remove ${r.name || 'this network'} and your address(es) in it?`)) return;
		if (primary.startsWith(`n:${i}:`)) primary = '';
		rows = rows.filter((_, j) => j !== i);
		// Keys of later rows shift down by one.
		const m = /^n:(\d+):(\d+)$/.exec(primary);
		if (m && Number(m[1]) > i) primary = `n:${Number(m[1]) - 1}:${m[2]}`;
	}

	function addAddress(i: number) {
		rows[i].addresses = [...rows[i].addresses, ''];
	}

	function removeAddress(i: number, j: number) {
		if (primary === `n:${i}:${j}`) primary = '';
		rows[i].addresses = rows[i].addresses.filter((_, k) => k !== j);
		if (rows[i].addresses.length === 0) rows[i].addresses = [''];
	}

	function removeOrphan(k: number) {
		if (primary === `o:${k}`) primary = '';
		orphans = orphans.filter((_, j) => j !== k);
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			config = await getConfig(auth.token);
			fromConfig(config);
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : 'Could not load configuration.';
		}
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (!config || !auth.token) return;
		saveError = null;
		saved = false;
		saving = true;
		try {
			const res = await putConfig(auth.token, {
				...$state.snapshot(config),
				networks: rows.map((r) => ({
					name: r.name.trim(),
					domain: r.domain.trim().toLowerCase(),
					original_name: r.original_name
				})),
				ftn_addresses: toAddresses()
			});
			config = res.config;
			fromConfig(res.config);
			saved = true;
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
</script>

<div class="mb-6 flex items-center justify-between">
	<h1 class="page-title">BinkP</h1>
	<a href="/admin/binkp/uplinks" class="btn-secondary btn-sm">Uplinks (Nodes/Points) &rarr;</a>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !config}
	<p class="text-sm text-muted">Loading…</p>
{:else}
	<form class="flex flex-col gap-6" onsubmit={handleSubmit}>
		<section class="flex flex-col gap-4 rounded-xl border border-line p-4">
			<div class="flex items-center justify-between">
				<h2 class="card-label">Networks and your addresses</h2>
				<button type="button" class="btn-secondary btn-xs" onclick={addNetwork}>+ Add Network</button>
			</div>
			<p class="text-xs leading-relaxed text-muted">
				The FTN networks you belong to and your node address in each. The name is what an uplink
				and its areas are grouped under (tabs in the admin and portal, dividers in the Telnet area
				lists); the domain is appended to your address (<span class="font-mono">21:3/194</span> in
				fsxnet becomes <span class="font-mono">21:3/194@fsxnet</span>). The primary address is
				stamped on netmail written here. Renaming a network renames it on its uplinks and areas too.
			</p>

			{#if rows.length === 0}
				<p class="text-sm text-muted">No networks yet.</p>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full min-w-[40rem] text-left text-[13px]">
						<thead class="card-label">
							<tr>
								<th class="w-16 pb-2 text-center font-normal">Primary</th>
								<th class="w-44 pr-3 pb-2 font-normal">Name</th>
								<th class="w-36 pr-3 pb-2 font-normal">Domain</th>
								<th class="pr-3 pb-2 font-normal">Your address</th>
								<th class="w-20 pb-2"></th>
							</tr>
						</thead>
						<tbody>
							{#each rows as r, i (i)}
								<tr class="border-t border-line align-top">
									<td class="py-2">
										<div class="flex flex-col items-center">
											{#each r.addresses as addr, j (j)}
												<label class="flex h-[2.1rem] items-center justify-center" title="Primary address">
													<input
														type="radio"
														name="primary"
														class="h-4 w-4 accent-[var(--color-accent)]"
														checked={primary === `n:${i}:${j}`}
														onchange={() => (primary = `n:${i}:${j}`)}
														disabled={!addr.trim()}
													/>
												</label>
											{/each}
										</div>
									</td>
									<td class="py-2 pr-3">
										<input class="field field-sm" bind:value={r.name} placeholder="fsxNet" required />
									</td>
									<td class="py-2 pr-3">
										<input class="field field-sm font-mono" bind:value={r.domain} placeholder="fsxnet" required />
									</td>
									<td class="py-2 pr-3">
										<div class="flex flex-col gap-0.5">
											{#each r.addresses as _, j (j)}
												<div class="flex h-[2.1rem] items-center gap-2">
													<input
														class="field field-sm flex-1 font-mono"
														bind:value={r.addresses[j]}
														placeholder="21:3/194"
													/>
													<span class="w-24 truncate font-mono text-xs text-faint"
														>@{r.domain.trim().toLowerCase() || '…'}</span
													>
													<button
														type="button"
														class="w-4 text-xs text-faint hover:text-red-400 {r.addresses.length > 1 ? '' : 'invisible'}"
														onclick={() => removeAddress(i, j)}
														title="Remove this address">✕</button
													>
												</div>
											{/each}
											<button
												type="button"
												class="mt-1 self-start text-xs text-faint hover:text-accent"
												onclick={() => addAddress(i)}
											>
												+ Address
											</button>
										</div>
									</td>
									<td class="py-2 text-right">
										<button type="button" class="btn-danger btn-xs mt-1" onclick={() => removeNetwork(i)}
											>Remove</button
										>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>

		{#if orphans.length > 0}
			<section class="flex flex-col gap-3 rounded-xl border border-amber-500/40 p-4">
				<h2 class="card-label">Addresses without a network</h2>
				<p class="text-xs text-muted">
					Their domain matches none of the networks above. Add the network, or remove the address.
				</p>
				{#each orphans as _, k (k)}
					<div class="flex items-center gap-3">
						<label class="flex w-16 justify-center" title="Primary address">
							<input
								type="radio"
								name="primary"
								class="h-4 w-4 accent-[var(--color-accent)]"
								checked={primary === `o:${k}`}
								onchange={() => (primary = `o:${k}`)}
							/>
						</label>
						<input class="field field-sm flex-1 font-mono" bind:value={orphans[k]} />
						<button type="button" class="btn-danger btn-xs" onclick={() => removeOrphan(k)}>Remove</button>
					</div>
				{/each}
			</section>
		{/if}

		{#if saveError}
			<p class="text-sm text-red-400">{saveError}</p>
		{/if}
		{#if saved}
			<p class="text-sm text-muted">Saved.</p>
		{/if}

		<button type="submit" disabled={saving} class="btn-primary">
			{saving ? 'Saving…' : 'Save changes'}
		</button>
	</form>
{/if}
