// FTN-style quoting for replies: " SW> " before every line of the
// original, with the sender's initials -- two words give their first
// letters ("Deon George" -> DG), one word its first two ("SwissMaik"
// -> Sw). Lines quoted before keep their own prefix; the original's
// kludges, tearline, origin, SEEN-BY and PATH are left out.
export function initials(name: string): string {
	const words = name.split(/[^\p{L}\p{N}]+/u).filter(Boolean);
	if (words.length === 0) return 'XX';
	if (words.length === 1) {
		const w = words[0];
		return w.charAt(0).toUpperCase() + (w.charAt(1) || '').toLowerCase();
	}
	return (words[0].charAt(0) + words[words.length - 1].charAt(0)).toUpperCase();
}

const QUOTED = /^ ?[\p{L}\p{N}]{0,4}>/u;

/**
 * The quoted original for a reply, headed by who wrote it to whom --
 * " -=> Mortar M. wrote to poindexter FORTRAN <=-", as GoldED and most
 * FTN editors do.
 */
export function quoteText(body: string, from: string, to = ''): string {
	const prefix = ` ${initials(from)}> `;
	const lines = body.replace(/\r\n?/g, '\n').split('\n');
	const out: string[] = [];
	for (const line of lines) {
		if (line.startsWith('\x01')) continue;
		if (line === '---' || line.startsWith('--- ')) break;
		if (/^ \* Origin:|^SEEN-BY:|^PATH:/.test(line)) continue;
		if (line.trim() === '') out.push('');
		else if (QUOTED.test(line)) out.push(line);
		else out.push(prefix + line);
	}
	while (out.length && out[out.length - 1] === '') out.pop();
	while (out.length && out[0] === '') out.shift();
	const head = ` -=> ${from} wrote to ${to.trim() || 'All'} <=-`;
	return out.length ? `${head}\n\n${out.join('\n')}` : head;
}
