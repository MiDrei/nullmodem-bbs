// The portal's, the reader's and the front page's texts in the
// visitor's language: the catalog's web.* keys (internal/i18n, edited
// in the admin's language editor). The page arrives with them
// (window.__NM_I18N, see internal/web's serveIndex); a switch fetches
// the other language's. t('web.x.y', { COUNT: 3 }) fills {COUNT}.

export interface Language {
	code: string;
	name: string;
}

interface Boot {
	lang: string;
	texts: Record<string, string>;
	languages: Language[];
}

const COOKIE = 'nm_lang';

const boot: Boot | undefined = typeof window !== 'undefined' ? (window as unknown as { __NM_I18N?: Boot }).__NM_I18N : undefined;

const state = $state<Boot>({
	lang: boot?.lang ?? 'en',
	texts: boot?.texts ?? {},
	languages: boot?.languages ?? [{ code: 'en', name: 'English' }]
});

function cookieLang(): string | null {
	if (typeof document === 'undefined') return null;
	const m = document.cookie.match(/(?:^|;\s*)nm_lang=([^;]+)/);
	return m ? decodeURIComponent(m[1]) : null;
}

function rememberLang(lang: string) {
	if (typeof document === 'undefined') return;
	document.cookie = `${COOKIE}=${encodeURIComponent(lang)}; path=/; max-age=${60 * 60 * 24 * 365}; samesite=lax`;
}

async function load(lang: string): Promise<void> {
	const res = await fetch(`/api/i18n-texts/${encodeURIComponent(lang)}`);
	if (!res.ok) return;
	const data = (await res.json()) as Boot;
	state.lang = data.lang;
	state.texts = data.texts;
	state.languages = data.languages;
	if (typeof document !== 'undefined') document.documentElement.lang = data.lang.split('-')[0];
}

// No texts in the page (the dev server), or a cached page in another
// language than chosen since (the reader offline): fetch them.
if (typeof window !== 'undefined') {
	const chosen = cookieLang();
	if (!boot) load(chosen ?? 'auto').catch(() => {});
	else if (chosen && chosen !== boot.lang && navigator.onLine) load(chosen).catch(() => {});
}

function fill(text: string, args?: Record<string, string | number>): string {
	if (!args) return text;
	return text.replace(/\{([A-Z][A-Z0-9_]*)\}/g, (m, name: string) => (name in args ? String(args[name]) : m));
}

/** key's text in the current language, its {NAME}s filled from args. */
export function t(key: string, args?: Record<string, string | number>): string {
	return fill(state.texts[key] ?? key, args);
}

/** The singular (key.one) or plural (key.other) text for n, {COUNT} = n. */
export function tn(key: string, n: number, args?: Record<string, string | number>): string {
	return t(`${key}.${n === 1 ? 'one' : 'other'}`, { COUNT: n, ...args });
}

export const i18n = {
	get lang() {
		return state.lang;
	},
	get languages() {
		return state.languages;
	},
	/** The locale for dates and numbers ("de-du" -> "de"). */
	get locale() {
		return state.lang.split('-')[0];
	}
};

/** Switches to lang and remembers it for this browser. */
export async function setLang(lang: string): Promise<void> {
	rememberLang(lang);
	if (lang !== state.lang || !Object.keys(state.texts).length) await load(lang);
}

/** After login: the account's language, if it has one. */
export async function useAccountLang(lang: string | undefined | null): Promise<void> {
	if (lang && lang !== state.lang) await setLang(lang);
	else if (lang) rememberLang(lang);
}
