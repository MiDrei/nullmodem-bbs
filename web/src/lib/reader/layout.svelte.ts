// Whether the reader shows its three panes side by side (tablet in
// landscape, desktop) or one screen at a time (phone, tablet upright).
const QUERY = '(min-width: 1000px)';

class ReaderLayout {
	wide = $state(false);

	constructor() {
		if (typeof window === 'undefined') return;
		const mq = window.matchMedia(QUERY);
		this.wide = mq.matches;
		mq.addEventListener('change', (e) => (this.wide = e.matches));
	}
}

export const readerLayout = new ReaderLayout();
