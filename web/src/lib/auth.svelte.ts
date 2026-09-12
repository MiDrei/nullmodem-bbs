const STORAGE_KEY = 'nullmodem.admin.session';

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

class AuthState {
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

export const auth = new AuthState();
