// Orders the BBS's .ans screens for the admin: by what they belong to
// (login and menus, message areas, file areas, netmail), each main
// screen with its parts (column header, rows, ...) under it, labelled
// by what they are rather than by file name.

export interface ScreenItem {
	name: string;
	label: string;
	/** A part of the screen above it (a row, the column header, ...). */
	part: boolean;
}

export interface ScreenGroup {
	title: string;
	items: ScreenItem[];
}

// file stem -> [group, label]; a stem's parts ("-row", ...) follow it.
const KNOWN: Record<string, [string, string]> = {
	welcome: ['Login & menus', 'Welcome (before login)'],
	main: ['Login & menus', 'Main menu'],
	sysop: ['Login & menus', 'Sysop menu'],
	logoff: ['Login & menus', 'Logoff'],
	msgareas: ['Message areas', 'Area list'],
	msglist: ['Message areas', 'Message list'],
	msgread: ['Message areas', 'Reading a message'],
	msgpost: ['Message areas', 'Writing a message'],
	filareas: ['File areas', 'Area list'],
	fillist: ['File areas', 'File list'],
	filread: ['File areas', 'File details'],
	netmail: ['Netmail', 'Inbox'],
	netread: ['Netmail', 'Reading netmail']
};

const PARTS: Record<string, string> = {
	columns: 'Column header',
	row: 'Row',
	'row-selected': 'Selected row',
	network: 'Network divider',
	meta: 'Header details'
};

const ORDER = ['Login & menus', 'Message areas', 'File areas', 'Netmail', 'Other'];

export function groupScreens(names: string[]): ScreenGroup[] {
	const byGroup = new Map<string, ScreenItem[]>();
	const add = (group: string, item: ScreenItem) => {
		if (!byGroup.has(group)) byGroup.set(group, []);
		byGroup.get(group)!.push(item);
	};
	const stems = Object.keys(KNOWN);
	// A screen's language variants (main.de.ans, main.de-du.ans) follow it.
	const addVariants = (group: string, base: string) => {
		for (const n of names.filter((x) => new RegExp(`^${base.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\.[a-z]{2}(-[a-z]+)?\\.ans$`).test(x)).sort()) {
			add(group, { name: n, label: `↳ ${n.slice(base.length + 1, -4)}`, part: true });
			used.add(n);
		}
	};
	// Main screens first, in KNOWN's order, each followed by its parts.
	const used = new Set<string>();
	for (const stem of stems) {
		const [group, label] = KNOWN[stem];
		if (names.includes(`${stem}.ans`)) {
			add(group, { name: `${stem}.ans`, label, part: false });
			used.add(`${stem}.ans`);
			addVariants(group, stem);
		}
		for (const [suffix, partLabel] of Object.entries(PARTS)) {
			const n = `${stem}-${suffix}.ans`;
			if (names.includes(n)) {
				add(group, { name: n, label: partLabel, part: true });
				used.add(n);
				addVariants(group, `${stem}-${suffix}`);
			}
		}
	}
	for (const n of names.filter((n) => !used.has(n)).sort()) {
		add('Other', { name: n, label: n.replace(/\.ans$/i, ''), part: false });
	}
	return ORDER.filter((g) => byGroup.has(g)).map((g) => ({ title: g, items: byGroup.get(g)! }));
}
