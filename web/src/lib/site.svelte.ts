// site holds what the BBS says about itself before anyone logs in
// (GET /api/bbs/info): shared by the portal and admin layouts and both
// login pages, fetched once per page load.
import { getBBSInfo, type BBSInfo } from '$lib/api';

class SiteState {
	// The generic product name until the fetch resolves, so a header is
	// never blank.
	info = $state<BBSInfo>({ name: 'NullModem BBS', version: '' });
	private loading: Promise<void> | null = null;

	load(): Promise<void> {
		this.loading ??= getBBSInfo()
			.then((info) => {
				if (info.name) this.info = info;
			})
			.catch(() => {
				// Non-critical: keep the fallback.
			});
		return this.loading;
	}

	/** "host:port" to reach the board by Telnet, "" when Telnet is off. */
	get telnetAddress(): string {
		if (!this.info.telnet_port || typeof location === 'undefined') return '';
		return `${location.hostname}:${this.info.telnet_port}`;
	}
}

export const site = new SiteState();
