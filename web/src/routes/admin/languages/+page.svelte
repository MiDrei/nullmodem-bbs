<script lang="ts">
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
		common: 'Shared',
		list: 'Lists',
		read: 'Lists',
		col: 'Column heads',
		approval: 'New accounts',
		guard: 'Refused before login',
		login: 'Login',
		register: 'Registration',
		lang: 'Choosing a language',
		menu: 'Menus',
		logoff: 'Logoff',
		who: "Who's online, node messages",
		sysop: 'Sysop functions',
		profile: 'Profile',
		doors: 'Doors',
		polls: 'Voting booth',
		files: 'Files',
		transfer: 'File transfers',
		qwk: 'QWK',
		msg: 'Writing messages',
		editor: 'Line editor',
		fse: 'Full-screen editor',
		areas: 'Message areas',
		msgs: 'Message areas',
		myareas: 'My areas',
		bbslist: 'BBS list',
		scan: 'New scan',
		summary: 'After login',
		search: 'Message search',
		oneliners: 'One-liners',
		chat: 'Chat',
		netmail: 'Netmail',
		nodelist: 'Nodelist',
		lastcallers: 'Last callers',
		screen: 'Screens ({T:key} in .ans files)',
		api: 'Web: error messages',
		push: 'Push notifications'
	};
	// The web's texts (web.<part>.<name>), by their part.
	const webNames: Record<string, string> = {
		common: 'shared',
		nav: 'navigation',
		footer: 'navigation',
		time: 'shared',
		login: 'login',
		areas: 'message areas',
		msgs: 'message areas',
		msg: 'messages',
		netmail: 'netmail',
		files: 'files',
		file: 'files',
		qwk: 'QWK',
		chat: 'chat',
		community: 'community',
		polls: 'community',
		bbslist: 'community',
		callers: 'community',
		profile: 'profile',
		search: 'search',
		share: 'shared files',
		terminal: 'terminal',
		home: 'front page',
		stats: 'statistics',
		push: 'reader app',
		reader: 'reader app'
	};
	function groupOf(key: string) {
		const [first, second] = key.split('.');
		if (first === 'web') return `Web: ${webNames[second] ?? second}`;
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
			await failed(err, 'Could not load the languages.');
		}
	});

	beforeNavigate((nav) => {
		if (dirty && !confirm('Leave without saving the texts?')) nav.cancel();
	});

	async function load() {
		if (!auth.token) return;
		loading = true;
		try {
			texts = await getCatalog(auth.token, lang);
			own = Object.fromEntries(texts.filter((t) => t.text !== '').map((t) => [t.key, t.text]));
			saved = JSON.stringify(clean(own));
			history.replaceState(history.state, '', `?lang=${encodeURIComponent(lang)}`);
		} finally {
			loading = false;
		}
	}

	async function pick(code: string) {
		if (code === lang) return;
		if (dirty && !confirm('Discard the changes to these texts?')) return;
		lang = code;
		try {
			await load();
		} catch (err) {
			await failed(err, 'Could not load the texts.');
		}
	}

	async function save() {
		if (!auth.token) return;
		saving = true;
		try {
			await saveCatalog(auth.token, lang, clean(own));
			await load();
			if (langs) langs.changed[lang] = Object.keys(clean(own)).length;
			toast.push('Saved -- callers see it right away.', 'success');
		} catch (err) {
			await failed(err, 'Could not save the texts.');
		} finally {
			saving = false;
		}
	}

	async function setBoard(code: string) {
		if (!auth.token || !langs) return;
		try {
			await setBoardLanguage(auth.token, code);
			langs.board = code;
			toast.push('Saved -- the BBS picks it up within half a minute.', 'success');
		} catch (err) {
			await failed(err, 'Could not save the language.');
		}
	}

	const langName = (code: string) => langs?.languages.find((l) => l.code === code)?.name ?? code;

	const shown = $derived.by(() => {
		const q = query.trim().toLowerCase();
		return texts.filter((t) => {
			if (filter === 'changed' && !(own[t.key] ?? '').trim()) return false;
			if (filter === 'inherited' && !t.from) return false;
			if (!q) return true;
			return [t.key, t.english, t.builtin, own[t.key] ?? ''].some((s) => s.toLowerCase().includes(q));
		});
	});

	const groups = $derived.by(() => {
		const out: { name: string; items: CatalogText[] }[] = [];
		for (const t of shown) {
			const name = groupOf(t.key);
			let g = out.find((x) => x.name === name);
			if (!g) out.push((g = { name, items: [] }));
			g.items.push(t);
		}
		return out;
	});

	// Placeholders a text uses that its English one doesn't fill.
	function unknownPlaceholders(t: CatalogText, text: string) {
		if (t.key.startsWith('screen.') || t.key.startsWith('menu.')) return [];
		const found = [...text.matchAll(/\{([A-Z][A-Z0-9_]*)\}/g)].map((m) => m[1]);
		return found.filter((p) => !t.placeholders.includes(p));
	}

	const rows = (t: CatalogText) => Math.min(6, Math.max(1, Math.ceil(Math.max(t.builtin.length, (own[t.key] ?? '').length) / 70), t.builtin.split('\n').length));
</script>

<div class="mb-5">
	<h1 class="page-title">Languages</h1>
	<p class="page-subtitle max-w-3xl leading-relaxed">
		The BBS speaks {langs ? langs.languages.map((l) => l.name).join(', ') : '…'}; a caller picks theirs at registration and in their
		profile. Change any text here -- what you leave empty keeps the built-in one, and English stands in for whatever a language
		lacks. A screen can come in each language too: <span class="font-mono">main.de.ans</span>, then
		<span class="font-mono">main.ans</span> (Deutsch (Du) tries <span class="font-mono">main.de-du.ans</span> first).
	</p>
</div>

{#if langs}
	<section class="card mb-4">
		<div class="flex flex-wrap items-end gap-4">
			<label class="flex flex-col gap-1 text-xs text-muted">
				The board's language
				<select class="field min-w-[12rem]" value={langs.board} onchange={(e) => setBoard(e.currentTarget.value)}>
					{#each langs.languages as l (l.code)}<option value={l.code}>{l.name}</option>{/each}
				</select>
			</label>
			<p class="max-w-xl pb-1 text-xs leading-relaxed text-faint">
				What callers read before they log in -- the login prompts, the welcome screen's language variant -- and afterwards if they
				never chose one.
			</p>
		</div>
	</section>

	<section class="card">
		<div class="mb-4 flex flex-wrap items-center gap-2">
			{#each langs.languages as l (l.code)}
				<button class="pill {lang === l.code ? 'pill-active' : ''}" onclick={() => pick(l.code)}>
					{l.name}{#if langs.changed[l.code]}<span class="ml-1.5 text-[10px] opacity-70">{langs.changed[l.code]} changed</span>{/if}
				</button>
			{/each}
			<input class="field ml-auto w-full sm:w-64" type="search" placeholder="Search key or text…" bind:value={query} />
		</div>
		<div class="mb-4 flex flex-wrap items-center gap-1 text-xs">
			<button class="pill {filter === 'all' ? 'pill-active' : ''}" onclick={() => (filter = 'all')}>All</button>
			<button class="pill {filter === 'changed' ? 'pill-active' : ''}" onclick={() => (filter = 'changed')}>Changed by you</button>
			{#if texts.some((t) => t.from)}
				<button class="pill {filter === 'inherited' ? 'pill-active' : ''}" onclick={() => (filter = 'inherited')}>Taken from another language</button>
			{/if}
			<span class="ml-auto text-faint">{shown.length} of {texts.length} texts</span>
		</div>

		{#if loading}
			<p class="text-sm text-muted">Loading…</p>
		{:else if !groups.length}
			<p class="text-sm text-muted">No text matches.</p>
		{/if}

		{#each groups as g (g.name)}
			<h2 class="card-label mt-5 mb-2 first:mt-0">{g.name}</h2>
			<div class="flex flex-col divide-y divide-line">
				{#each g.items as t (t.key)}
					{@const mine = own[t.key] ?? ''}
					{@const bad = unknownPlaceholders(t, mine)}
					<div class="grid gap-2 py-2.5 md:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
						<div class="min-w-0">
							<div class="truncate font-mono text-[11px] text-faint" title={t.key}>{t.key}</div>
							{#if lang !== 'en'}
								<div class="text-sm break-words whitespace-pre-wrap text-muted">{t.english}</div>
							{/if}
							{#if t.placeholders.length}
								<div class="mt-1 flex flex-wrap gap-1">
									{#each t.placeholders as p (p)}<span class="rounded bg-surface px-1.5 py-0.5 font-mono text-[10px] text-accent">{'{' + p + '}'}</span>{/each}
								</div>
							{/if}
						</div>
						<div class="min-w-0">
							<div class="flex items-start gap-2">
								<textarea
									class="field min-w-0 flex-1 resize-y font-mono text-[13px] {mine.trim() ? 'border-accent/60' : ''}"
									rows={rows(t)}
									value={mine}
									oninput={(e) => (own[t.key] = e.currentTarget.value)}
									placeholder={t.builtin}
									aria-label={t.key}
								></textarea>
								{#if mine.trim()}
									<button class="btn-secondary btn-xs mt-1 shrink-0" title="Back to the built-in text" onclick={() => (own[t.key] = '')}>Reset</button>
								{/if}
							</div>
							{#if t.from && !mine.trim()}
								<div class="mt-1 text-[11px] text-faint">Taken from {langName(t.from)}.</div>
							{/if}
							{#if bad.length}
								<div class="mt-1 text-[11px] text-amber-300">
									{bad.map((p) => '{' + p + '}').join(', ')} isn't filled in here{t.placeholders.length ? ` -- it can use ${t.placeholders.map((p) => '{' + p + '}').join(', ')}` : ''}.
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
				<span class="text-sm text-muted">Unsaved changes in {langName(lang)}</span>
				<button class="btn-secondary btn-sm" onclick={load}>Revert</button>
				<button class="btn-primary btn-sm" disabled={saving} onclick={save}>{saving ? 'Saving…' : 'Save'}</button>
			</div>
		</div>
	{/if}
{/if}
