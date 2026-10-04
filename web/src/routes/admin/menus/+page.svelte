<script lang="ts">
	import { t } from '$lib/i18n.svelte';
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
			await failed(err, t('admin.menus.could_not_load_the_menus'));
		}
	});

	beforeNavigate((nav) => {
		if (dirty && !confirm(t('admin.menus.leave_without_saving_the_menu'))) nav.cancel();
	});

	async function open(name: string) {
		if (!auth.token) return;
		if (dirty && !confirm(t('admin.menus.discard_the_changes_to_this'))) return;
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
			await failed(err, t('admin.menus.could_not_load_the_menu'));
		}
	}

	function newMenu() {
		if (dirty && !confirm(t('admin.menus.discard_the_changes_to_this'))) return;
		const name = prompt(t('admin.menus.name_of_the_new_menu'))?.trim().toLowerCase();
		if (!name) return;
		if (menus.some((m) => m.name === name)) {
			toast.push(t('admin.menus.there_s_a_name_menu', { NAME: name }), 'error');
			return;
		}
		current = { name, title: name[0].toUpperCase() + name.slice(1), screen: '', items: [{ key: 'Q', label: t('admin.common.back'), action: 'back', min_sl: 0 }] };
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
				previewError = err instanceof ApiError ? err.message : t('admin.menus.no_preview');
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
			toast.push(t('admin.menus.saved_callers_see_it_on'), 'success');
		} catch (err) {
			await failed(err, t('admin.menus.could_not_save_the_menu'));
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
		if (!confirm(t('admin.menus.delete_the_name_menu', { NAME: current.name }))) return;
		try {
			await deleteMenu(auth.token, current.name);
			saved = JSON.stringify(current);
			await loadList();
			await open('main');
		} catch (err) {
			await failed(err, t('admin.menus.could_not_delete_the_menu'));
		}
	}

	const describe = (action: string) => {
		if (action === 'back') return t('admin.menus.back_to_the_menu_this');
		if (action === 'logoff') return t('admin.menus.says_goodbye_and_hangs_up');
		if (action.startsWith('goto:')) return t('admin.menus.opens_the_v_menu', { V: action.slice(5) });
		const b = builtins.find((x) => 'builtin:' + x.name === action);
		return b ? b.description + (b.sysop ? t('admin.menus.asks_for_the_sysop_s') : '') : '';
	};
</script>

<div class="mb-5">
	<h1 class="page-title">{t('admin.menus.menus')}</h1>
	<p class="page-subtitle max-w-3xl leading-relaxed">
		{t('admin.menus.what_callers_can_do_on')}
	</p>
</div>

<div class="grid gap-4 lg:grid-cols-[13rem_1fr]">
	<section class="card self-start p-0">
		<h2 class="card-label px-4 pt-4 pb-2">{t('admin.menus.menus')}</h2>
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
			<div class="border-t border-line bg-surface px-4 py-2.5 text-sm text-accent">{current.name} <span class="text-xs text-faint">{t('admin.menus.new')}</span></div>
		{/if}
		<div class="border-t border-line p-3">
			<button class="btn-secondary btn-sm w-full" onclick={newMenu}>{t('admin.menus.new_menu')}</button>
		</div>
	</section>

	{#if current}
		<div class="flex min-w-0 flex-col gap-4">
			<section class="card">
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="flex flex-col gap-1 text-xs text-muted">
						{t('admin.menus.title_for_the_generated_list', { V: '{BBSNAME}', V2: '{NODE}' })}
						<input class="field" bind:value={current.title} />
					</label>
					<label class="flex flex-col gap-1 text-xs text-muted">
						{t('admin.menus.screen')}
						<div class="flex gap-2">
							<select class="field min-w-0 flex-1" bind:value={current.screen}>
								<option value="">{t('admin.menus.the_generated_list')}</option>
								{#each screens as s (s)}<option value={s}>{s}</option>{/each}
							</select>
							{#if current.screen}
								<a class="btn-secondary btn-sm shrink-0" href="/admin/designer?screen={encodeURIComponent(current.screen)}">{t('admin.menus.edit_screen')}</a>
							{/if}
						</div>
					</label>
				</div>

				{#if translating}
					<div class="mt-3 grid gap-3 sm:grid-cols-2">
						{#each others as l (l.code)}
							<label class="flex flex-col gap-1 text-xs text-muted">
								{t('admin.menus.title_in_name', { NAME: l.name })}
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
						<div class="mb-2 text-xs text-muted">{t('admin.menus.this_version_s_stock_name', { NAME: current.name })}</div>
						<div class="flex flex-col gap-1.5">
							{#each missing as it (it.action)}
								<div class="flex items-center gap-3">
									<span class="font-mono text-accent">[{it.key}]</span>
									<span class="min-w-0 flex-1 truncate">{it.label} <span class="text-xs text-faint">· {describe(it.action)}</span></span>
									<button class="btn-primary btn-xs" onclick={() => addItem(it)}>{t('admin.common.add')}</button>
								</div>
							{/each}
						</div>
					</div>
				{/if}

				<div class="mt-4 flex flex-col gap-2">
					<div class="hidden grid-cols-[4.5rem_1fr_1fr_4.5rem_5.5rem] gap-2 px-1 text-[11px] text-faint md:grid">
						<span>{t('admin.menus.key')}</span><span>{t('admin.menus.label')}</span><span>{t('admin.menus.does')}</span><span>{t('admin.menus.sl_from')}</span><span></span>
					</div>
					{#each current.items as it, i (i)}
						<div class="grid grid-cols-[4.5rem_1fr] gap-2 rounded-lg border border-line p-2 md:grid-cols-[4.5rem_1fr_1fr_4.5rem_5.5rem] md:border-0 md:p-0">
							<input class="field text-center font-mono uppercase" maxlength="8" bind:value={it.key} placeholder="?" aria-label={t('admin.menus.key')} />
							<input class="field" bind:value={it.label} placeholder={t('admin.menus.label')} aria-label={t('admin.menus.label')} />
							<select class="field col-span-2 md:col-span-1" bind:value={it.action} onchange={() => actionChanged(it)} title={describe(it.action)} aria-label={t('admin.menus.action')}>
								<optgroup label={t('admin.menus.commands')}>
									{#each builtins as b (b.name)}
										<option value={'builtin:' + b.name}>{b.label}{b.sysop ? t('admin.menus.sysop') : ''}</option>
									{/each}
								</optgroup>
								<optgroup label={t('admin.menus.menus')}>
									{#each menus.filter((m) => m.name !== current?.name) as m (m.name)}
										<option value={'goto:' + m.name}>{t('admin.menus.open_the_name_menu', { NAME: m.name })}</option>
									{/each}
								</optgroup>
								<optgroup label={t('admin.menus.leave')}>
									<option value="back">{t('admin.menus.back_to_the_previous_menu')}</option>
									<option value="logoff">{t('admin.menus.log_off')}</option>
								</optgroup>
							</select>
							<input class="field" type="number" min="0" max="255" bind:value={it.min_sl} aria-label={t('admin.menus.lowest_security_level')} />
							<div class="flex items-center justify-end gap-1">
								<button class="btn-secondary btn-xs" disabled={i === 0} onclick={() => move(i, -1)} aria-label={t('admin.menus.up')}>↑</button>
								<button class="btn-secondary btn-xs" disabled={i === current.items.length - 1} onclick={() => move(i, 1)} aria-label={t('admin.menus.down')}>↓</button>
								<button class="btn-secondary btn-xs" onclick={() => removeItem(i)} aria-label={t('admin.common.remove')}>✕</button>
							</div>
							{#if translating}
								<div class="col-span-2 grid gap-2 sm:grid-cols-2 md:col-span-5 md:mb-2 md:ml-[5rem]">
									{#each others as l (l.code)}
										<input
											class="field text-sm"
											value={it.labels?.[l.code] ?? ''}
											oninput={(e) => (it.labels = { ...(it.labels ?? {}), [l.code]: e.currentTarget.value })}
											placeholder={`${l.name}: ${it.shown?.[l.code] ?? it.label}`}
											aria-label={t('admin.menus.label_in_name', { NAME: l.name })}
										/>
									{/each}
								</div>
							{/if}
						</div>
					{/each}
					<div class="flex flex-wrap items-center gap-2">
						<button class="btn-secondary btn-sm" onclick={() => addItem()}>{t('admin.menus.add_an_item')}</button>
						{#if others.length}
							<button class="btn-secondary btn-sm" onclick={() => (translating = !translating)}>
								{translating ? t('admin.menus.hide_the_other_languages') : t('admin.menus.other_languages')}
							</button>
							{#if translating}
								<span class="text-xs text-faint">{t('admin.menus.empty_callers_see_the_grey')}</span>
							{/if}
						{/if}
					</div>
				</div>

				<div class="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-line pt-4">
					<div class="text-xs text-faint">
						{#if usedBy.length}{t('admin.menus.opened_from_v', { V: usedBy.join(', ') })}{:else if current.name === 'main'}{t('admin.menus.the_menu_after_login')}{:else if !isNew}{t('admin.menus.no_menu_leads_here_yet')}{/if}
					</div>
					<div class="flex gap-2">
						{#if current.name !== 'main'}
							<button class="btn-secondary btn-sm" onclick={remove} disabled={usedBy.length > 0} title={usedBy.length ? t('admin.menus.remove_the_items_leading_here') : ''}>
								{isNew ? t('admin.menus.discard') : t('admin.menus.delete_menu')}
							</button>
						{/if}
						{#if dirty && !isNew}
							<button class="btn-secondary btn-sm" onclick={() => current && open(current.name)}>{t('admin.common.revert')}</button>
						{/if}
						<button class="btn-primary btn-sm" disabled={saving || !dirty} onclick={save}>{saving ? t('admin.common.saving') : isNew ? t('admin.menus.create_menu') : t('admin.common.save')}</button>
					</div>
				</div>
			</section>

			<section class="card">
				<div class="mb-3 flex flex-wrap items-center justify-between gap-3">
					<h2 class="card-label">{t('admin.menus.preview')}</h2>
					<div class="flex flex-wrap gap-1">
						{#each [[10, t('admin.menus.a_caller')], [255, t('admin.menus.the_sysop')]] as [sl, label] (sl)}
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
							<div class="mb-1.5 text-amber-300">{t('admin.menus.the_screen_doesn_t_seem')}</div>
							<div class="flex flex-wrap gap-x-4 gap-y-1">
								{#each preview.not_shown as it (it.key)}
									<span><span class="font-mono text-accent">[{it.key}]</span> {it.label}</span>
								{/each}
							</div>
							<div class="mt-1.5 text-xs text-muted">
								{t('admin.menus.callers_can_still_use_these')}
								{#if current.screen}<a class="text-accent hover:underline" href="/admin/designer?screen={encodeURIComponent(current.screen)}">{t('admin.menus.add_them_in_the_designer')}</a>.{/if}
							</div>
						</div>
					{/if}
					{#if preview.only_on_screen.length}
						<div class="mt-3 rounded-lg border border-amber-500/40 bg-amber-500/5 p-3 text-sm">
							<span class="text-amber-300">{t('admin.menus.the_screen_shows')}</span>
							{#each preview.only_on_screen as k (k)}<span class="ml-1.5 font-mono text-accent">[{k}]</span>{/each}
							<span class="text-amber-300">{t('admin.menus.but_v_can_t_use', { V: previewSL >= 200 ? t('admin.menus.the_sysop_lower') : t('admin.menus.a_caller_lower'), V2: preview.only_on_screen.length === 1 ? t('admin.menus.it') : t('admin.menus.them') })}</span>
							<div class="mt-1 text-xs text-muted">{t('admin.menus.pressing_it_only_gets_unknown')}</div>
						</div>
					{/if}
					{#if preview.has_screen && !preview.not_shown.length && !preview.only_on_screen.length}
						<p class="mt-3 text-xs text-muted">{t('admin.menus.screen_and_items_match')}</p>
					{/if}
				{/if}
			</section>
		</div>
	{/if}
</div>
