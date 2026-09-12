export interface BBSConfig {
	name: string;
	sysop: string;
	new_user_sl: number;
	telnet_enabled: boolean;
	telnet_addr: string;
	ssh_enabled: boolean;
	ssh_addr: string;
}

export interface LoginResponse {
	token: string;
	username: string;
	security_level: number;
	expires_at: string;
}

export interface Node {
	node: number;
	remote_ip: string;
	term_type: string;
	username: string;
	connected_at: string;
}

export interface Dashboard {
	bbs_name: string;
	version: string;
	user_count: number;
	message_area_count: number;
	file_area_count: number;
	nodes: Node[];
}

export class ApiError extends Error {
	status: number;
	constructor(status: number, message: string) {
		super(message);
		this.status = status;
	}
}

async function request<T>(path: string, options: RequestInit = {}, token?: string): Promise<T> {
	const headers = new Headers(options.headers);
	headers.set('Content-Type', 'application/json');
	if (token) headers.set('Authorization', `Bearer ${token}`);

	const res = await fetch(path, { ...options, headers });
	if (!res.ok) {
		let message = res.statusText;
		try {
			const body = await res.json();
			if (body?.error) message = body.error;
		} catch {
			// response body wasn't JSON; fall back to statusText
		}
		throw new ApiError(res.status, message);
	}
	return (await res.json()) as T;
}

export function login(username: string, password: string): Promise<LoginResponse> {
	return request<LoginResponse>('/api/auth/login', {
		method: 'POST',
		body: JSON.stringify({ username, password })
	});
}

export function getConfig(token: string): Promise<BBSConfig> {
	return request<BBSConfig>('/api/config', { method: 'GET' }, token);
}

export function putConfig(
	token: string,
	config: BBSConfig
): Promise<{ config: BBSConfig; note: string }> {
	return request('/api/config', { method: 'PUT', body: JSON.stringify(config) }, token);
}

export function getDashboard(token: string): Promise<Dashboard> {
	return request<Dashboard>('/api/dashboard', { method: 'GET' }, token);
}
