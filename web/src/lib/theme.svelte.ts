// The admin's light/dark choice, kept in this browser. Light is the
// default. src/app.html applies the saved choice before the first paint
// so the admin never flashes dark; the admin layout keeps it applied
// while it's shown and takes it off again for the portal, which stays
// dark.
export const THEME_KEY = 'nullmodem.admin.theme';

export type Theme = 'light' | 'dark';

function saved(): Theme {
	try {
		return localStorage.getItem(THEME_KEY) === 'dark' ? 'dark' : 'light';
	} catch {
		return 'light';
	}
}

class AdminTheme {
	mode = $state<Theme>(typeof localStorage === 'undefined' ? 'light' : saved());

	toggle() {
		this.mode = this.mode === 'light' ? 'dark' : 'light';
		try {
			localStorage.setItem(THEME_KEY, this.mode);
		} catch {
			// Private mode or blocked storage: the switch still works for
			// this visit.
		}
		this.apply();
	}

	apply() {
		document.documentElement.dataset.theme = this.mode;
	}

	remove() {
		delete document.documentElement.dataset.theme;
	}
}

export const adminTheme = new AdminTheme();
