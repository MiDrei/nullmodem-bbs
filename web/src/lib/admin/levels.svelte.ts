// The named security levels, loaded once for the admin pages that let
// a level be chosen (SLSelect) and refreshed after they're edited.
import { getSecurityLevels, type SecurityLevel } from '$lib/api';
import { auth } from '$lib/auth.svelte';

class Levels {
	list = $state<SecurityLevel[]>([]);
	custom = $state(false);
	#loading: Promise<void> | null = null;

	load(force = false): Promise<void> {
		if (!auth.token) return Promise.resolve();
		if (this.#loading && !force) return this.#loading;
		this.#loading = getSecurityLevels(auth.token)
			.then((r) => {
				this.list = r.levels;
				this.custom = r.custom;
			})
			.catch(() => {
				this.#loading = null;
			});
		return this.#loading;
	}

	set(levels: SecurityLevel[], custom: boolean) {
		this.list = levels;
		this.custom = custom;
	}

	/** "20 – Regular user", or just the number for an unnamed level. */
	label(level: number): string {
		const l = this.list.find((x) => x.level === level);
		return l ? `${level} – ${l.name}` : String(level);
	}
}

export const levels = new Levels();
