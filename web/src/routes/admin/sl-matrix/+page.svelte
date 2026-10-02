<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listMenus,
		listMessageAreas,
		listFileAreas,
		setMenuItemSL,
		ApiError,
		type MenuDef,
		type MessageArea,
		type FileArea
	} from '$lib/api';

	// Mirrors internal/user.SLNewUser / SLSysop.
	const SL_NEW_USER = 10;
	const SL_SYSOP = 255;

	type Row = {
		sl: number;
		kind: 'menu' | 'msg-read' | 'msg-write' | 'file-download' | 'file-upload';
		resource: string;
		detail: string;
		editKey?: string; // "menuName/itemKey" when editable
		linkHref?: string;
	};

	let menus = $state<MenuDef[]>([]);
	let messageAreas = $state<MessageArea[]>([]);
	let fileAreas = $state<FileArea[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	// Pending edits for menu-item SL inputs, keyed by "menuName/itemKey".
	let edits = $state<Record<string, number>>({});
	let saving = $state<Record<string, boolean>>({});

	async function load() {
		if (!auth.token) return;
		try {
			[menus, messageAreas, fileAreas] = await Promise.all([
				listMenus(auth.token),
				listMessageAreas(auth.token),
				listFileAreas(auth.token)
			]);
			loadError = null;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : 'Could not load the SL matrix.';
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

	function rowsFor(): Row[] {
		const rows: Row[] = [];
		for (const m of menus) {
			for (const item of m.items) {
				const editKey = `${m.name}/${item.key}`;
				rows.push({
					sl: edits[editKey] ?? item.min_sl,
					kind: 'menu',
					resource: `${m.title} (${m.name})`,
					detail: `[${item.key}] ${item.label} → ${item.action}`,
					editKey
				});
			}
		}
		for (const a of messageAreas) {
			rows.push({
				sl: a.min_sl_read,
				kind: 'msg-read',
				resource: `${a.name} (${a.tag})`,
				detail: 'Message area — read',
				linkHref: '/admin/message-areas'
			});
			rows.push({
				sl: a.min_sl_write,
				kind: 'msg-write',
				resource: `${a.name} (${a.tag})`,
				detail: 'Message area — write',
				linkHref: '/admin/message-areas'
			});
		}
		for (const a of fileAreas) {
			rows.push({
				sl: a.min_sl_download,
				kind: 'file-download',
				resource: `${a.name} (${a.tag})`,
				detail: 'File area — download',
				linkHref: '/admin/file-areas'
			});
			rows.push({
				sl: a.min_sl_upload,
				kind: 'file-upload',
				resource: `${a.name} (${a.tag})`,
				detail: 'File area — upload',
				linkHref: '/admin/file-areas'
			});
		}
		return rows.sort((a, b) => a.sl - b.sl || a.resource.localeCompare(b.resource));
	}

	let rows = $derived(rowsFor());

	const kindLabels: Record<Row['kind'], string> = {
		menu: 'Menu item',
		'msg-read': 'Message read',
		'msg-write': 'Message write',
		'file-download': 'File download',
		'file-upload': 'File upload'
	};

	const kindClasses: Record<Row['kind'], string> = {
		menu: 'bg-cyan-950 text-cyan-400',
		'msg-read': 'bg-slate-800 text-slate-400',
		'msg-write': 'bg-slate-800 text-slate-300',
		'file-download': 'bg-slate-800 text-slate-400',
		'file-upload': 'bg-slate-800 text-slate-300'
	};

	async function saveMenuItem(editKey: string) {
		if (!auth.token) return;
		const [menuName, itemKey] = editKey.split('/');
		const value = edits[editKey];
		if (value === undefined) return;
		saving = { ...saving, [editKey]: true };
		try {
			const result = await setMenuItemSL(auth.token, menuName, itemKey, value);
			menus = menus.map((m) => (m.name === menuName ? result.menu : m));
			delete edits[editKey];
			edits = { ...edits };
			toast.push(`Saved ${menuName}/${itemKey} -- callers see it on their next menu.`, 'success');
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			toast.push(err instanceof ApiError ? err.message : 'Could not save.', 'error');
		} finally {
			const rest = { ...saving };
			delete rest[editKey];
			saving = rest;
		}
	}
</script>

<div class="mb-6">
	<h1 class="page-title">SL Matrix</h1>
	<p class="mt-1 text-sm text-slate-500">
		Every SL-gated resource in one place, sorted by required security level. New-user SL is
		<span class="font-mono text-slate-300">{SL_NEW_USER}</span>, sysop SL is
		<span class="font-mono text-amber-400">{SL_SYSOP}</span>. Menu items are editable here; areas link
		to their own pages.
	</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else}
	<div class="overflow-x-auto rounded-xl border border-line">
		<table class="w-full text-left text-sm">
			<thead class="card-label">
				<tr class="border-b border-slate-800">
					<th class="p-3">Min SL</th>
					<th class="p-3">Type</th>
					<th class="p-3">Resource</th>
					<th class="p-3">Detail</th>
					<th class="p-3"></th>
				</tr>
			</thead>
			<tbody>
				{#each rows as row (row.editKey ?? `${row.kind}:${row.resource}:${row.detail}`)}
					<tr class="border-b border-line align-top {row.sl >= SL_SYSOP ? 'bg-amber-950/20' : ''}">
						<td class="p-3">
							{#if row.editKey}
								<input
									type="number"
									min="0"
									max="255"
									class="w-20 field field-sm"
									value={row.sl}
									oninput={(e) =>
										(edits = { ...edits, [row.editKey!]: Number((e.target as HTMLInputElement).value) })}
								/>
							{:else}
								<span class="font-mono {row.sl >= SL_SYSOP ? 'text-amber-400' : 'text-slate-100'}"
									>{row.sl}</span
								>
							{/if}
						</td>
						<td class="p-3">
							<span class="rounded px-1.5 py-0.5 text-[10px] tracking-wide uppercase {kindClasses[row.kind]}"
								>{kindLabels[row.kind]}</span
							>
						</td>
						<td class="p-3 text-slate-100">{row.resource}</td>
						<td class="p-3 text-slate-400">
							{#if row.linkHref}
								<a href={row.linkHref} class="hover:text-cyan-400 hover:underline">{row.detail}</a>
							{:else}
								{row.detail}
							{/if}
						</td>
						<td class="p-3">
							{#if row.editKey}
								<button
									class="btn-primary btn-sm"
									disabled={saving[row.editKey] || edits[row.editKey] === undefined}
									onclick={() => saveMenuItem(row.editKey!)}
								>
									{saving[row.editKey] ? 'Saving…' : 'Save'}
								</button>
							{/if}
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
