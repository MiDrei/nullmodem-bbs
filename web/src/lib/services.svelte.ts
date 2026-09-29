// The daemons' status for the admin, refreshed every few seconds while
// the admin is open -- the Services page and the "restart needed"
// banner in the admin layout both read it.
import { auth } from '$lib/auth.svelte';
import { listServices, restartService, type ServiceStatus } from '$lib/api';

const REFRESH_MS = 5000;

export const SERVICE_INFO: Record<string, { title: string; description: string }> = {
	bbs: { title: 'BBS', description: 'Telnet and SSH: menus, message and file areas, doors' },
	mailer: { title: 'Mailer', description: 'BinkP polls and the tosser: echomail, netmail, file echoes' },
	web: { title: 'Web', description: 'This admin and the web portal' }
};

/** Title and description of a daemon or a door's background program ("door:<door name>"). */
export function serviceInfo(name: string): { title: string; description: string } {
	if (SERVICE_INFO[name]) return SERVICE_INFO[name];
	if (name.startsWith('door:')) {
		const door = name.slice('door:'.length);
		return { title: door, description: `Background program of the door ${door}` };
	}
	return { title: name, description: '' };
}

class ServicesState {
	list = $state<ServiceStatus[]>([]);
	loaded = $state(false);
	private timer: ReturnType<typeof setInterval> | null = null;
	private users = 0;

	async refresh() {
		if (!auth.token) return;
		try {
			this.list = await listServices(auth.token);
			this.loaded = true;
		} catch {
			// The web daemon itself may be restarting; try again next round.
		}
	}

	/** Starts the periodic refresh; returns the matching stop. */
	start(): () => void {
		this.users++;
		if (!this.timer) {
			this.refresh();
			this.timer = setInterval(() => this.refresh(), REFRESH_MS);
		}
		return () => {
			this.users--;
			if (this.users <= 0 && this.timer) {
				clearInterval(this.timer);
				this.timer = null;
			}
		};
	}

	async restart(name: string, mode: 'now' | 'idle' = 'now') {
		if (!auth.token) return;
		await restartService(auth.token, name, mode);
		await this.refresh();
	}

	/** Services waiting for a restart to pick up saved changes. */
	get needingRestart(): ServiceStatus[] {
		return this.list.filter((s) => s.restart_needed.length > 0 && !s.restart_pending);
	}
}

export const servicesState = new ServicesState();
