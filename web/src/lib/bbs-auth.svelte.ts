// bbsAuth is auth.svelte.ts's counterpart for the BBS user portal
// (routes/(portal)/*) -- a separate session, under its own
// localStorage key, so a sysop's admin session and their own portal
// session (if they log into both) don't collide or share a token
// whose claims mean different things on each side.
const STORAGE_KEY = 'nullmodem.bbs.session';

interface Session {
	token: string;
	username: string;
}

function readStoredSession(): Session | null {
	try {
		const raw = localStorage.getItem(STORAGE_KEY);
		return raw ? (JSON.parse(raw) as Session) : null;
	} catch {
		return null;
	}
}

class BBSAuthState {
	session = $state<Session | null>(readStoredSession());

	get token() {
		return this.session?.token ?? null;
	}

	get username() {
		return this.session?.username ?? null;
	}

	set(session: Session) {
		this.session = session;
		try {
			localStorage.setItem(STORAGE_KEY, JSON.stringify(session));
		} catch {
			// localStorage unavailable (private mode, etc.); session still
			// works for the current page load.
		}
	}

	clear() {
		this.session = null;
		try {
			localStorage.removeItem(STORAGE_KEY);
		} catch {
			// ignore
		}
	}
}

export const bbsAuth = new BBSAuthState();
