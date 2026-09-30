// bbsAuth is auth.svelte.ts's counterpart for the BBS user portal
// (routes/(portal)/*) -- a separate session, under its own
// localStorage key, so a sysop's admin session and their own portal
// session (if they log into both) don't collide or share a token
// whose claims mean different things on each side.
const STORAGE_KEY = 'nullmodem.bbs.session';

interface Session {
	token: string;
	username: string;
	/** Profile time zone (IANA name); "" or absent = the browser's own zone. See $lib/datetime. */
	timezone?: string;
	/** Security level, for showing the sysop the way to the admin. */
	securityLevel?: number;
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

	get timezone() {
		return this.session?.timezone || undefined;
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

	/** Updates the stored profile time zone -- after a profile save, or when a refresh finds it changed (e.g. set via Telnet/SSH). */
	/** Sysop access (SL 255): the portal links to the admin. */
	get isSysop() {
		return (this.session?.securityLevel ?? 0) >= 255;
	}

	/** Updates the stored security level (after a profile refresh). */
	setSecurityLevel(level: number) {
		if (this.session && this.session.securityLevel !== level) {
			this.set({ ...this.session, securityLevel: level });
		}
	}

	setTimezone(timezone: string) {
		if (this.session && this.session.timezone !== timezone) {
			this.set({ ...this.session, timezone });
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
