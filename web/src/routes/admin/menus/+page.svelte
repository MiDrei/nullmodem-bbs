<script lang="ts">
	// The Telnet/SSH menus: their items (key, label, what it does, the
	// lowest level that sees it), the screen shown instead of the
	// generated list, and a preview as a caller or the sysop sees it.
	// Saved menus apply on the caller's next menu, no restart. Labels
	// and the title can come in each language; a stock one is
	// translated by the catalog unless the sysop writes their own.
	import { onMount } from 'svelte';
	import { goto, beforeNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import AnsiArt from '$lib/AnsiArt.svelte';
	import {
		listMenus,
		getMenu,
		saveMenu,
		deleteMenu,
		getMenuActions,
		previewMenu,
		listScreens,
		getLanguages,
		ApiError,
		type Language,
		type MenuDef,
		type MenuItem,
		type MenuBuiltin,
		type Grid
	} from '$lib/api';

	let menus = $state<MenuDef[]>([]);
	let builtins = $state<MenuBuiltin[]>([]);
	let screens = $state<string[]>([]);
	let current = $state<MenuDef | null>(null);
	let saved = $state('');
	let isNew = $state(false);
	let missing = $state<MenuItem[]>([]);
	let usedBy = $state<string[]>([]);
	let previewSL = $state(10);
	let previewLang = $state('en');
	let languages = $state<Language[]>([]);
	// The other languages' fields are shown.
	let translating = $state(false);
	const others = $derived(languages.filter((l) => l.code !== 'en'));
	let preview = $state<{ grid: Grid; has_screen: boolean; not_shown: MenuItem[]; only_on_screen: string[] } | null>(null);
	let previewError = $state('');
	let saving = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;

	const dirty = $derived(current !== null && JSON.stringify(current) !== saved);

	async function failed(err: unknown, fallback: string) {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return;
		}
		toast.push(err instanceof ApiError ? err.message : fallback, 'error');
	}

	async function loadList() {
		if (!auth.token) return;
		menus = await listMenus(auth.token);
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			const [acts, scr, langs] = await Promise.all([getMenuActions(auth.token), listScreens(auth.token), getLanguages(auth.token), loadList()]);
			builtins = acts.builtins;
			languages = langs.languages;
			// A screen's language variants (main.de.ans) are picked by themselves.
			screens = scr.filter((s) => !s.lang).map((s) => s.name).filter((n) => n.endsWith('.ans'));
			await open(page.url.searchParams.get('menu') ?? 'main');
		} catch (err) {
			await failed(err, 'Could not load the menus.');
		}
	});

	beforeNavigate((nav) => {
		if (dirty && !confirm('Leave without saving the menu?')) nav.cancel();
	});

	async function open(name: string) {
		if (!auth.token) return;
		if (dirty && !confirm('Discard the changes to this menu?')) return;
		try {
			const m = await getMenu(auth.token, name);
			current = m.menu;
			saved = JSON.stringify(m.menu);
			missing = m.missing_defaults;
			usedBy = m.used_by;
			isNew = false;
			history.replaceState(history.state, '', `?menu=${encodeURIComponent(name)}`);
			refreshPreview();
		} catch (err) {
			await failed(err, 'Could not load the menu.');
		}
	}

	function newMenu() {
		if (dirty && !confirm('Discard the changes to this menu?')) return;
		const name = prompt('Name of the new menu (lower case, e.g. "games"):')?.trim().toLowerCase();
		if (!name) return;
		if (menus.some((m) => m.name === name)) {
			toast.push(`There's a ${name} menu already.`, 'error');
			return;
		}
		current = { name, title: name[0].toUpperCase() + name.slice(1), screen: '', items: [{ key: 'Q', label: 'Back', action: 'back', min_sl: 0 }] };
		saved = '';
		missing = [];
		usedBy = [];
		isNew = true;
		refreshPreview();
	}

	function refreshPreview() {
		clearTimeout(timer);
		timer = setTimeout(async () => {
			if (!auth.token || !current) return;
			try {
				preview = await previewMenu(auth.token, current, previewSL, previewLang);
				previewError = '';
			} catch (err) {
				previewError = err instanceof ApiError ? err.message : 'No preview.';
			}
		}, 350);
	}

	// Any edit: a new preview.
	$effect(() => {
		JSON.stringify(current);
		previewSL;
		previewLang;
		refreshPreview();
	});

	function move(i: number, by: number) {
		if (!current) return;
		const j = i + by;
		if (j < 0 || j >= current.items.length) return;
		const items = [...current.items];
		[items[i], items[j]] = [items[j], items[i]];
		current.items = items;
	}

	function removeItem(i: number) {
		if (!current) return;
		current.items = current.items.filter((_, k) => k !== i);
	}

	function addItem(it?: MenuItem) {
		if (!current) return;
		const taken = new Set(current.items.map((x) => x.key.toUpperCase()));
		const item: MenuItem = it ? { ...it } : { key: '', label: '', action: 'builtin:who', min_sl: 0 };
		item.labels ??= {};
		if (taken.has(item.key.toUpperCase())) item.key = '';
		// Before a trailing Quit/Back, where new things usually go.
		const last = current.items[current.items.length - 1];
		const at = last && (last.action === 'logoff' || last.action === 'back') ? current.items.length - 1 : current.items.length;
		current.items = [...current.items.slice(0, at), item, ...current.items.slice(at)];
		if (it) missing = missing.filter((m) => m.action !== it.action);
	}

	function actionChanged(it: MenuItem) {
		// A fresh item takes the command's own label.
		const b = builtins.find((x) => 'builtin:' + x.name === it.action);
		if (b && !it.label) it.label = b.label;
	}

	async function save() {
		if (!auth.token || !current) return;
		saving = true;
		try {
			const m = await saveMenu(auth.token, current);
			current = m.menu;
			saved = JSON.stringify(m.menu);
			missing = m.missing_defaults;
			usedBy = m.used_by;
			isNew = false;
			await loadList();
			toast.push(`Saved -- callers see it on their next menu.`, 'success');
		} catch (err) {
			await failed(err, 'Could not save the menu.');
		} finally {
			saving = false;
		}
	}

	async function remove() {
		if (!auth.token || !current) return;
		if (isNew) {
			current = null;
			saved = '';
			await open('main');
			return;
		}
		if (!confirm(`Delete the ${current.name} menu?`)) return;
		try {
			await deleteMenu(auth.token, current.name);
			saved = JSON.stringify(current);
			await loadList();
			await open('main');
		} catch (err) {
			await failed(err, 'Could not delete the menu.');
		}
	}

	const describe = (action: string) => {
		if (action === 'back') return 'Back to the menu this one was opened from';
		if (action === 'logoff') return 'Says goodbye and hangs up';
		if (action.startsWith('goto:')) return `Opens the ${action.slice(5)} menu`;
		const b = builtins.find((x) => 'builtin:' + x.name === action);
		return b ? b.description + (b.sysop ? ' (asks for the sysop’s second factor)' : '') : '';
	};
</script>

<div class="mb-5">
	<h1 class="page-title">Menus</h1>
	<p class="page-subtitle max-w-3xl leading-relaxed">
		What callers can do on Telnet and SSH. Each item has a key, a label, what it does, and the lowest security level that
		sees it. A menu with a screen of its own shows that instead of the generated list -- add new items there too (the
		preview tells you which ones it lacks); a screen can come in each language (main.de.ans). Saved menus apply on the
		caller's next menu.
	</p>
</div>

<div class="grid gap-4 lg:grid-cols-[13rem_1fr]">
	<section class="card self-start p-0">
		<h2 class="card-label px-4 pt-4 pb-2">Menus</h2>
		{#each menus as m (m.name)}
			<button
				class="flex w-full items-center justify-between border-t border-line px-4 py-2.5 text-left text-sm hover:bg-surface {current?.name === m.name ? 'bg-surface text-accent' : 'text-ink-strong'}"
				onclick={() => open(m.name)}
			>
				<span>{m.name}</span>
				<span class="text-xs text-faint">{m.items.length}</span>
			</button>
		{/each}
		{#if isNew && current}
			<div class="border-t border-line bg-surface px-4 py-2.5 text-sm text-accent">{current.name} <span class="text-xs text-faint">new</span></div>
		{/if}
		<div class="border-t border-line p-3">
			<button class="btn-secondary btn-sm w-full" onclick={newMenu}>+ New menu</button>
		</div>
	</section>

	{#if current}
		<div class="flex min-w-0 flex-col gap-4">
			<section class="card">
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="flex flex-col gap-1 text-xs text-muted">
						Title (for the generated list; {'{BBSNAME}'}, {'{NODE}'} … work)
						<input class="field" bind:value={current.title} />
					</label>
					<label class="flex flex-col gap-1 text-xs text-muted">
						Screen
						<div class="flex gap-2">
							<select class="field min-w-0 flex-1" bind:value={current.screen}>
								<option value="">— the generated list —</option>
								{#each screens as s (s)}<option value={s}>{s}</option>{/each}
							</select>
							{#if current.screen}
								<a class="btn-secondary btn-sm shrink-0" href="/admin/designer?screen={encodeURIComponent(current.screen)}">Edit screen</a>
							{/if}
						</div>
					</label>
				</div>

				{#if translating}
					<div class="mt-3 grid gap-3 sm:grid-cols-2">
						{#each others as l (l.code)}
							<label class="flex flex-col gap-1 text-xs text-muted">
								Title in {l.name}
								<input
									class="field"
									value={current.titles?.[l.code] ?? ''}
									oninput={(e) => current && (current.titles = { ...(current.titles ?? {}), [l.code]: e.currentTarget.value })}
									placeholder={current.titles_shown?.[l.code] ?? current.title}
								/>
							</label>
						{/each}
					</div>
				{/if}

				{#if missing.length}
					<div class="mt-4 rounded-lg border border-accent/40 bg-accent/5 p-3 text-sm">
						<div class="mb-2 text-xs text-muted">This version's stock {current.name} menu has, and yours doesn't:</div>
						<div class="flex flex-col gap-1.5">
							{#each missing as it (it.action)}
								<div class="flex items-center gap-3">
									<span class="font-mono text-accent">[{it.key}]</span>
									<span class="min-w-0 flex-1 truncate">{it.label} <span class="text-xs text-faint">· {describe(it.action)}</span></span>
									<button class="btn-primary btn-xs" onclick={() => addItem(it)}>Add</button>
								</div>
							{/each}
						</div>
					</div>
				{/if}

				<div class="mt-4 flex flex-col gap-2">
					<div class="hidden grid-cols-[4.5rem_1fr_1fr_4.5rem_5.5rem] gap-2 px-1 text-[11px] text-faint md:grid">
						<span>Key</span><span>Label</span><span>Does</span><span>SL from</span><span></span>
					</div>
					{#each current.items as it, i (i)}
						<div class="grid grid-cols-[4.5rem_1fr] gap-2 rounded-lg border border-line p-2 md:grid-cols-[4.5rem_1fr_1fr_4.5rem_5.5rem] md:border-0 md:p-0">
							<input class="field text-center font-mono uppercase" maxlength="8" bind:value={it.key} placeholder="?" aria-label="Key" />
							<input class="field" bind:value={it.label} placeholder="Label" aria-label="Label" />
							<select class="field col-span-2 md:col-span-1" bind:value={it.action} onchange={() => actionChanged(it)} title={describe(it.action)} aria-label="Action">
								<optgroup label="Commands">
									{#each builtins as b (b.name)}
										<option value={'builtin:' + b.name}>{b.label}{b.sysop ? ' (sysop)' : ''}</option>
									{/each}
								</optgroup>
								<optgroup label="Menus">
									{#each menus.filter((m) => m.name !== current?.name) as m (m.name)}
										<option value={'goto:' + m.name}>Open the {m.name} menu</option>
									{/each}
								</optgroup>
								<optgroup label="Leave">
									<option value="back">Back to the previous menu</option>
									<option value="logoff">Log off</option>
								</optgroup>
							</select>
							<input class="field" type="number" min="0" max="255" bind:value={it.min_sl} aria-label="Lowest security level" />
							<div class="flex items-center justify-end gap-1">
								<button class="btn-secondary btn-xs" disabled={i === 0} onclick={() => move(i, -1)} aria-label="Up">↑</button>
								<button class="btn-secondary btn-xs" disabled={i === current.items.length - 1} onclick={() => move(i, 1)} aria-label="Down">↓</button>
								<button class="btn-secondary btn-xs" onclick={() => removeItem(i)} aria-label="Remove">✕</button>
							</div>
							{#if translating}
								<div class="col-span-2 grid gap-2 sm:grid-cols-2 md:col-span-5 md:mb-2 md:ml-[5rem]">
									{#each others as l (l.code)}
										<input
											class="field text-sm"
											value={it.labels?.[l.code] ?? ''}
											oninput={(e) => (it.labels = { ...(it.labels ?? {}), [l.code]: e.currentTarget.value })}
											placeholder={`${l.name}: ${it.shown?.[l.code] ?? it.label}`}
											aria-label={`Label in ${l.name}`}
										/>
									{/each}
								</div>
							{/if}
						</div>
					{/each}
					<div class="flex flex-wrap items-center gap-2">
						<button class="btn-secondary btn-sm" onclick={() => addItem()}>+ Add an item</button>
						{#if others.length}
							<button class="btn-secondary btn-sm" onclick={() => (translating = !translating)}>
								{translating ? 'Hide the other languages' : 'Other languages…'}
							</button>
							{#if translating}
								<span class="text-xs text-faint">Empty: callers see the grey text -- the stock label's translation, else the English one.</span>
							{/if}
						{/if}
					</div>
				</div>

				<div class="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-line pt-4">
					<div class="text-xs text-faint">
						{#if usedBy.length}Opened from: {usedBy.join(', ')}{:else if current.name === 'main'}The menu after login{:else if !isNew}No menu leads here yet{/if}
					</div>
					<div class="flex gap-2">
						{#if current.name !== 'main'}
							<button class="btn-secondary btn-sm" onclick={remove} disabled={usedBy.length > 0} title={usedBy.length ? 'Remove the items leading here first' : ''}>
								{isNew ? 'Discard' : 'Delete menu'}
							</button>
						{/if}
						{#if dirty && !isNew}
							<button class="btn-secondary btn-sm" onclick={() => current && open(current.name)}>Revert</button>
						{/if}
						<button class="btn-primary btn-sm" disabled={saving || !dirty} onclick={save}>{saving ? 'Saving…' : isNew ? 'Create menu' : 'Save'}</button>
					</div>
				</div>
			</section>

			<section class="card">
				<div class="mb-3 flex flex-wrap items-center justify-between gap-3">
					<h2 class="card-label">Preview</h2>
					<div class="flex flex-wrap gap-1">
						{#each [[10, 'A caller'], [255, 'The sysop']] as [sl, label] (sl)}
							<button class="pill {previewSL === sl ? 'pill-active' : ''}" onclick={() => (previewSL = sl as number)}>{label}</button>
						{/each}
						{#if languages.length > 1}
							<span class="mx-1 w-px self-stretch bg-line"></span>
							{#each languages as l (l.code)}
								<button class="pill {previewLang === l.code ? 'pill-active' : ''}" onclick={() => (previewLang = l.code)}>{l.name}</button>
							{/each}
						{/if}
					</div>
				</div>
				{#if previewError}
					<p class="text-sm text-amber-300">{previewError}</p>
				{:else if preview}
					<div class="ansi-panel">
						<AnsiArt grid={preview.grid} fit maxZoom={1.4} />
					</div>
					{#if preview.not_shown.length}
						<div class="mt-3 rounded-lg border border-amber-500/40 bg-amber-500/5 p-3 text-sm">
							<div class="mb-1.5 text-amber-300">The screen doesn't seem to show:</div>
							<div class="flex flex-wrap gap-x-4 gap-y-1">
								{#each preview.not_shown as it (it.key)}
									<span><span class="font-mono text-accent">[{it.key}]</span> {it.label}</span>
								{/each}
							</div>
							<div class="mt-1.5 text-xs text-muted">
								Callers can still use these keys, but won't know.
								{#if current.screen}<a class="text-accent hover:underline" href="/admin/designer?screen={encodeURIComponent(current.screen)}">Add them in the designer</a>.{/if}
							</div>
						</div>
					{/if}
					{#if preview.only_on_screen.length}
						<div class="mt-3 rounded-lg border border-amber-500/40 bg-amber-500/5 p-3 text-sm">
							<span class="text-amber-300">The screen shows</span>
							{#each preview.only_on_screen as k (k)}<span class="ml-1.5 font-mono text-accent">[{k}]</span>{/each}
							<span class="text-amber-300">, but {previewSL >= 200 ? 'the sysop' : 'a caller'} can't use {preview.only_on_screen.length === 1 ? 'it' : 'them'}</span>
							<div class="mt-1 text-xs text-muted">Pressing it only gets "Unknown command." -- add the item, or take it off the screen.</div>
						</div>
					{/if}
					{#if preview.has_screen && !preview.not_shown.length && !preview.only_on_screen.length}
						<p class="mt-3 text-xs text-muted">Screen and items match.</p>
					{/if}
				{/if}
			</section>
		</div>
	{/if}
</div>
