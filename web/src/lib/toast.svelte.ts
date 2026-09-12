export interface Toast {
	id: number;
	message: string;
	kind: 'success' | 'error';
}

let nextId = 1;

class ToastState {
	toasts = $state<Toast[]>([]);

	push(message: string, kind: Toast['kind'] = 'success', durationMs = 3000) {
		const id = nextId++;
		this.toasts.push({ id, message, kind });
		setTimeout(() => {
			this.toasts = this.toasts.filter((t) => t.id !== id);
		}, durationMs);
	}
}

export const toast = new ToastState();
