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

	// The texts in sections (where they show up) and, in each, groups
	// (what they're about) -- one group on screen at a time.
	type Section = 'telnet' | 'screens' | 'web' | 'admin' | 'notices';
	const sections: Section[] = ['telnet', 'screens', 'web', 'admin', 'notices'];
	const sectionLabel = (s: Section) =>
		({
			telnet: t('admin.languages.section.telnet'),
			screens: t('admin.languages.section.screens'),
			web: t('admin.common.web'),
			admin: t('web.common.admin'),
			notices: t('admin.languages.section.notices')
		})[s];

	// A key's group: telnet "netmail", web "w:reader", admin "a:doors".
	const telnetMerge: Record<string, string> = { read: 'list', msgs: 'areas' };
	const webMerge: Record<string, string> = { msgs: 'areas', footer: 'nav', time: 'common', file: 'files', polls: 'community', bbslist: 'community', callers: 'community', push: 'reader' };
	function groupOf(key: string): string {
		const [first, second] = key.split('.');
		if (first === 'web') return 'w:' + (webMerge[second] ?? second);
		if (first === 'admin') return 'a:' + second;
		return telnetMerge[first] ?? first;
	}
	// Where a text shows in the editor: a shared one (common.*,
	// web.common.*, admin.common.*) in the place of a key merged into
	// it -- by topic, not in one big "shared" pile.
	function homeKey(x: CatalogText): string {
		if (!x.was?.length) return x.key;
		let mine: (k: string) => boolean;
		const area = x.key.match(/^(?:(web|admin)\.)?common\./);
		if (!area) return x.key;
		if (area[1]) mine = (k) => k.startsWith(area[1] + '.');
		else mine = (k) => !/^(web|admin)\./.test(k);
		return x.was.find(mine) ?? x.was[0];
	}
	// The other places a shared text shows up.
	function alsoIn(x: CatalogText): string {
		const home = groupOf(homeKey(x));
		const seen = new Set<string>([home]);
		const out: string[] = [];
		for (const k of [x.key, ...(x.was ?? [])]) {
			const g = groupOf(k);
			if (seen.has(g) || /^(common|w:common|a:common)$/.test(g)) continue;
			seen.add(g);
			out.push(sectionLabel(sectionOf(g)) + ' · ' + groupLabel(g));
		}
		return out.join(', ');
	}

	function sectionOf(group: string): Section {
		if (group.startsWith('w:')) return 'web';
		if (group.startsWith('a:') || group === 'doortpl') return 'admin';
		const first = group;
		if (['screen', 'col', 'menu', 'builtin'].includes(first)) return 'screens';
		if (['api', 'push', 'health', 'email', 'restart'].includes(first)) return 'notices';
		return 'telnet';
	}

	// The telnet groups in the order a caller meets them; the others
	// follow their names.
	const telnetOrder = ['common', 'guard', 'login', 'register', 'approval', 'lang', 'summary', 'scan', 'list', 'areas', 'myareas', 'msg', 'editor', 'fse', 'search', 'netmail', 'files', 'transfer', 'qwk', 'chat', 'oneliners', 'who', 'polls', 'bbslist', 'lastcallers', 'nodelist', 'doors', 'profile', 'sysop', 'logoff'];

	function groupLabel(g: string): string {
		const labels: Record<string, string> = {
			common: t('admin.languages.group.shared'),
			list: t('admin.languages.group.lists'),
			col: t('admin.languages.group.column_heads'),
			approval: t('admin.languages.group.new_accounts'),
			guard: t('admin.languages.group.refused_before_login'),
			login: t('admin.languages.group.login'),
			register: t('admin.languages.group.registration'),
			lang: t('admin.languages.group.choosing_a_language'),
			menu: t('admin.common.menus'),
			builtin: t('admin.languages.group.menu_commands'),
			logoff: t('admin.common.logoff'),
			who: t('admin.languages.group.whos_online'),
			sysop: t('admin.languages.group.sysop_functions'),
			profile: t('web.common.profile'),
			doors: t('common.doors'),
			polls: t('common.voting_booth'),
			files: t('common.files'),
			transfer: t('admin.languages.group.file_transfers'),
			qwk: t('admin.languages.group.qwk'),
			msg: t('admin.languages.group.writing_messages'),
			editor: t('admin.languages.group.line_editor'),
			fse: t('admin.languages.group.full_screen_editor'),
			areas: t('admin.common.message_areas'),
			myareas: t('common.my_areas'),
			bbslist: t('common.bbs_list'),
			scan: t('admin.languages.group.new_scan'),
			summary: t('admin.languages.group.after_login'),
			search: t('admin.languages.group.message_search'),
			oneliners: t('common.one_liners'),
			chat: t('common.chat'),
			netmail: t('common.netmail'),
			nodelist: t('common.nodelist'),
			lastcallers: t('web.common.last_callers'),
			screen: t('admin.languages.group.screens_t_key_in_ans'),
			api: t('admin.languages.group.web_error_messages'),
			push: t('admin.languages.push_notifications'),
			health: t('admin.languages.group.health'),
			email: t('common.email_gateway'),
			restart: t('admin.languages.group.restart_reasons'),
			doortpl: t('admin.languages.group.door_templates'),
			'w:common': t('admin.languages.group.shared'),
			'w:nav': t('admin.languages.group.navigation'),
			'w:login': t('admin.languages.group.login'),
			'w:areas': t('admin.common.message_areas'),
			'w:msg': t('admin.languages.group.reading_writing'),
			'w:netmail': t('common.netmail'),
			'w:files': t('common.files'),
			'w:share': t('admin.languages.group.public_downloads'),
			'w:qwk': t('admin.languages.group.qwk'),
			'w:chat': t('common.chat'),
			'w:community': t('web.common.community'),
			'w:profile': t('web.common.profile'),
			'w:search': t('admin.languages.group.message_search'),
			'w:terminal': t('admin.languages.group.web_terminal'),
			'w:home': t('admin.languages.group.front_page_2'),
			'w:stats': t('admin.common.statistics'),
			'w:reader': t('web.common.reader_app'),
			'a:archive': t('admin.common.packet_analyzer'),
			'a:areafix': t('admin.common.areafix_filefix'),
			'a:backups': t('admin.common.backups'),
			'a:binkp': t('admin.common.networks_addresses'),
			'a:binkp_uplinks': t('admin.common.uplinks_nodes_points'),
			'a:chat': t('admin.common.chat_one_liners'),
			'a:chatsettings': t('admin.languages.group.chat_rooms_bridges'),
			'a:common': t('admin.languages.group.shared'),
			'a:dashboard': t('admin.nav.dashboard'),
			'a:designer': t('admin.common.ansi_designer'),
			'a:doors': t('common.doors'),
			'a:email': t('common.email_gateway'),
			'a:file_areas': t('common.file_areas'),
			'a:languages': t('admin.common.languages'),
			'a:login': t('admin.languages.group.login'),
			'a:logs': t('admin.common.logs'),
			'a:maintenance': t('admin.common.maintenance'),
			'a:menus': t('admin.common.menus'),
			'a:message_areas': t('common.message_areas'),
			'a:nav': t('admin.languages.group.navigation'),
			'a:netmail': t('admin.common.undeliverable_netmail'),
			'a:nodelists': t('common.nodelists'),
			'a:offsite': t('admin.common.off_site_copy'),
			'a:pending_areas': t('admin.common.pending_areas'),
			'a:polls': t('admin.common.polls_bbs_list'),
			'a:screens': t('admin.common.screens'),
			'a:security': t('admin.common.security'),
			'a:services': t('admin.common.services'),
			'a:settings': t('web.common.settings'),
			'a:security_levels': t('admin.common.security_levels'),
			'a:stats': t('admin.common.statistics'),
			'a:twofactor': t('admin.common.two_factor_login'),
			'a:users': t('admin.common.users')
		};
		return labels[g] ?? g.replace(/^[wa]:/, '').replace(/_/g, ' ');
	}

	let section = $state<Section>('telnet');
	let group = $state('');

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
			const sec = page.url.searchParams.get('section') as Section | null;
			if (sec && sections.includes(sec)) section = sec;
			group = page.url.searchParams.get('group') ?? '';
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
			remember();
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

	// The texts the filter lets through, anywhere.
	const filtered = $derived.by(() => {
		const q = query.trim().toLowerCase();
		return texts.filter((x) => {
			if (filter === 'changed' && !(own[x.key] ?? '').trim()) return false;
			if (filter === 'inherited' && !x.from) return false;
			if (!q) return true;
			return [x.key, x.english, x.builtin, own[x.key] ?? ''].some((s) => s.toLowerCase().includes(q));
		});
	});

	type Group = { id: string; label: string; items: CatalogText[]; changed: number };
	function grouped(list: CatalogText[]): Group[] {
		const by = new Map<string, Group>();
		for (const x of list) {
			const id = groupOf(homeKey(x));
			let g = by.get(id);
			if (!g) by.set(id, (g = { id, label: groupLabel(id), items: [], changed: 0 }));
			g.items.push(x);
			if ((own[x.key] ?? '').trim()) g.changed++;
		}
		return [...by.values()].sort((a, b) => {
			const ia = telnetOrder.indexOf(a.id), ib = telnetOrder.indexOf(b.id);
			if (ia >= 0 || ib >= 0) return (ia < 0 ? 999 : ia) - (ib < 0 ? 999 : ib);
			return a.label.localeCompare(b.label);
		});
	}

	// Each section's groups (for the tabs' counts and the group list).
	const bySection = $derived.by(() => {
		const out = Object.fromEntries(sections.map((s) => [s, [] as CatalogText[]])) as Record<Section, CatalogText[]>;
		for (const x of filtered) out[sectionOf(groupOf(homeKey(x)))].push(x);
		return out;
	});
	const sectionGroups = $derived(grouped(bySection[section]));
	const searching = $derived(query.trim() !== '');
	// What's on screen: the search's hits (capped), else the one group.
	const SEARCH_CAP = 150;
	const groups = $derived.by(() => {
		if (searching) {
			const hits = grouped(filtered.slice(0, SEARCH_CAP)).sort(
				(a, b) => sections.indexOf(sectionOf(a.id)) - sections.indexOf(sectionOf(b.id))
			);
			for (const g of hits) g.label = sectionLabel(sectionOf(g.id)) + ' · ' + g.label;
			return hits;
		}
		const g = sectionGroups.find((x) => x.id === group) ?? sectionGroups[0];
		return g ? [g] : [];
	});
	const shown = $derived(groups.reduce((n, g) => n + g.items.length, 0));

	function pickSection(s: Section) {
		section = s;
		group = '';
		remember();
	}
	function pickGroup(id: string) {
		group = id;
		remember();
		document.getElementById('texts')?.scrollIntoView({ block: 'nearest' });
	}
	// Where you are, in the address: a reload stays there.
	function remember() {
		const u = new URL(location.href);
		u.searchParams.set('lang', lang);
		u.searchParams.set('section', section);
		if (group) u.searchParams.set('group', group);
		else u.searchParams.delete('group');
		history.replaceState(history.state, '', u);
	}

	// Placeholders a text uses that its English one doesn't fill.
	function unknownPlaceholders(x: CatalogText, text: string) {
		if (x.key.startsWith('screen.') || x.key.startsWith('menu.')) return [];
		const found = [...text.matchAll(/\{([A-Z][A-Z0-9_]*)\}/g)].map((m) => m[1]);
		return found.filter((p) => !x.placeholders.includes(p));
	}

	const rows = (x: CatalogText) => Math.min(6, Math.max(1, Math.ceil(Math.max(x.builtin.length, (own[x.key] ?? '').length) / 70), x.builtin.split('\n').length));
</script>

<div class="mb-5">
	<h1 class="page-title">{t('admin.common.languages')}</h1>
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

		<div class="mb-3 flex flex-wrap gap-1 border-b border-line pb-3">
			{#each sections as sec (sec)}
				<button
					class="pill {!searching && section === sec ? 'pill-active' : ''}"
					disabled={searching}
					onclick={() => pickSection(sec)}
				>
					{sectionLabel(sec)}
					<span class="ml-1 text-[10px] opacity-70">{bySection[sec].length}</span>
				</button>
			{/each}
		</div>
		<div class="mb-4 flex flex-wrap items-center gap-1 text-xs">
			<button class="pill {filter === 'all' ? 'pill-active' : ''}" onclick={() => (filter = 'all')}>{t('web.common.all')}</button>
			<button class="pill {filter === 'changed' ? 'pill-active' : ''}" onclick={() => (filter = 'changed')}>{t('admin.languages.changed_by_you')}</button>
			{#if texts.some((x) => x.from)}
				<button class="pill {filter === 'inherited' ? 'pill-active' : ''}" onclick={() => (filter = 'inherited')}>{t('admin.languages.taken_from_another_language')}</button>
			{/if}
			<span class="ml-auto text-faint">
				{#if searching && filtered.length > SEARCH_CAP}
					{t('admin.languages.search_capped', { SHOWN: SEARCH_CAP, FOUND: filtered.length })}
				{:else}
					{t('admin.languages.length_of_length2_texts', { LENGTH: searching ? filtered.length : shown, LENGTH2: texts.length })}
				{/if}
			</span>
		</div>

		{#if loading}
			<p class="text-sm text-muted">{t('web.common.loading')}</p>
		{:else if !groups.length}
			<p class="text-sm text-muted">{t('admin.languages.no_text_matches')}</p>
		{:else}
			<div class="grid gap-4 {searching ? '' : 'md:grid-cols-[13rem_minmax(0,1fr)]'}">
				{#if !searching}
					<!-- The section's groups: a list beside the texts, a menu on a phone. -->
					<select class="field md:hidden" value={groups[0]?.id} onchange={(e) => pickGroup(e.currentTarget.value)}>
						{#each sectionGroups as sg (sg.id)}
							<option value={sg.id}>{sg.label} ({sg.items.length}){sg.changed ? ' •' : ''}</option>
						{/each}
					</select>
					<nav class="hidden md:block">
						<div class="sticky top-4 flex max-h-[calc(100vh-2rem)] flex-col overflow-y-auto">
							{#each sectionGroups as sg (sg.id)}
								<button
									class="flex items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-[13px] {groups[0]?.id === sg.id
										? 'bg-surface font-medium text-ink-strong'
										: 'text-muted hover:text-ink'}"
									onclick={() => pickGroup(sg.id)}
								>
									<span class="min-w-0 flex-1 truncate">{sg.label}</span>
									{#if sg.changed}<span class="h-1.5 w-1.5 shrink-0 rounded-full bg-accent" title={t('admin.languages.v_changed', { V: sg.changed })}></span>{/if}
									<span class="shrink-0 text-[11px] text-faint">{sg.items.length}</span>
								</button>
							{/each}
						</div>
					</nav>
				{/if}
				<div id="texts" class="min-w-0">
					{#each groups as g (g.id)}
						<h2 class="card-label mt-5 mb-2 first:mt-0">{g.label}</h2>
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
							{#if alsoIn(x)}
								<div class="mt-1 text-[11px] text-faint">{t('admin.languages.also_used_in', { WHERE: alsoIn(x) })}</div>
							{/if}
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
				</div>
			</div>
		{/if}
	</section>

	{#if dirty}
		<div class="sticky bottom-4 mt-4 flex justify-end">
			<div class="card flex items-center gap-3 px-4 py-3 shadow-lg">
				<span class="text-sm text-muted">{t('admin.languages.unsaved_changes_in_v', { V: langName(lang) })}</span>
				<button class="btn-secondary btn-sm" onclick={load}>{t('admin.common.revert')}</button>
				<button class="btn-primary btn-sm" disabled={saving} onclick={save}>{saving ? t('web.common.saving') : t('web.common.save')}</button>
			</div>
		</div>
	{/if}
{/if}
