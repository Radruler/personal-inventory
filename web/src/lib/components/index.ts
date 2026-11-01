// UI kit (thread D). Everything is styled with var(--…) tokens only and is
// shown on /dev/kit. Props are documented in each component (JSDoc on $props).

// primitives
export { default as Badge } from './Badge.svelte';
export { default as Button } from './Button.svelte';
export { default as Chip } from './Chip.svelte';
export { default as Icon } from './Icon.svelte';
export { default as ProvenanceBadge } from './ProvenanceBadge.svelte';
export { default as SearchBox } from './SearchBox.svelte';
export { default as Sheet } from './Sheet.svelte';
export { default as Spinner } from './Spinner.svelte';
export { default as Tabs } from './Tabs.svelte';
export { default as Toaster } from './Toaster.svelte';

// states
export { default as EmptyState } from './EmptyState.svelte';
export { default as ErrorState } from './ErrorState.svelte';
export { default as LoadingState } from './LoadingState.svelte';

// domain
export { default as ActiveFilters } from './ActiveFilters.svelte';
export { default as FilterSheet } from './FilterSheet.svelte';
export { default as Gallery } from './Gallery.svelte';
export { default as ItemCard } from './ItemCard.svelte';
export { default as LocationChip } from './LocationChip.svelte';
export { default as LocationPicker } from './LocationPicker.svelte';
export { default as MediaViewer } from './MediaViewer.svelte';
export { default as MultiSelectBar } from './MultiSelectBar.svelte';
export { default as PhotoGrid } from './PhotoGrid.svelte';

// logic and types
export type { TabDef } from './Tabs.svelte';
export type { ViewerPhoto } from './MediaViewer.svelte';
export { ICONS, KIND_ICON, LOCATION_KIND_ICON, STATUS_ICON, kindIcon, type IconName } from './icons';
export * from './format';
export * from './filters';
export * from './gallery';
export * from './locationTree';
export * from './selection';
export { toast, dismissToast, toasts, type ToastItem, type ToastTone, type ToastAction } from './toast.svelte';
export { focusables, trapTab } from './focus';
