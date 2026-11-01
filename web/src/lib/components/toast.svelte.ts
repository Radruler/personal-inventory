// App-wide toasts. Call `toast('Saved')` from anywhere; render <Toaster />
// once (the app layout should own it; /dev/kit renders its own).
export type ToastTone = 'info' | 'success' | 'error';
export type ToastAction = { label: string; run: () => void };
export type ToastItem = { id: number; message: string; tone: ToastTone; action?: ToastAction; timeout: number };

export const toasts: ToastItem[] = $state([]);

let seq = 0;
const timers = new Map<number, ReturnType<typeof setTimeout>>();

export function dismissToast(id: number) {
  clearTimeout(timers.get(id));
  timers.delete(id);
  const i = toasts.findIndex((t) => t.id === id);
  if (i >= 0) toasts.splice(i, 1);
}

/** Shows a toast; returns its id. timeout 0 = stays until dismissed. Errors default to 8 s. */
export function toast(
  message: string,
  opts: { tone?: ToastTone; action?: ToastAction; timeout?: number } = {}
): number {
  const id = ++seq;
  const tone = opts.tone ?? 'info';
  const timeout = opts.timeout ?? (tone === 'error' ? 8000 : 4000);
  toasts.push({ id, message, tone, action: opts.action, timeout });
  while (toasts.length > 3) dismissToast(toasts[0].id);
  if (timeout > 0) timers.set(id, setTimeout(() => dismissToast(id), timeout));
  return id;
}
