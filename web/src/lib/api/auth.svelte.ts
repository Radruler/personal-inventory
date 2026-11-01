// Optional bearer token (INV_TOKEN on the server). Kept in localStorage only;
// nothing is stored server-side. `auth.needed` flips to true after a 401 so
// the layout can ask for the token.
const KEY = 'inv.token';

function read(): string | null {
  try {
    return localStorage.getItem(KEY);
  } catch {
    return null;
  }
}

export const auth = $state({ token: read(), needed: false });

export function setToken(token: string | null) {
  auth.token = token && token.trim() ? token.trim() : null;
  auth.needed = false;
  try {
    if (auth.token) localStorage.setItem(KEY, auth.token);
    else localStorage.removeItem(KEY);
  } catch {
    /* private mode: keep it in memory */
  }
}
