// Small display helpers shared by the kit (pure; unit-tested).
import type { LocationRef, Placement, PlacementStatus, Schemas } from '$lib/api/client';

export const KIND_LABEL: Record<Schemas['Kind'], string> = {
  clothing: 'Clothing',
  object: 'Object',
  utility: 'Utility'
};

export const LOCATION_KIND_LABEL: Record<Schemas['LocationKind'], string> = {
  site: 'Site',
  room: 'Room',
  unit: 'Unit',
  shelf: 'Shelf',
  container: 'Box'
};

export const STATUS_LABEL: Record<PlacementStatus, string> = {
  placed: 'Placed',
  approximate: 'Approximate',
  staged: 'Staged',
  unknown: 'Unknown',
  gone: 'Gone'
};

export const BASIS_LABEL: Record<Schemas['FactBasis'], string> = {
  user: 'You',
  observed: 'Observed',
  looked_up: 'Looked up',
  inferred: 'Inferred'
};

/** `outerwear/shell` → `outerwear › shell`; null/empty → ''. */
export function formatCategory(category: string | null | undefined): string {
  if (!category) return '';
  return category
    .split('/')
    .map((s) => s.trim())
    .filter(Boolean)
    .join(' › ');
}

/** Label for a role string (registry roles plus a readable fallback). */
export function roleLabel(role: string): string {
  const known: Record<string, string> = {
    flat_lay: 'Flat lay',
    on_body: 'On body',
    stock: 'Stock',
    label: 'Label',
    care_label: 'Care label',
    hang_tag: 'Hang tag',
    detail: 'Detail',
    defect: 'Defect',
    box_exterior: 'Box exterior',
    box_contents: 'Box contents',
    receipt: 'Receipt',
    location: 'Location',
    other: 'Other'
  };
  if (known[role]) return known[role];
  const s = role.replace(/[_-]+/g, ' ').trim();
  return s ? s[0].toUpperCase() + s.slice(1) : 'Other';
}

/** The shortest useful name for a location: its code, else its name. */
export function shortRef(ref: Pick<LocationRef, 'code' | 'name'> | null | undefined): string {
  if (!ref) return '';
  return ref.code || ref.name;
}

/**
 * Text for a location chip. `full` is the server's label verbatim. `compact`
 * keeps cards short: staged becomes "Bedroom → U2", unknown "Unknown".
 */
export function chipText(p: Placement, variant: 'compact' | 'full' = 'compact'): string {
  if (variant === 'full') return p.label;
  switch (p.status) {
    case 'staged': {
      const from = p.location ? shortRef(p.location) : '?';
      return `${from} → ${shortRef(p.target) || '?'}`;
    }
    case 'unknown':
      return 'Unknown';
    case 'placed':
    case 'approximate':
      return p.location?.label ?? p.label;
    default:
      return p.label;
  }
}

/** Splits a staged label "A → going to B" into its two halves, or null. */
export function splitStaged(label: string): [string, string] | null {
  const i = label.indexOf('→');
  if (i < 0) return null;
  return [label.slice(0, i).trim(), label.slice(i + 1).trim()];
}

/** 0..1 → "80%"; null/undefined → ''. */
export function formatConfidence(c: number | null | undefined): string {
  if (c === null || c === undefined || Number.isNaN(c)) return '';
  return `${Math.round(Math.max(0, Math.min(1, c)) * 100)}%`;
}

/** Message for any thrown value (ApiError, Error, string, unknown). */
export function errorMessage(err: unknown): string {
  if (!err) return '';
  if (typeof err === 'string') return err;
  if (err instanceof Error) return err.message;
  if (typeof err === 'object' && 'message' in err && typeof (err as { message: unknown }).message === 'string') {
    return (err as { message: string }).message;
  }
  return String(err);
}
