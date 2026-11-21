// Base64, which QuickJS has no built-in for.
const B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
export const toBase64 = (u) => { let o = ""; for (let i = 0; i < u.length; i += 3) { const n = (u[i] << 16) | ((u[i + 1] ?? 0) << 8) | (u[i + 2] ?? 0); o += B64[n >> 18] + B64[(n >> 12) & 63] + (i + 1 < u.length ? B64[(n >> 6) & 63] : "=") + (i + 2 < u.length ? B64[n & 63] : "="); } return o; };
export const fromBase64 = (s) => { const t = s.replace(/=+$/, ""), u = new Uint8Array(Math.floor(t.length * 3 / 4)); for (let i = 0, j = 0, n = 0, bits = 0; i < t.length; i++) { n = (n << 6) | B64.indexOf(t[i]); bits += 6; if (bits >= 8) { bits -= 8; u[j++] = (n >> bits) & 255; } } return u; };
