import { t } from '$lib/i18n.svelte';
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
// Groups and labels are catalog keys (shown in the admin's language).
const KNOWN: Record<string, [string, string]> = {
	welcome: ['login', 'welcome'],
	main: ['login', 'main'],
	sysop: ['login', 'sysop'],
	logoff: ['login', 'logoff'],
	msgareas: ['msgs', 'area_list'],
	msglist: ['msgs', 'msg_list'],
	msgread: ['msgs', 'msg_read'],
	msgpost: ['msgs', 'msg_post'],
	filareas: ['files', 'area_list'],
	fillist: ['files', 'file_list'],
	filread: ['files', 'file_read'],
	netmail: ['netmail', 'inbox'],
	netread: ['netmail', 'net_read']
};

const PARTS: Record<string, string> = {
	columns: 'columns',
	row: 'row',
	'row-selected': 'row_selected',
	network: 'network',
	meta: 'meta'
};

const ORDER = ['login', 'msgs', 'files', 'netmail', 'other'];

function groupTitle(g: string): string {
	return (
		{
			login: t('admin.screens.group.login'),
			msgs: t('admin.common.message_areas'),
			files: t('common.file_areas'),
			netmail: t('common.netmail'),
			other: t('admin.screens.group.other')
		}[g] ?? g
	);
}

function screenLabel(k: string): string {
	return (
		{
			welcome: t('admin.screens.label.welcome'),
			main: t('common.main_menu'),
			sysop: t('common.sysop_menu'),
			logoff: t('admin.common.logoff'),
			area_list: t('admin.screens.label.area_list'),
			msg_list: t('admin.screens.label.msg_list'),
			msg_read: t('admin.screens.label.msg_read'),
			msg_post: t('admin.screens.label.msg_post'),
			file_list: t('admin.screens.label.file_list'),
			file_read: t('admin.screens.label.file_read'),
			inbox: t('web.common.inbox'),
			net_read: t('admin.screens.label.net_read'),
			columns: t('admin.screens.label.columns'),
			row: t('admin.screens.label.row'),
			row_selected: t('admin.screens.label.row_selected'),
			network: t('admin.screens.label.network'),
			meta: t('admin.screens.label.meta')
		}[k] ?? k
	);
}

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
			add(group, { name: `${stem}.ans`, label: screenLabel(label), part: false });
			used.add(`${stem}.ans`);
			addVariants(group, stem);
		}
		for (const [suffix, partLabel] of Object.entries(PARTS)) {
			const n = `${stem}-${suffix}.ans`;
			if (names.includes(n)) {
				add(group, { name: n, label: screenLabel(partLabel), part: true });
				used.add(n);
				addVariants(group, `${stem}-${suffix}`);
			}
		}
	}
	for (const n of names.filter((n) => !used.has(n)).sort()) {
		add('other', { name: n, label: n.replace(/\.ans$/i, ''), part: false });
	}
	return ORDER.filter((g) => byGroup.has(g)).map((g) => ({ title: groupTitle(g), items: byGroup.get(g)! }));
}
