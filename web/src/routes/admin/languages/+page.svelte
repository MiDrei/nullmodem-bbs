<script lang="ts">
	import { t } from '$lib/i18n.svelte';
	// The language editor: the board's own language, and every text of
	// every language -- the built-in one, or the sysop's own. {NAME}s are
	// placeholders the board fills in; a text may leave one out, not make
	// one up. Saved texts apply right away (Telnet and the web).
	import { onMount } from 'svelte';
	import { goto, beforeNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { getLanguages, setBoardLanguage, getCatalog, saveCatalog, ApiError, type Languages, type CatalogText } from '$lib/api';

	let langs = $state<Languages | null>(null);
	let lang = $state('de');
	let texts = $state<CatalogText[]>([]);
	// key -> the sysop's own text, as edited
	let own = $state<Record<string, string>>({});
	let saved = $state('{}');
	let query = $state('');
	let filter = $state<'all' | 'changed' | 'inherited'>('all');
	let saving = $state(false);
	let loading = $state(false);

	const dirty = $derived(JSON.stringify(clean(own)) !== saved);

	// The groups, by the keys' first part.
	const groupNames: Record<string, string> = {
		common: t('admin.languages.group.shared'),
		list: t('admin.languages.group.lists'),
		read: t('admin.languages.group.lists'),
		col: t('admin.languages.group.column_heads'),
		approval: t('admin.languages.group.new_accounts'),
		guard: t('admin.languages.group.refused_before_login'),
		login: t('admin.languages.group.login'),
		register: t('admin.languages.group.registration'),
		lang: t('admin.languages.group.choosing_a_language'),
		menu: t('admin.languages.group.menus'),
		logoff: t('admin.languages.group.logoff'),
		who: "Who's online, node messages",
		sysop: t('admin.languages.group.sysop_functions'),
		profile: t('admin.languages.group.profile'),
		doors: t('admin.languages.group.doors'),
		polls: t('admin.languages.group.voting_booth'),
		files: t('admin.languages.group.files'),
		transfer: t('admin.languages.group.file_transfers'),
		qwk: t('admin.languages.group.qwk'),
		msg: t('admin.languages.group.writing_messages'),
		editor: t('admin.languages.group.line_editor'),
		fse: t('admin.languages.group.full_screen_editor'),
		areas: t('admin.languages.group.message_areas'),
		msgs: t('admin.languages.group.message_areas'),
		myareas: t('admin.languages.group.my_areas'),
		bbslist: t('admin.languages.group.bbs_list'),
		scan: t('admin.languages.group.new_scan'),
		summary: t('admin.languages.group.after_login'),
		search: t('admin.languages.group.message_search'),
		oneliners: t('admin.languages.group.one_liners'),
		chat: t('admin.languages.group.chat'),
		netmail: t('admin.languages.group.netmail'),
		nodelist: t('admin.languages.group.nodelist'),
		lastcallers: t('admin.languages.group.last_callers'),
		screen: t('admin.languages.group.screens_t_key_in_ans'),
		api: t('admin.languages.group.web_error_messages'),
		push: t('admin.languages.push_notifications')
	};
	// The web's texts (web.<part>.<name>), by their part.
	const webNames: Record<string, string> = {
		common: 'shared',
		nav: 'navigation',
		footer: 'navigation',
		time: 'shared',
		login: 'login',
		areas: t('admin.languages.group.message_areas_2'),
		msgs: t('admin.languages.group.message_areas_2'),
		msg: 'messages',
		netmail: 'netmail',
		files: 'files',
		file: 'files',
		qwk: t('admin.languages.group.qwk'),
		chat: 'chat',
		community: 'community',
		polls: 'community',
		bbslist: 'community',
		callers: 'community',
		profile: 'profile',
		search: 'search',
		share: t('admin.languages.group.shared_files'),
		terminal: 'terminal',
		home: t('admin.languages.group.front_page'),
		stats: 'statistics',
		push: t('admin.languages.group.reader_app'),
		reader: t('admin.languages.group.reader_app')
	};
	function groupOf(key: string) {
		const [first, second] = key.split('.');
		if (first === 'web') return t('admin.languages.web_v', { V: webNames[second] ?? second });
		if (first === 'admin') return t('admin.languages.admin_v', { V: second.replace(/_/g, ' ') });
		return groupNames[first] ?? first;
	}

	function clean(m: Record<string, string>) {
		const out: Record<string, string> = {};
		for (const [k, v] of Object.entries(m)) if (v.trim() !== '') out[k] = v;
		return out;
	}

	async function failed(err: unknown, fallback: string) {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return;
		}
		toast.push(err instanceof ApiError ? err.message : fallback, 'error');
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			langs = await getLanguages(auth.token);
			const want = page.url.searchParams.get('lang');
			lang = want && langs.languages.some((l) => l.code === want) ? want : langs.languages.find((l) => l.code !== langs!.fallback)?.code ?? langs.fallback;
			await load();
		} catch (err) {
			await failed(err, t('admin.languages.could_not_load_the_languages'));
		}
	});

	beforeNavigate((nav) => {
		if (dirty && !confirm(t('admin.languages.leave_without_saving_the_texts'))) nav.cancel();
	});

	async function load() {
		if (!auth.token) return;
		loading = true;
		try {
			texts = await getCatalog(auth.token, lang);
			own = Object.fromEntries(texts.filter((x) => x.text !== '').map((x) => [x.key, x.text]));
			saved = JSON.stringify(clean(own));
			history.replaceState(history.state, '', `?lang=${encodeURIComponent(lang)}`);
		} finally {
			loading = false;
		}
	}

	async function pick(code: string) {
		if (code === lang) return;
		if (dirty && !confirm(t('admin.languages.discard_the_changes_to_these'))) return;
		lang = code;
		try {
			await load();
		} catch (err) {
			await failed(err, t('admin.languages.could_not_load_the_texts'));
		}
	}

	async function save() {
		if (!auth.token) return;
		saving = true;
		try {
			await saveCatalog(auth.token, lang, clean(own));
			await load();
			if (langs) langs.changed[lang] = Object.keys(clean(own)).length;
			toast.push(t('admin.languages.saved_callers_see_it_right'), 'success');
		} catch (err) {
			await failed(err, t('admin.languages.could_not_save_the_texts'));
		} finally {
			saving = false;
		}
	}

	async function setBoard(code: string) {
		if (!auth.token || !langs) return;
		try {
			await setBoardLanguage(auth.token, code);
			langs.board = code;
			toast.push(t('admin.languages.saved_the_bbs_picks_it'), 'success');
		} catch (err) {
			await failed(err, t('admin.languages.could_not_save_the_language'));
		}
	}

	const langName = (code: string) => langs?.languages.find((l) => l.code === code)?.name ?? code;

	const shown = $derived.by(() => {
		const q = query.trim().toLowerCase();
		return texts.filter((x) => {
			if (filter === 'changed' && !(own[x.key] ?? '').trim()) return false;
			if (filter === 'inherited' && !x.from) return false;
			if (!q) return true;
			return [x.key, x.english, x.builtin, own[x.key] ?? ''].some((s) => s.toLowerCase().includes(q));
		});
	});

	const groups = $derived.by(() => {
		const out: { name: string; items: CatalogText[] }[] = [];
		for (const x of shown) {
			const name = groupOf(x.key);
			let g = out.find((x) => x.name === name);
			if (!g) out.push((g = { name, items: [] }));
			g.items.push(x);
		}
		return out;
	});

	// Placeholders a text uses that its English one doesn't fill.
	function unknownPlaceholders(x: CatalogText, text: string) {
		if (x.key.startsWith('screen.') || x.key.startsWith('menu.')) return [];
		const found = [...text.matchAll(/\{([A-Z][A-Z0-9_]*)\}/g)].map((m) => m[1]);
		return found.filter((p) => !x.placeholders.includes(p));
	}

	const rows = (x: CatalogText) => Math.min(6, Math.max(1, Math.ceil(Math.max(x.builtin.length, (own[x.key] ?? '').length) / 70), x.builtin.split('\n').length));
</script>

<div class="mb-5">
	<h1 class="page-title">{t('admin.languages.languages')}</h1>
	<p class="page-subtitle max-w-3xl leading-relaxed">
		{t('admin.languages.the_bbs_speaks_v_a', { V: langs ? langs.languages.map((l) => l.name).join(', ') : '…' })} <span class="font-mono">main.de.ans</span>{t('admin.languages.then')}
		<span class="font-mono">main.ans</span> {t('admin.languages.deutsch_du_tries')} <span class="font-mono">main.de-du.ans</span> {t('admin.languages.first')}
	</p>
</div>

{#if langs}
	<section class="card mb-4">
		<div class="flex flex-wrap items-end gap-4">
			<label class="flex flex-col gap-1 text-xs text-muted">
				{t('admin.languages.the_board_s_language')}
				<select class="field min-w-[12rem]" value={langs.board} onchange={(e) => setBoard(e.currentTarget.value)}>
					{#each langs.languages as l (l.code)}<option value={l.code}>{l.name}</option>{/each}
				</select>
			</label>
			<p class="max-w-xl pb-1 text-xs leading-relaxed text-faint">
				{t('admin.languages.what_callers_read_before_they')}
			</p>
		</div>
	</section>

	<section class="card">
		<div class="mb-4 flex flex-wrap items-center gap-2">
			{#each langs.languages as l (l.code)}
				<button class="pill {lang === l.code ? 'pill-active' : ''}" onclick={() => pick(l.code)}>
					{l.name}{#if langs.changed[l.code]}<span class="ml-1.5 text-[10px] opacity-70">{t('admin.languages.v_changed', { V: langs.changed[l.code] })}</span>{/if}
				</button>
			{/each}
			<input class="field ml-auto w-full sm:w-64" type="search" placeholder={t('admin.languages.search_key_or_text')} bind:value={query} />
		</div>
		<div class="mb-4 flex flex-wrap items-center gap-1 text-xs">
			<button class="pill {filter === 'all' ? 'pill-active' : ''}" onclick={() => (filter = 'all')}>{t('admin.languages.all')}</button>
			<button class="pill {filter === 'changed' ? 'pill-active' : ''}" onclick={() => (filter = 'changed')}>{t('admin.languages.changed_by_you')}</button>
			{#if texts.some((x) => x.from)}
				<button class="pill {filter === 'inherited' ? 'pill-active' : ''}" onclick={() => (filter = 'inherited')}>{t('admin.languages.taken_from_another_language')}</button>
			{/if}
			<span class="ml-auto text-faint">{t('admin.languages.length_of_length2_texts', { LENGTH: shown.length, LENGTH2: texts.length })}</span>
		</div>

		{#if loading}
			<p class="text-sm text-muted">{t('admin.common.loading')}</p>
		{:else if !groups.length}
			<p class="text-sm text-muted">{t('admin.languages.no_text_matches')}</p>
		{/if}

		{#each groups as g (g.name)}
			<h2 class="card-label mt-5 mb-2 first:mt-0">{g.name}</h2>
			<div class="flex flex-col divide-y divide-line">
				{#each g.items as x (x.key)}
					{@const mine = own[x.key] ?? ''}
					{@const bad = unknownPlaceholders(x, mine)}
					<div class="grid gap-2 py-2.5 md:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
						<div class="min-w-0">
							<div class="truncate font-mono text-[11px] text-faint" title={x.key}>{x.key}</div>
							{#if lang !== 'en'}
								<div class="text-sm break-words whitespace-pre-wrap text-muted">{x.english}</div>
							{/if}
							{#if x.placeholders.length}
								<div class="mt-1 flex flex-wrap gap-1">
									{#each x.placeholders as p (p)}<span class="rounded bg-surface px-1.5 py-0.5 font-mono text-[10px] text-accent">{'{' + p + '}'}</span>{/each}
								</div>
							{/if}
						</div>
						<div class="min-w-0">
							<div class="flex items-start gap-2">
								<textarea
									class="field min-w-0 flex-1 resize-y font-mono text-[13px] {mine.trim() ? 'border-accent/60' : ''}"
									rows={rows(x)}
									value={mine}
									oninput={(e) => (own[x.key] = e.currentTarget.value)}
									placeholder={x.builtin}
									aria-label={x.key}
								></textarea>
								{#if mine.trim()}
									<button class="btn-secondary btn-xs mt-1 shrink-0" title={t('admin.languages.back_to_the_built_in')} onclick={() => (own[x.key] = '')}>{t('admin.languages.reset')}</button>
								{/if}
							</div>
							{#if x.from && !mine.trim()}
								<div class="mt-1 text-[11px] text-faint">{t('admin.languages.taken_from_v', { V: langName(x.from) })}</div>
							{/if}
							{#if bad.length}
								<div class="mt-1 text-[11px] text-amber-300">
									{t('admin.languages.v_isn_t_filled_in', { V: bad.map((p) => '{' + p + '}').join(', '), V2: x.placeholders.length ? t('admin.languages.it_can_use_v', { V: x.placeholders.map((p) => '{' + p + '}').join(', ') }) : '' })}
								</div>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{/each}
	</section>

	{#if dirty}
		<div class="sticky bottom-4 mt-4 flex justify-end">
			<div class="card flex items-center gap-3 px-4 py-3 shadow-lg">
				<span class="text-sm text-muted">{t('admin.languages.unsaved_changes_in_v', { V: langName(lang) })}</span>
				<button class="btn-secondary btn-sm" onclick={load}>{t('admin.common.revert')}</button>
				<button class="btn-primary btn-sm" disabled={saving} onclick={save}>{saving ? t('admin.common.saving') : t('admin.common.save')}</button>
			</div>
		</div>
	{/if}
{/if}
